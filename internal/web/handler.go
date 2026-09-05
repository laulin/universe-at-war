// Package web implements the server-rendered HTTP transport.
package web

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strconv"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appsetup "universeatwar/internal/app/setup"
	"universeatwar/internal/domain/rules"
	"universeatwar/internal/domain/server"
	webassets "universeatwar/web"
)

const (
	sessionCookieName = "uaw_session"
	csrfCookieName    = "uaw_csrf"
	maxFormBytes      = 64 << 10
)

type authenticationService interface {
	Login(context.Context, string, string) (appauth.LoginResult, error)
	Resolve(context.Context, string) (appauth.Principal, error)
	ChangePassword(context.Context, string, string, string) (appauth.LoginResult, error)
}

type stateService interface {
	Current(context.Context) (server.State, error)
}

type secretGenerator interface {
	Generate() (string, error)
}

type setupService interface {
	Load(context.Context, appauth.Principal) (appsetup.Draft, error)
	Save(context.Context, appauth.Principal, int, int64, rules.Ruleset) (appsetup.Draft, error)
	Activate(context.Context, appauth.Principal, int64, rules.Ruleset) error
}

// Dependencies are the application services required by the HTTP adapter.
type Dependencies struct {
	Authentication authenticationService
	ServerState    stateService
	CSRFSecrets    secretGenerator
	Setup          setupService
	SecureCookies  bool
}

// Handler serves the minimal bootstrap and authentication interface.
type Handler struct {
	authentication authenticationService
	serverState    stateService
	csrfSecrets    secretGenerator
	setup          setupService
	secureCookies  bool
	templates      *template.Template
	mux            *http.ServeMux
}

// New builds a handler and parses all embedded templates eagerly.
func New(dependencies Dependencies) (http.Handler, error) {
	if dependencies.Authentication == nil || dependencies.ServerState == nil || dependencies.CSRFSecrets == nil {
		return nil, errors.New("web: incomplete dependencies")
	}
	templates, err := template.ParseFS(webassets.Files, "templates/*.html")
	if err != nil {
		return nil, err
	}
	staticFiles, err := fs.Sub(webassets.Files, "static")
	if err != nil {
		return nil, err
	}
	handler := &Handler{
		authentication: dependencies.Authentication,
		serverState:    dependencies.ServerState,
		csrfSecrets:    dependencies.CSRFSecrets,
		setup:          dependencies.Setup,
		secureCookies:  dependencies.SecureCookies,
		templates:      templates,
		mux:            http.NewServeMux(),
	}
	handler.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFiles))))
	handler.mux.HandleFunc("GET /healthz", handler.health)
	handler.mux.HandleFunc("GET /login", handler.loginPage)
	handler.mux.HandleFunc("POST /login", handler.login)
	handler.mux.HandleFunc("GET /password/change", handler.passwordChangePage)
	handler.mux.HandleFunc("POST /password/change", handler.passwordChange)
	handler.mux.HandleFunc("GET /setup/{step}", handler.setupPage)
	handler.mux.HandleFunc("POST /setup/{step}", handler.saveSetupStep)
	handler.mux.HandleFunc("GET /{$}", handler.home)
	return handler.securityHeaders(handler.mux), nil
}

func (h *Handler) health(response http.ResponseWriter, request *http.Request) {
	state, err := h.serverState.Current(request.Context())
	if err != nil {
		http.Error(response, "health unavailable", http.StatusServiceUnavailable)
		return
	}
	response.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(response).Encode(map[string]string{"status": "ok", "server_state": string(state)})
}

func (h *Handler) loginPage(response http.ResponseWriter, request *http.Request) {
	token, ok := h.ensureCSRF(response, request)
	if !ok {
		return
	}
	h.render(response, http.StatusOK, "login.html", pageData{CSRFToken: token})
}

func (h *Handler) login(response http.ResponseWriter, request *http.Request) {
	if !h.validCSRF(response, request) {
		return
	}
	result, err := h.authentication.Login(request.Context(), request.FormValue("username"), request.FormValue("password"))
	if err != nil {
		if errors.Is(err, appauth.ErrInvalidCredentials) {
			token, _ := request.Cookie(csrfCookieName)
			h.render(response, http.StatusUnauthorized, "login.html", pageData{
				CSRFToken: token.Value,
				Error:     "Identifiant ou mot de passe incorrect.",
			})
			return
		}
		http.Error(response, "authentication unavailable", http.StatusInternalServerError)
		return
	}
	h.setSessionCookie(response, result.Token, result.ExpiresAt)
	if result.MustChangePassword {
		http.Redirect(response, request, "/password/change", http.StatusSeeOther)
		return
	}
	http.Redirect(response, request, "/setup/1", http.StatusSeeOther)
}

