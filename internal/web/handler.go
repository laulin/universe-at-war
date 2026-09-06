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
	"log/slog"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appeconomy "universeatwar/internal/app/economy"
	appfleet "universeatwar/internal/app/fleet"
	appgalaxy "universeatwar/internal/app/galaxy"
	appjumpgate "universeatwar/internal/app/jumpgate"
	appphalanx "universeatwar/internal/app/phalanx"
	appregistration "universeatwar/internal/app/registration"
	appreports "universeatwar/internal/app/reports"
	appresearch "universeatwar/internal/app/research"
	appsetup "universeatwar/internal/app/setup"
	appshipyard "universeatwar/internal/app/shipyard"
	"universeatwar/internal/domain/building"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/research"
	"universeatwar/internal/domain/rules"
	"universeatwar/internal/domain/server"
	"universeatwar/internal/domain/unit"
	"universeatwar/internal/domain/universe"
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

type registrationService interface {
	Policy(context.Context) (string, error)
	Register(context.Context, string, string) (int64, error)
}

type economyService interface {
	CreateEmpire(context.Context, appauth.Principal, string) (appeconomy.Planet, error)
	Planets(context.Context, appauth.Principal) ([]appeconomy.Planet, error)
	Buildings(context.Context, appauth.Principal, int64) (appeconomy.Planet, []appeconomy.BuildingChoice, error)
	StartConstruction(context.Context, appauth.Principal, int64, building.ID, string) (appeconomy.Queue, error)
}

type researchService interface {
	Overview(context.Context, appauth.Principal, int64) (appresearch.Overview, error)
	Start(context.Context, appauth.Principal, int64, research.ID, string) (appresearch.Queue, error)
}

type shipyardService interface {
	Ships(context.Context, appauth.Principal, int64) (appshipyard.Overview, error)
	Defenses(context.Context, appauth.Principal, int64) (appshipyard.Overview, error)
	OrderFamily(context.Context, appauth.Principal, int64, unit.ID, unit.Family, int64, string) (appshipyard.Order, error)
}

type fleetService interface {
	Overview(context.Context, appauth.Principal, int64) (appfleet.Overview, error)
	Preview(context.Context, appauth.Principal, int64, appfleet.LaunchRequest) (domainfleet.Plan, error)
	Launch(context.Context, appauth.Principal, int64, appfleet.LaunchRequest, string) (appfleet.Fleet, error)
	Recall(context.Context, appauth.Principal, int64, string) (appfleet.Fleet, error)
}

// bodyKindName names a kind of celestial body for the player.
func bodyKindName(kind building.Placement) string {
	if kind == building.OnMoon {
		return "Lune"
	}
	return "Planète"
}

type galaxyService interface {
	System(context.Context, appauth.Principal, int, int) (appgalaxy.View, error)
}

type reportsService interface {
	List(context.Context, appauth.Principal, appreports.Filter) ([]appreports.Summary, error)
	Get(context.Context, appauth.Principal, int64) (appreports.Detail, error)
	MarkRead(context.Context, appauth.Principal, int64) error
	UnreadHostile(context.Context, appauth.Principal) (int, error)
	Share(context.Context, appauth.Principal, int64, bool) error
	SharedWithAlliance(context.Context, appauth.Principal) ([]appreports.Summary, error)
}

type phalanxService interface {
	Scan(context.Context, appauth.Principal, int64, universe.Coordinate) (appphalanx.Scan, error)
}

type jumpGateService interface {
	Overview(context.Context, appauth.Principal, int64) (appjumpgate.Overview, error)
	Jump(context.Context, appauth.Principal, int64, int64, domainfleet.Composition, string) (appjumpgate.Transfer, error)
}

