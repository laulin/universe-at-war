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
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appeconomy "universeatwar/internal/app/economy"
	appsetup "universeatwar/internal/app/setup"
	"universeatwar/internal/domain/building"
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
	Logout(context.Context, string) error
}

type stateService interface {
	Current(context.Context) (server.State, error)
}

type secretGenerator interface {
	Generate() (string, error)
}

type loginRateLimiter interface {
	Allow(string) (time.Duration, bool)
	Failure(string)
	Success(string)
}

type setupService interface {
	Load(context.Context, appauth.Principal) (appsetup.Draft, error)
	Save(context.Context, appauth.Principal, int, int64, rules.Ruleset) (appsetup.Draft, error)
	Activate(context.Context, appauth.Principal, int64, rules.Ruleset) error
}

type economyService interface {
	CreateEmpire(context.Context, appauth.Principal, string) (appeconomy.Planet, error)
	Planets(context.Context, appauth.Principal) ([]appeconomy.Planet, error)
	Buildings(context.Context, appauth.Principal, int64) (appeconomy.Planet, []appeconomy.BuildingChoice, error)
	StartConstruction(context.Context, appauth.Principal, int64, building.ID, string) (appeconomy.Queue, error)
}

// Dependencies are the application services required by the HTTP adapter.
type Dependencies struct {
	Authentication authenticationService
	ServerState    stateService
	CSRFSecrets    secretGenerator
	Setup          setupService
	Economy        economyService
	SecureCookies  bool
	LoginLimiter   loginRateLimiter
}