func (h *Handler) passwordChangePage(response http.ResponseWriter, request *http.Request) {
	if _, _, ok := h.requirePrincipal(response, request); !ok {
		return
	}
	token, ok := h.ensureCSRF(response, request)
	if !ok {
		return
	}
	h.render(response, http.StatusOK, "password-change.html", pageData{CSRFToken: token})
}

func (h *Handler) passwordChange(response http.ResponseWriter, request *http.Request) {
	_, sessionToken, ok := h.requirePrincipal(response, request)
	if !ok {
		return
	}
	if !h.validCSRF(response, request) {
		return
	}
	result, err := h.authentication.ChangePassword(
		request.Context(),
		sessionToken,
		request.FormValue("current_password"),
		request.FormValue("new_password"),
	)
	if err != nil {
		csrfCookie, _ := request.Cookie(csrfCookieName)
		message := "Impossible de changer le mot de passe."
		if errors.Is(err, appauth.ErrWeakPassword) {
			message = "Le nouveau mot de passe doit contenir au moins 12 caractères."
		}
		h.render(response, http.StatusBadRequest, "password-change.html", pageData{CSRFToken: csrfCookie.Value, Error: message})
		return
	}
	h.setSessionCookie(response, result.Token, result.ExpiresAt)
	http.Redirect(response, request, "/setup/1", http.StatusSeeOther)
}

func (h *Handler) setupPage(response http.ResponseWriter, request *http.Request) {
	principal, _, ok := h.requirePrincipal(response, request)
	if !ok {
		return
	}
	if principal.MustChangePassword {
		http.Redirect(response, request, "/password/change", http.StatusSeeOther)
		return
	}
	if h.setup == nil {
		http.NotFound(response, request)
		return
	}
	draft, err := h.setup.Load(request.Context(), principal)
	if errors.Is(err, appsetup.ErrAlreadyCompleted) {
		http.Redirect(response, request, "/", http.StatusSeeOther)
		return
	}
	if err != nil {
		http.Error(response, "setup unavailable", http.StatusForbidden)
		return
	}
	requestedStep, err := strconv.Atoi(request.PathValue("step"))
	if err != nil || requestedStep < 1 || requestedStep > 10 {
		http.NotFound(response, request)
		return
	}
	if requestedStep != draft.CurrentStep {
		http.Redirect(response, request, fmt.Sprintf("/setup/%d", draft.CurrentStep), http.StatusSeeOther)
		return
	}
	token, ok := h.ensureCSRF(response, request)
	if !ok {
		return
	}
	h.renderSetup(response, http.StatusOK, setupPageData{
		CSRFToken: token, Step: draft.CurrentStep, Version: draft.Version, Rules: draft.Rules,
	})
}