// Dependencies are the application services required by the HTTP adapter.
type Dependencies struct {
	Authentication authenticationService
	ServerState    stateService
	CSRFSecrets    secretGenerator
	Setup          setupService
	Economy        economyService
	Research       researchService
	Shipyard       shipyardService
	Fleet          fleetService
	Galaxy         galaxyService
	Reports        reportsService
	Phalanx        phalanxService
	JumpGate       jumpGateService
	Alliance       allianceService
	ACS            acsService
	Registration   registrationService
	Logger         *slog.Logger
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
	research       researchService
	shipyard       shipyardService
	fleet          fleetService
	galaxy         galaxyService
	reports        reportsService
	phalanx        phalanxService
	jumpGate       jumpGateService
	alliance       allianceService
	acs            acsService
	registration   registrationService
	secureCookies  bool
	loginLimiter   loginRateLimiter
	clock          func() time.Time
	pages          map[string]*template.Template
	mux            *http.ServeMux
}

// gamePages share the navigation shell; the others keep a bare centred panel.
var (
	gamePages  = []string{"overview", "economy", "research", "production", "fleet", "fleet-send", "fleet-confirm", "galaxy", "reports", "report", "phalanx", "jump-gate", "alliance", "operations"}
	plainPages = []string{"login", "password-change", "empire", "setup", "register"}
)

// parsePages clones the right base template per page so that every page may
// define its own "content" block without colliding with its neighbours.
func parsePages() (map[string]*template.Template, error) {
	pages := map[string]*template.Template{}
	for base, names := range map[string][]string{"layout": gamePages, "shell": plainPages} {
		root, err := template.ParseFS(webassets.Files, "templates/"+base+".html")
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			clone, err := root.Clone()
			if err != nil {
				return nil, err
			}
			page, err := clone.ParseFS(webassets.Files, "templates/"+name+".html")
			if err != nil {
				return nil, err
			}
			pages[name] = page
		}
	}
	return pages, nil
}