// Handler serves the minimal bootstrap and authentication interface.
type Handler struct {
	authentication authenticationService
	serverState    stateService
	csrfSecrets    secretGenerator
	setup          setupService
	economy        economyService
	secureCookies  bool
	loginLimiter   loginRateLimiter
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
	limiter := dependencies.LoginLimiter
	if limiter == nil {
		limiter = NewLoginLimiter(time.Now)
	}
	handler := &Handler{
		authentication: dependencies.Authentication,
		serverState:    dependencies.ServerState,
		csrfSecrets:    dependencies.CSRFSecrets,
		setup:          dependencies.Setup,
		economy:        dependencies.Economy,
		secureCookies:  dependencies.SecureCookies,
		loginLimiter:   limiter,
		templates:      templates,
		mux:            http.NewServeMux(),
	}
	handler.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFiles))))
	handler.mux.HandleFunc("GET /healthz", handler.health)
	handler.mux.HandleFunc("GET /login", handler.loginPage)
	handler.mux.HandleFunc("POST /login", handler.login)
	handler.mux.HandleFunc("GET /password/change", handler.passwordChangePage)
	handler.mux.HandleFunc("POST /password/change", handler.passwordChange)
	handler.mux.HandleFunc("POST /logout", handler.logout)
	handler.mux.HandleFunc("GET /setup/{step}", handler.setupPage)
	handler.mux.HandleFunc("POST /setup/{step}", handler.saveSetupStep)
	handler.mux.HandleFunc("POST /empire", handler.createEmpire)
	handler.mux.HandleFunc("GET /planets/{planet}", handler.planetPage)
	handler.mux.HandleFunc("POST /planets/{planet}/buildings/{building}", handler.startBuilding)
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
	loginKey := rateLimitKey(request, request.FormValue("username"))
	if retry, allowed := h.loginLimiter.Allow(loginKey); !allowed {
		seconds := int(retry.Round(time.Second) / time.Second)
		if seconds < 1 {
			seconds = 1
		}
		response.Header().Set("Retry-After", strconv.Itoa(seconds))
		http.Error(response, "too many login attempts", http.StatusTooManyRequests)
		return
	}
	result, err := h.authentication.Login(request.Context(), request.FormValue("username"), request.FormValue("password"))
	if err != nil {
		if errors.Is(err, appauth.ErrInvalidCredentials) {
			h.loginLimiter.Failure(loginKey)
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
	h.loginLimiter.Success(loginKey)
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

func (h *Handler) logout(response http.ResponseWriter, request *http.Request) {
	_, sessionToken, ok := h.requirePrincipal(response, request)
	if !ok {
		return
	}
	if !h.validCSRF(response, request) {
		return
	}
	if err := h.authentication.Logout(request.Context(), sessionToken); err != nil {
		http.Error(response, "logout unavailable", http.StatusInternalServerError)
		return
	}
	http.SetCookie(response, &http.Cookie{
		Name: sessionCookieName, Value: "", Path: "/", HttpOnly: true,
		Secure: h.secureCookies, SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})
	http.Redirect(response, request, "/login", http.StatusSeeOther)
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
	if h.economy == nil {
		http.Error(response, "economy unavailable", http.StatusServiceUnavailable)
		return
	}
	planets, err := h.economy.Planets(request.Context(), principal)
	if errors.Is(err, appeconomy.ErrNoEmpire) {
		token, ok := h.ensureCSRF(response, request)
		if !ok {
			return
		}
		h.render(response, http.StatusOK, "empire.html", pageData{CSRFToken: token})
		return
	}
	if err != nil {
		http.Error(response, "economy unavailable", http.StatusInternalServerError)
		return
	}
	h.renderOverview(response, request, http.StatusOK, planets)
}

// planetPage renders one planet of the signed-in account. A planet owned by
// somebody else is reported as missing so the response never proves it exists.
func (h *Handler) planetPage(response http.ResponseWriter, request *http.Request) {
	principal, _, ok := h.requirePrincipal(response, request)
	if !ok {
		return
	}
	if principal.MustChangePassword {
		http.Redirect(response, request, "/password/change", http.StatusSeeOther)
		return
	}
	planetID, ok := h.planetParameter(response, request)
	if !ok {
		return
	}
	planet, choices, err := h.economy.Buildings(request.Context(), principal, planetID)
	if errors.Is(err, appeconomy.ErrPlanetNotFound) || errors.Is(err, appeconomy.ErrNoEmpire) {
		http.NotFound(response, request)
		return
	}
	if err != nil {
		http.Error(response, "economy unavailable", http.StatusInternalServerError)
		return
	}
	h.renderEconomy(response, request, http.StatusOK, planet, choices, "")
}

func (h *Handler) planetParameter(response http.ResponseWriter, request *http.Request) (int64, bool) {
	planetID, err := strconv.ParseInt(request.PathValue("planet"), 10, 64)
	if err != nil || planetID <= 0 {
		http.NotFound(response, request)
		return 0, false
	}
	return planetID, true
}

func (h *Handler) createEmpire(response http.ResponseWriter, request *http.Request) {
	principal, _, ok := h.requirePrincipal(response, request)
	if !ok {
		return
	}
	if !h.validCSRF(response, request) {
		return
	}
	if h.economy == nil {
		http.NotFound(response, request)
		return
	}
	_, err := h.economy.CreateEmpire(request.Context(), principal, request.PostFormValue("name"))
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, appeconomy.ErrEmpireExists) {
			status = http.StatusConflict
		}
		h.render(response, status, "empire.html", pageData{CSRFToken: request.PostFormValue("csrf_token"), Error: "Impossible de créer cet empire."})
		return
	}
	http.Redirect(response, request, "/", http.StatusSeeOther)
}

func (h *Handler) startBuilding(response http.ResponseWriter, request *http.Request) {
	principal, _, ok := h.requirePrincipal(response, request)
	if !ok {
		return
	}
	if !h.validCSRF(response, request) {
		return
	}
	planetID, ok := h.planetParameter(response, request)
	if !ok {
		return
	}
	_, err := h.economy.StartConstruction(request.Context(), principal, planetID, building.ID(request.PathValue("building")), request.PostFormValue("idempotency_key"))
	if errors.Is(err, appeconomy.ErrPlanetNotFound) || errors.Is(err, appeconomy.ErrNoEmpire) {
		http.NotFound(response, request)
		return
	}
	if err != nil {
		planet, choices, loadErr := h.economy.Buildings(request.Context(), principal, planetID)
		if loadErr != nil {
			http.Error(response, "construction unavailable", http.StatusBadRequest)
			return
		}
		h.renderEconomy(response, request, http.StatusBadRequest, planet, choices, buildingError(err))
		return
	}
	http.Redirect(response, request, fmt.Sprintf("/planets/%d", planetID), http.StatusSeeOther)
}