func (h *Handler) saveSetupStep(response http.ResponseWriter, request *http.Request) {
	principal, _, ok := h.requirePrincipal(response, request)
	if !ok {
		return
	}
	if principal.MustChangePassword {
		http.Redirect(response, request, "/password/change", http.StatusSeeOther)
		return
	}
	if h.setup == nil {
		http.NotFound(response, request)
		return
	}
	if !h.validCSRF(response, request) {
		return
	}
	step, err := strconv.Atoi(request.PathValue("step"))
	if err != nil || step < 1 || step > 10 {
		http.NotFound(response, request)
		return
	}
	draft, err := h.setup.Load(request.Context(), principal)
	if err != nil {
		http.Error(response, "setup unavailable", http.StatusForbidden)
		return
	}
	if step != draft.CurrentStep {
		http.Error(response, "setup step conflict", http.StatusConflict)
		return
	}
	expectedVersion, err := strconv.ParseInt(request.PostFormValue("version"), 10, 64)
	if err != nil || expectedVersion != draft.Version {
		http.Error(response, "setup version conflict", http.StatusConflict)
		return
	}
	if step == 10 {
		if request.PostFormValue("confirm") != "yes" {
			h.renderSetup(response, http.StatusBadRequest, setupPageData{
				CSRFToken: request.PostFormValue("csrf_token"), Step: step, Version: draft.Version,
				Rules: draft.Rules, Error: "La confirmation explicite est obligatoire.",
			})
			return
		}
		if err := h.setup.Activate(request.Context(), principal, expectedVersion, draft.Rules); err != nil {
			http.Error(response, "setup activation failed", http.StatusConflict)
			return
		}
		http.Redirect(response, request, "/", http.StatusSeeOther)
		return
	}

	updated := draft.Rules
	if err := updateRulesFromForm(step, request, &updated); err != nil {
		h.renderSetup(response, http.StatusBadRequest, setupPageData{
			CSRFToken: request.PostFormValue("csrf_token"), Step: step, Version: draft.Version,
			Rules: updated, Error: err.Error(),
		})
		return
	}
	saved, err := h.setup.Save(request.Context(), principal, step, expectedVersion, updated)
	if err != nil {
		h.renderSetup(response, http.StatusBadRequest, setupPageData{
			CSRFToken: request.PostFormValue("csrf_token"), Step: step, Version: draft.Version,
			Rules: updated, Error: err.Error(),
		})
		return
	}
	http.Redirect(response, request, fmt.Sprintf("/setup/%d", saved.CurrentStep), http.StatusSeeOther)
}

func (h *Handler) home(response http.ResponseWriter, request *http.Request) {
	principal, _, ok := h.requirePrincipal(response, request)
	if !ok {
		return
	}
	if principal.MustChangePassword {
		http.Redirect(response, request, "/password/change", http.StatusSeeOther)
		return
	}
	state, err := h.serverState.Current(request.Context())
	if err != nil {
		http.Error(response, "server state unavailable", http.StatusInternalServerError)
		return
	}
	if state != server.Running {
		http.Redirect(response, request, "/setup/1", http.StatusSeeOther)
		return
	}
	http.NotFound(response, request)
}

func (h *Handler) requirePrincipal(response http.ResponseWriter, request *http.Request) (appauth.Principal, string, bool) {
	cookie, err := request.Cookie(sessionCookieName)
	if err != nil {
		http.Redirect(response, request, "/login", http.StatusSeeOther)
		return appauth.Principal{}, "", false
	}
	principal, err := h.authentication.Resolve(request.Context(), cookie.Value)
	if err != nil {
		http.Redirect(response, request, "/login", http.StatusSeeOther)
		return appauth.Principal{}, "", false
	}
	return principal, cookie.Value, true
}

func (h *Handler) ensureCSRF(response http.ResponseWriter, request *http.Request) (string, bool) {
	if cookie, err := request.Cookie(csrfCookieName); err == nil && cookie.Value != "" {
		return cookie.Value, true
	}
	token, err := h.csrfSecrets.Generate()
	if err != nil {
		http.Error(response, "security token unavailable", http.StatusInternalServerError)
		return "", false
	}
	http.SetCookie(response, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int((12 * time.Hour).Seconds()),
	})
	return token, true
}

func (h *Handler) validCSRF(response http.ResponseWriter, request *http.Request) bool {
	request.Body = http.MaxBytesReader(response, request.Body, maxFormBytes)
	if err := request.ParseForm(); err != nil {
		http.Error(response, "invalid form", http.StatusBadRequest)
		return false
	}
	cookie, err := request.Cookie(csrfCookieName)
	formToken := request.PostFormValue("csrf_token")
	if err != nil || cookie.Value == "" || formToken == "" || len(cookie.Value) != len(formToken) ||
		subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(formToken)) != 1 {
		http.Error(response, "invalid CSRF token", http.StatusForbidden)
		return false
	}
	return true
}

func (h *Handler) setSessionCookie(response http.ResponseWriter, token string, expiresAt time.Time) {
	http.SetCookie(response, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: http.SameSiteLaxMode,
		Expires:  expiresAt,
	})
}

func (h *Handler) render(response http.ResponseWriter, status int, name string, data pageData) {
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.WriteHeader(status)
	if err := h.templates.ExecuteTemplate(response, name, data); err != nil {
		return
	}
}

func (h *Handler) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		response.Header().Set("X-Content-Type-Options", "nosniff")
		response.Header().Set("X-Frame-Options", "DENY")
		response.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(response, request)
	})
}

type pageData struct {
	CSRFToken string
	Error     string
}