// New builds a handler and parses all embedded templates eagerly.
func New(dependencies Dependencies) (http.Handler, error) {
	if dependencies.Authentication == nil || dependencies.ServerState == nil || dependencies.CSRFSecrets == nil {
		return nil, errors.New("web: incomplete dependencies")
	}
	pages, err := parsePages()
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
		research:       dependencies.Research,
		shipyard:       dependencies.Shipyard,
		fleet:          dependencies.Fleet,
		galaxy:         dependencies.Galaxy,
		reports:        dependencies.Reports,
		phalanx:        dependencies.Phalanx,
		jumpGate:       dependencies.JumpGate,
		alliance:       dependencies.Alliance,
		acs:            dependencies.ACS,
		registration:   dependencies.Registration,
		secureCookies:  dependencies.SecureCookies,
		loginLimiter:   limiter,
		clock:          func() time.Time { return time.Now().UTC() },
		pages:          pages,
		mux:            http.NewServeMux(),
	}
	handler.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFiles))))
	handler.mux.HandleFunc("GET /healthz", handler.health)
	handler.mux.HandleFunc("GET /login", handler.loginPage)
	handler.mux.HandleFunc("GET /register", handler.registerPage)
	handler.mux.HandleFunc("POST /register", handler.register)
	handler.mux.HandleFunc("POST /login", handler.login)
	handler.mux.HandleFunc("GET /password/change", handler.passwordChangePage)
	handler.mux.HandleFunc("POST /password/change", handler.passwordChange)
	handler.mux.HandleFunc("POST /logout", handler.logout)
	handler.mux.HandleFunc("GET /setup/{step}", handler.setupPage)
	handler.mux.HandleFunc("POST /setup/{step}", handler.saveSetupStep)
	handler.mux.HandleFunc("POST /empire", handler.createEmpire)
	handler.mux.HandleFunc("GET /planets/switch", handler.switchBody)
	handler.mux.HandleFunc("GET /planets/{planet}", handler.planetPage)
	handler.mux.HandleFunc("POST /planets/{planet}/buildings/{building}", handler.startBuilding)
	handler.mux.HandleFunc("GET /planets/{planet}/research", handler.researchPage)
	handler.mux.HandleFunc("POST /planets/{planet}/research/{research}", handler.startResearch)
	handler.mux.HandleFunc("GET /planets/{planet}/shipyard", handler.shipyardPage)
	handler.mux.HandleFunc("POST /planets/{planet}/shipyard/{unit}", handler.orderShips)
	handler.mux.HandleFunc("GET /planets/{planet}/defense", handler.defensePage)
	handler.mux.HandleFunc("GET /planets/{planet}/fleet", handler.fleetPage)
	handler.mux.HandleFunc("GET /planets/{planet}/fleet/send", handler.fleetSendPage)
	handler.mux.HandleFunc("POST /planets/{planet}/fleet/preview", handler.previewFleet)
	handler.mux.HandleFunc("POST /planets/{planet}/fleet/launch", handler.launchFleet)
	handler.mux.HandleFunc("POST /fleets/{fleet}/recall", handler.recallFleet)
	handler.mux.HandleFunc("GET /planets/{planet}/phalanx", handler.phalanxPage)
	handler.mux.HandleFunc("POST /planets/{planet}/phalanx", handler.scanPhalanx)
	handler.mux.HandleFunc("GET /planets/{planet}/jump", handler.jumpGatePage)
	handler.mux.HandleFunc("POST /planets/{planet}/jump", handler.jumpShips)
	handler.mux.HandleFunc("GET /galaxy/{galaxy}/{system}", handler.galaxyPage)
	handler.mux.HandleFunc("POST /galaxy/{galaxy}/{system}/{position}/spy", handler.spyFromGalaxy)
	handler.mux.HandleFunc("GET /reports", handler.reportsPage)
	handler.mux.HandleFunc("GET /reports/{report}", handler.reportPage)
	handler.mux.HandleFunc("POST /reports/{report}/read", handler.markReportRead)
	handler.mux.HandleFunc("POST /reports/{report}/share", handler.shareReport)
	handler.mux.HandleFunc("GET /alliance", handler.alliancePage)
	handler.mux.HandleFunc("POST /alliance", handler.createAlliance)
	handler.mux.HandleFunc("POST /alliance/invite", handler.inviteToAlliance)
	handler.mux.HandleFunc("POST /alliance/invitations/{invitation}", handler.answerInvitation)
	handler.mux.HandleFunc("POST /alliance/leave", handler.leaveAlliance)
	handler.mux.HandleFunc("POST /alliance/members/{player}/expel", handler.expelMember)
	handler.mux.HandleFunc("POST /alliance/members/{player}/role", handler.promoteMember)
	handler.mux.HandleFunc("POST /alliance/diplomacy", handler.declareRelation)
	handler.mux.HandleFunc("POST /alliance/description", handler.describeAlliance)
	handler.mux.HandleFunc("GET /alliance/operations", handler.operationsPage)
	handler.mux.HandleFunc("GET /alliance/operations/{group}", handler.operationPage)
	handler.mux.HandleFunc("POST /alliance/operations/{group}/preview", handler.previewOperation)
	handler.mux.HandleFunc("POST /alliance/operations/{group}/join", handler.joinOperation)
	handler.mux.HandleFunc("POST /planets/{planet}/fleet/operation", handler.openOperation)
	handler.mux.HandleFunc("POST /fleets/{fleet}/withdraw", handler.withdrawFromOperation)
	handler.mux.HandleFunc("POST /planets/{planet}/defense/{unit}", handler.orderDefenses)
	handler.mux.HandleFunc("GET /{$}", handler.home)
	return handler.securityHeaders(requestID(requestLogger(dependencies.Logger, handler.mux))), nil
}

// registrationOpen reports whether the universe currently accepts players. A
// closed or unavailable universe simply hides the whole registration path.
func (h *Handler) registrationOpen(ctx context.Context) bool {
	if h.registration == nil {
		return false
	}
	policy, err := h.registration.Policy(ctx)
	return err == nil && policy == appregistration.PolicyOpen
}

func (h *Handler) registerPage(response http.ResponseWriter, request *http.Request) {
	if !h.registrationOpen(request.Context()) {
		http.NotFound(response, request)
		return
	}
	token, ok := h.ensureCSRF(response, request)
	if !ok {
		return
	}
	h.render(response, http.StatusOK, "register", pageData{pageShell{CSRFToken: token}})
}