type buildingPageChoice struct {
	ID             building.ID
	Name           string
	Level          int
	CostMetal      int64
	CostCrystal    int64
	CostDeuterium  int64
	Duration       time.Duration
	CanStart       bool
	Reason         string
	IdempotencyKey string
}

type overviewPageData struct {
	CSRFToken string
	Planets   []appeconomy.Planet
}

func (h *Handler) renderOverview(response http.ResponseWriter, request *http.Request, status int, planets []appeconomy.Planet) {
	token, ok := h.ensureCSRF(response, request)
	if !ok {
		return
	}
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.WriteHeader(status)
	_ = h.templates.ExecuteTemplate(response, "overview.html", overviewPageData{CSRFToken: token, Planets: planets})
}

type economyPageData struct {
	CSRFToken string
	Error     string
	Planet    appeconomy.Planet
	Choices   []buildingPageChoice
}

func (h *Handler) renderEconomy(response http.ResponseWriter, request *http.Request, status int, planet appeconomy.Planet, choices []appeconomy.BuildingChoice, message string) {
	token, ok := h.ensureCSRF(response, request)
	if !ok {
		return
	}
	views := make([]buildingPageChoice, 0, len(choices))
	for _, choice := range choices {
		reason := choice.Reason
		if planet.ActiveQueue != nil {
			reason = "Une construction est déjà en cours."
		} else if choice.Available && !choice.Affordable {
			reason = "Ressources insuffisantes."
		}
		views = append(views, buildingPageChoice{
			ID: choice.Definition.ID, Name: buildingName(choice.Definition.ID), Level: choice.Level,
			CostMetal: choice.Plan.Cost.Metal, CostCrystal: choice.Plan.Cost.Crystal, CostDeuterium: choice.Plan.Cost.Deuterium,
			Duration: choice.Plan.Duration, CanStart: choice.Available && choice.Affordable,
			Reason: reason, IdempotencyKey: fmt.Sprintf("%s:%s:%d", token, choice.Definition.ID, choice.Plan.TargetLevel),
		})
	}
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.WriteHeader(status)
	_ = h.templates.ExecuteTemplate(response, "economy.html", economyPageData{CSRFToken: token, Error: message, Planet: planet, Choices: views})
}

func buildingError(err error) string {
	switch {
	case errors.Is(err, appeconomy.ErrQueueBusy):
		return "Une construction est déjà en cours."
	case errors.Is(err, appeconomy.ErrInvalidRequest):
		return "La demande de construction est invalide."
	default:
		return "La construction ne peut pas démarrer : vérifiez les ressources et les prérequis."
	}
}

func buildingName(id building.ID) string {
	names := map[building.ID]string{
		building.MetalMine: "Mine de métal", building.CrystalMine: "Mine de cristal",
		building.DeuteriumSynthesizer: "Synthétiseur de deutérium", building.SolarPlant: "Centrale solaire",
		building.MetalStorage: "Hangar de métal", building.CrystalStorage: "Hangar de cristal",
		building.DeuteriumTank: "Réservoir de deutérium", building.RoboticsFactory: "Usine de robots",
		building.NaniteFactory: "Usine de nanites", building.Shipyard: "Chantier spatial",
		building.ResearchLab: "Laboratoire de recherche", building.MissileSilo: "Silo à missiles",
		building.Terraformer: "Terraformeur",
	}
	if name := names[id]; name != "" {
		return name
	}
	return string(id)
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

func rateLimitKey(request *http.Request, username string) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		host = request.RemoteAddr
	}
	return host + "|" + strings.ToLower(strings.TrimSpace(username))
}