func (h *Handler) register(response http.ResponseWriter, request *http.Request) {
	if !h.registrationOpen(request.Context()) {
		http.NotFound(response, request)
		return
	}
	if !h.validCSRF(response, request) {
		return
	}
	username := request.PostFormValue("username")
	password := request.PostFormValue("password")
	if password != request.PostFormValue("password_confirmation") {
		h.render(response, http.StatusBadRequest, "register", pageData{pageShell{
			CSRFToken: request.PostFormValue("csrf_token"),
			Error:     "Les deux mots de passe doivent être identiques.",
		}})
		return
	}
	if _, err := h.registration.Register(request.Context(), username, password); err != nil {
		// The same neutral message covers every refusal so the form never
		// reveals which usernames already exist.
		h.render(response, http.StatusBadRequest, "register", pageData{pageShell{
			CSRFToken: request.PostFormValue("csrf_token"),
			Error:     "Inscription impossible avec ces informations. Choisissez un autre identifiant de 3 à 32 caractères (a-z, 0-9, _) et un mot de passe d'au moins 12 caractères.",
		}})
		return
	}
	http.Redirect(response, request, "/login?registered=1", http.StatusSeeOther)
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
	h.render(response, http.StatusOK, "login", loginPageData{
		pageShell:        pageShell{CSRFToken: token},
		RegistrationOpen: h.registrationOpen(request.Context()),
		Registered:       request.URL.Query().Get("registered") == "1",
	})
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
			h.render(response, http.StatusUnauthorized, "login", pageData{
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
	h.render(response, http.StatusOK, "password-change", pageData{pageShell{CSRFToken: token}})
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
		h.render(response, http.StatusBadRequest, "password-change", pageData{pageShell{CSRFToken: csrfCookie.Value, Error: message}})
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
		h.render(response, http.StatusOK, "empire", pageData{pageShell{CSRFToken: token}})
		return
	}
	if err != nil {
		http.Error(response, "economy unavailable", http.StatusInternalServerError)
		return
	}
	h.renderOverview(response, request, http.StatusOK, principal, planets)
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
	planets, planet, choices, err := h.planetView(request.Context(), principal, planetID)
	if errors.Is(err, appeconomy.ErrPlanetNotFound) || errors.Is(err, appeconomy.ErrNoEmpire) {
		http.NotFound(response, request)
		return
	}
	if err != nil {
		http.Error(response, "economy unavailable", http.StatusInternalServerError)
		return
	}
	h.renderEconomy(response, request, http.StatusOK, principal, planets, planet, choices, "")
}

// planetView loads the bodies of the account and the selected planet, so the
// navigation shell and the page itself always agree.
func (h *Handler) planetView(ctx context.Context, principal appauth.Principal, planetID int64) ([]appeconomy.Planet, appeconomy.Planet, []appeconomy.BuildingChoice, error) {
	planets, err := h.economy.Planets(ctx, principal)
	if err != nil {
		return nil, appeconomy.Planet{}, nil, err
	}
	planet, choices, err := h.economy.Buildings(ctx, principal, planetID)
	if err != nil {
		return nil, appeconomy.Planet{}, nil, err
	}
	return planets, planet, choices, nil
}

// switchBody redirects the body selector to the chosen planet so the selector
// works without JavaScript.
func (h *Handler) switchBody(response http.ResponseWriter, request *http.Request) {
	if _, _, ok := h.requirePrincipal(response, request); !ok {
		return
	}
	planetID, err := strconv.ParseInt(request.URL.Query().Get("planet"), 10, 64)
	if err != nil || planetID <= 0 {
		http.Redirect(response, request, "/", http.StatusSeeOther)
		return
	}
	http.Redirect(response, request, fmt.Sprintf("/planets/%d", planetID), http.StatusSeeOther)
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
		h.render(response, status, "empire", pageData{pageShell{CSRFToken: request.PostFormValue("csrf_token"), Error: "Impossible de créer cet empire."}})
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
		planets, planet, choices, loadErr := h.planetView(request.Context(), principal, planetID)
		if loadErr != nil {
			http.Error(response, "construction unavailable", http.StatusBadRequest)
			return
		}
		h.renderEconomy(response, request, http.StatusBadRequest, principal, planets, planet, choices, buildingError(err))
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
	pageShell
	Planets []appeconomy.Planet
}

func (h *Handler) renderOverview(response http.ResponseWriter, request *http.Request, status int, principal appauth.Principal, planets []appeconomy.Planet) {
	token, ok := h.ensureCSRF(response, request)
	if !ok {
		return
	}
	shell := h.gameShell(request.Context(), token, principal, "overview", planets, 0)
	h.render(response, status, "overview", overviewPageData{pageShell: shell, Planets: planets})
}

type economyPageData struct {
	pageShell
	Planet  appeconomy.Planet
	Choices []buildingPageChoice
}

func (h *Handler) renderEconomy(response http.ResponseWriter, request *http.Request, status int, principal appauth.Principal, planets []appeconomy.Planet, planet appeconomy.Planet, choices []appeconomy.BuildingChoice, message string) {
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
	shell := h.gameShell(request.Context(), token, principal, "planet", planets, planet.ID)
	shell.Error = message
	h.render(response, status, "economy", economyPageData{pageShell: shell, Planet: planet, Choices: views})
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

// shipCatalogue exposes the ship definitions in catalogue order for the views.
func (h *Handler) shipCatalogue() []unit.Definition {
	return unit.DefaultCatalogue().Definitions(unit.Ship)
}

func (h *Handler) render(response http.ResponseWriter, status int, name string, data any) {
	page, known := h.pages[name]
	if !known {
		http.Error(response, "page unavailable", http.StatusInternalServerError)
		return
	}
	root := "layout"
	if slices.Contains(plainPages, name) {
		root = "shell"
	}
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.WriteHeader(status)
	_ = page.ExecuteTemplate(response, root, data)
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

// pageShell carries everything the layout needs, whatever the page shows.
type pageShell struct {
	CSRFToken string
	Error     string
	Username  string
	Section   string
	Bodies    []bodyLink
	Current   *bodyLink
	Now       time.Time
	Alerts    int
}

// bodyLink is one entry of the celestial body selector.
type bodyLink struct {
	ID         int64
	Name       string
	Coordinate string
	Kind       string
	IsMoon     bool
	Current    bool
}

type pageData struct {
	pageShell
}

// loginPageData adds the registration affordances of the login screen.
type loginPageData struct {
	pageShell
	RegistrationOpen bool
	Registered       bool
}

// gameShell builds the navigation shell from the bodies of the account.
func (h *Handler) gameShell(ctx context.Context, token string, principal appauth.Principal, section string, planets []appeconomy.Planet, currentID int64) pageShell {
	shell := pageShell{CSRFToken: token, Username: principal.Username, Section: section, Now: h.clock()}
	if h.reports != nil {
		if alerts, err := h.reports.UnreadHostile(ctx, principal); err == nil {
			shell.Alerts = alerts
		}
	}
	for _, planet := range planets {
		link := bodyLink{
			ID: planet.ID, Name: planet.Name, Coordinate: planet.Coordinate.String(),
			Kind: bodyKindName(planet.Kind), IsMoon: planet.Kind == building.OnMoon, Current: planet.ID == currentID,
		}
		shell.Bodies = append(shell.Bodies, link)
		if link.Current {
			current := link
			shell.Current = &current
		}
	}
	if shell.Current == nil && len(shell.Bodies) > 0 {
		current := shell.Bodies[0]
		shell.Current = &current
	}
	return shell
}

func rateLimitKey(request *http.Request, username string) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		host = request.RemoteAddr
	}
	return host + "|" + strings.ToLower(strings.TrimSpace(username))
}
