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
	domaineconomy "universeatwar/internal/domain/economy"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/research"
	"universeatwar/internal/domain/rules"
	"universeatwar/internal/domain/server"
	"universeatwar/internal/domain/unit"
	"universeatwar/internal/domain/universe"
	"universeatwar/internal/observability"
	webassets "universeatwar/web"
)

const (
	sessionCookieName = "uaw_session"
	csrfCookieName    = "uaw_csrf"
	// bodyCookieName remembers the body the player last looked at, so a screen
	// that has no planet of its own still shows the right resources.
	bodyCookieName = "uaw_body"
	maxFormBytes   = 64 << 10
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
	Profiles(context.Context, appauth.Principal) ([]rules.Profile, error)
	Apply(context.Context, appauth.Principal, int64, string) (appsetup.Draft, error)
	Import(context.Context, appauth.Principal, int64, []byte) (appsetup.Draft, error)
	Export(context.Context, appauth.Principal) ([]byte, error)
	Differences(context.Context, appauth.Principal) ([]rules.Difference, error)
}

type registrationService interface {
	Policy(context.Context) (string, error)
	Register(context.Context, string, string, string) (int64, error)
}

type economyService interface {
	CreateEmpire(context.Context, appauth.Principal, string) (appeconomy.Planet, error)
	Planets(context.Context, appauth.Principal) ([]appeconomy.Planet, error)
	Buildings(context.Context, appauth.Principal, int64) (appeconomy.Planet, []appeconomy.BuildingChoice, error)
	EnqueueBuilding(context.Context, appauth.Principal, int64, building.ID, string) (appeconomy.Queue, error)
}

type researchService interface {
	Overview(context.Context, appauth.Principal, int64) (appresearch.Overview, error)
	EnqueueResearch(context.Context, appauth.Principal, int64, research.ID, string) (appresearch.Queue, error)
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
	Artificials    artificialService
	Dashboard      dashboardService
	Invitations    invitationService
	Moderation     moderationService
	Backups        backupService
	Registration   registrationService
	Logger         *slog.Logger
	Metrics        *observability.Metrics
	SecureCookies  bool
	LoginLimiter   loginRateLimiter
}

// Handler serves the minimal bootstrap and authentication interface.
type Handler struct {
	authentication  authenticationService
	serverState     stateService
	csrfSecrets     secretGenerator
	setup           setupService
	economy         economyService
	research        researchService
	shipyard        shipyardService
	fleet           fleetService
	galaxy          galaxyService
	reports         reportsService
	phalanx         phalanxService
	jumpGate        jumpGateService
	alliance        allianceService
	acs             acsService
	artificials     artificialService
	dashboard       dashboardService
	invitations     invitationService
	moderation      moderationService
	backups         backupService
	metrics         *observability.Metrics
	registration    registrationService
	secureCookies   bool
	loginLimiter    loginRateLimiter
	registerLimiter loginRateLimiter
	clock           func() time.Time
	pages           map[string]*template.Template
	mux             *http.ServeMux
}

// gamePages share the navigation shell; the others keep a bare centred panel.
var (
	gamePages  = []string{"overview", "economy", "research", "production", "fleet", "fleet-send", "fleet-confirm", "galaxy", "reports", "report", "phalanx", "jump-gate", "alliance", "operations", "admin-ai", "admin-ai-detail", "admin", "moderation"}
	plainPages = []string{"login", "password-change", "empire", "setup", "register", "profiles", "closed"}
)

// parsePages clones the right base template per page so that every page may
// define its own "content" block without colliding with its neighbours.
func parsePages() (map[string]*template.Template, error) {
	pages := map[string]*template.Template{}
	for base, names := range map[string][]string{"layout": gamePages, "shell": plainPages} {
		// queue.html rides along with the shell so that every build screen shares
		// one queue panel instead of three drifting copies.
		sources := []string{"templates/" + base + ".html"}
		if base == "layout" {
			sources = append(sources, "templates/queue.html")
		}
		root, err := template.New(base+".html").Funcs(templateFuncs).ParseFS(webassets.Files, sources...)
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
		artificials:    dependencies.Artificials,
		dashboard:      dependencies.Dashboard,
		invitations:    dependencies.Invitations,
		moderation:     dependencies.Moderation,
		backups:        dependencies.Backups,
		registration:   dependencies.Registration,
		secureCookies:  dependencies.SecureCookies,
		loginLimiter:   limiter,
		// Signing up forgives a few typos before it starts slowing down.
		registerLimiter: &LoginLimiter{FreeAttempts: 3, now: time.Now, attempts: map[string]loginAttempt{}},
		clock:           func() time.Time { return time.Now().UTC() },
		pages:           pages,
		mux:             http.NewServeMux(),
	}
	handler.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFiles))))
	handler.mux.HandleFunc("GET /art/{category}/{slug}", handler.art)
	handler.mux.HandleFunc("GET /healthz", handler.health)
	handler.mux.HandleFunc("GET /login", handler.loginPage)
	handler.mux.HandleFunc("GET /register", handler.registerPage)
	handler.mux.HandleFunc("POST /register", handler.register)
	handler.mux.HandleFunc("POST /login", handler.login)
	handler.mux.HandleFunc("GET /password/change", handler.passwordChangePage)
	handler.mux.HandleFunc("POST /password/change", handler.passwordChange)
	handler.mux.HandleFunc("POST /logout", handler.logout)
	handler.mux.HandleFunc("GET /setup/profiles", handler.profilesPage)
	handler.mux.HandleFunc("POST /setup/profiles", handler.applyProfile)
	handler.mux.HandleFunc("POST /setup/import", handler.importProfile)
	handler.mux.HandleFunc("GET /setup/export", handler.exportProfile)
	handler.mux.HandleFunc("GET /setup/{step}", handler.setupPage)
	handler.mux.HandleFunc("POST /setup/{step}", handler.saveSetupStep)
	handler.mux.HandleFunc("POST /empire", handler.createEmpire)
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
	handler.mux.HandleFunc("GET /admin", handler.dashboardPage)
	handler.mux.HandleFunc("POST /admin/accounts/{account}/role", handler.changeRole)
	handler.mux.HandleFunc("POST /admin/accounts/{account}/status", handler.changeStatus)
	handler.mux.HandleFunc("POST /admin/backup", handler.takeBackup)
	handler.mux.HandleFunc("POST /admin/invitations", handler.createInvitation)
	handler.mux.HandleFunc("POST /admin/invitations/{invitation}/revoke", handler.revokeInvitation)
	handler.mux.HandleFunc("GET /admin/moderation", handler.moderationPage)
	handler.mux.HandleFunc("POST /admin/moderation", handler.applyBan)
	handler.mux.HandleFunc("POST /admin/moderation/{ban}/lift", handler.liftBan)
	handler.mux.HandleFunc("GET /admin/ai", handler.artificialPage)
	handler.mux.HandleFunc("POST /admin/ai", handler.createArtificial)
	handler.mux.HandleFunc("GET /admin/ai/{player}", handler.artificialDetailPage)
	handler.mux.HandleFunc("POST /admin/ai/{player}/retire", handler.retireArtificial)
	handler.mux.HandleFunc("POST /admin/ai/{player}/alliance", handler.enlistArtificial)
	handler.mux.HandleFunc("POST /planets/{planet}/defense/{unit}", handler.orderDefenses)
	handler.mux.HandleFunc("GET /{$}", handler.home)
	handler.metrics = dependencies.Metrics
	handler.mux.HandleFunc("GET /admin/metrics", handler.metricsPage)
	return handler.securityHeaders(requestID(requestLogger(dependencies.Logger, dependencies.Metrics, handler.mux))), nil
}

// registrationOpen reports whether the universe currently accepts players. A
// closed or unavailable universe simply hides the whole registration path.
func (h *Handler) registrationOpen(ctx context.Context) bool {
	return h.registrationPolicy(ctx) != ""
}

// registrationPolicy reports the policy when the universe accepts anybody at
// all, and nothing when it is closed.
func (h *Handler) registrationPolicy(ctx context.Context) string {
	if h.registration == nil {
		return ""
	}
	policy, err := h.registration.Policy(ctx)
	if err != nil {
		return ""
	}
	if policy == appregistration.PolicyOpen || policy == appregistration.PolicyInvitation {
		return policy
	}
	return ""
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
	h.render(response, http.StatusOK, "register", registerPageData{
		pageShell:  pageShell{CSRFToken: token},
		Invitation: h.registrationPolicy(request.Context()) == appregistration.PolicyInvitation,
	})
}

func (h *Handler) register(response http.ResponseWriter, request *http.Request) {
	if !h.registrationOpen(request.Context()) {
		http.NotFound(response, request)
		return
	}
	if !h.validCSRF(response, request) {
		return
	}
	// Signing up is rate limited like signing in: a closed universe must not be
	// brute forced through its invitation field.
	key := rateLimitKey(request, "register")
	if wait, allowed := h.registerLimiter.Allow(key); !allowed {
		response.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		h.renderRegistration(response, request, "Trop de tentatives. Réessayez dans un instant.")
		return
	}
	username := request.PostFormValue("username")
	password := request.PostFormValue("password")
	if password != request.PostFormValue("password_confirmation") {
		h.renderRegistration(response, request, "Les deux mots de passe doivent être identiques.")
		return
	}
	if _, err := h.registration.Register(request.Context(), username, password,
		request.PostFormValue("invitation")); err != nil {
		// The same neutral message covers every refusal, so the form never says
		// which usernames exist nor which invitations are real.
		h.registerLimiter.Failure(key)
		h.renderRegistration(response, request,
			"Inscription impossible avec ces informations. Choisissez un autre identifiant de 3 à 32 caractères (a-z, 0-9, _) et un mot de passe d'au moins 12 caractères.")
		return
	}
	h.registerLimiter.Success(key)
	http.Redirect(response, request, "/login?registered=1", http.StatusSeeOther)
}

// registerPageData is the sign-up form. It says whether an invitation is
// needed, never whether a given one exists.
type registerPageData struct {
	pageShell
	Invitation bool
}

// renderRegistration draws the form again with a message that reveals nothing.
func (h *Handler) renderRegistration(response http.ResponseWriter, request *http.Request, message string) {
	h.render(response, http.StatusBadRequest, "register", registerPageData{
		pageShell: pageShell{
			CSRFToken: request.PostFormValue("csrf_token"),
			Error:     message,
		},
		Invitation: h.registrationPolicy(request.Context()) == appregistration.PolicyInvitation,
	})
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
	http.Redirect(response, request, "/", http.StatusSeeOther)
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
	http.Redirect(response, request, "/", http.StatusSeeOther)
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
	if errors.Is(err, appsetup.ErrForbidden) {
		http.NotFound(response, request)
		return
	}
	if err != nil {
		http.Error(response, "setup unavailable", http.StatusInternalServerError)
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
	if errors.Is(err, appsetup.ErrForbidden) {
		http.NotFound(response, request)
		return
	}
	if err != nil {
		http.Error(response, "setup unavailable", http.StatusInternalServerError)
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
		// Configuring the universe is the work of an administrator. Anybody
		// else is simply told the doors are not open, rather than being sent
		// to a page that would only refuse them.
		if principal.HasRole(appauth.RoleAdmin) {
			http.Redirect(response, request, "/setup/1", http.StatusSeeOther)
			return
		}
		token, ok := h.ensureCSRF(response, request)
		if !ok {
			return
		}
		h.render(response, http.StatusOK, "closed", pageData{pageShell{
			CSRFToken: token,
			Username:  principal.Username,
		}})
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

func (h *Handler) planetParameter(response http.ResponseWriter, request *http.Request) (int64, bool) {
	planetID, err := strconv.ParseInt(request.PathValue("planet"), 10, 64)
	if err != nil || planetID <= 0 {
		http.NotFound(response, request)
		return 0, false
	}
	h.rememberBody(response, planetID)
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
	_, err := h.economy.EnqueueBuilding(request.Context(), principal, planetID, building.ID(request.PathValue("building")), request.PostFormValue("idempotency_key"))
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
	Planets []overviewBodyView
}

// overviewBodyView is one row of the empire table. Construction summarises the
// body's queue: its head plus how many orders wait behind.
type overviewBodyView struct {
	appeconomy.Planet
	Construction *overviewConstructionView
}

type overviewConstructionView struct {
	Name    string
	Detail  string
	EndsAt  string
	Waiting int
}

func (h *Handler) renderOverview(response http.ResponseWriter, request *http.Request, status int, principal appauth.Principal, planets []appeconomy.Planet) {
	token, ok := h.ensureCSRF(response, request)
	if !ok {
		return
	}
	rows := make([]overviewBodyView, 0, len(planets))
	for _, planet := range planets {
		row := overviewBodyView{Planet: planet}
		if head := planet.Queue; len(head) > 0 {
			row.Construction = &overviewConstructionView{
				Name: buildingName(head[0].Building), Detail: fmt.Sprintf("niveau %d", head[0].TargetLevel),
				EndsAt: head[0].CompletesAt.Format(clockLayout), Waiting: len(head) - 1,
			}
		}
		rows = append(rows, row)
	}
	shell := h.gameShell(request.Context(), token, principal, "overview", planets, h.rememberedBody(request))
	h.render(response, status, "overview", overviewPageData{pageShell: shell, Planets: rows})
}

type economyPageData struct {
	pageShell
	Planet  appeconomy.Planet
	Queue   queuePanel
	Choices []buildingPageChoice
}

func (h *Handler) renderEconomy(response http.ResponseWriter, request *http.Request, status int, principal appauth.Principal, planets []appeconomy.Planet, planet appeconomy.Planet, choices []appeconomy.BuildingChoice, message string) {
	token, ok := h.ensureCSRF(response, request)
	if !ok {
		return
	}
	views := make([]buildingPageChoice, 0, len(choices))
	for _, choice := range choices {
		reason := choiceReason(choice.Missing, choice.Reason,
			len(planet.Queue) >= planet.Rules.Progression.QueueLength, choice.Available && !choice.Affordable)
		views = append(views, buildingPageChoice{
			ID: choice.Definition.ID, Name: buildingName(choice.Definition.ID), Level: choice.Level,
			CostMetal: choice.Plan.Cost.Metal, CostCrystal: choice.Plan.Cost.Crystal, CostDeuterium: choice.Plan.Cost.Deuterium,
			Duration: choice.Plan.Duration, CanStart: choice.Available && choice.Affordable,
			Reason: reason, IdempotencyKey: fmt.Sprintf("%s:%s:%d", token, choice.Definition.ID, choice.Plan.TargetLevel),
		})
	}
	shell := h.gameShell(request.Context(), token, principal, "planet", planets, planet.ID)
	shell.Error = message
	h.render(response, status, "economy", economyPageData{
		pageShell: shell, Planet: planet,
		Queue: buildingQueuePanel(planet, shell.Now), Choices: views,
	})
}

func buildingError(err error) string {
	switch {
	case errors.Is(err, appeconomy.ErrQueueFull):
		return "La file de construction est pleine."
	case errors.Is(err, appeconomy.ErrQueueBusy):
		return "La file de construction vient de changer : réessayez."
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
	// Totals sum what the account owns, for the head of the bodies column.
	Totals empireTotals
	// Administrator opens the administration pages in the navigation. It never
	// grants anything by itself: every route checks the role again.
	Administrator bool
}

// bodyLink is one entry of the celestial body column: identity plus the settled
// economy of the body, which every screen shows in the shell.
type bodyLink struct {
	ID         int64
	Name       string
	Coordinate string
	Kind       string
	IsMoon     bool
	Current    bool
	// ArtSlot names the illustration of the body.
	ArtSlot string
	// Resources are metal, crystal and deuterium, in that order, so the column
	// and the bar always read the same way.
	Resources       []shellResource
	EnergyProduced  int64
	EnergyConsumed  int64
	EnergyAvailable int64
	UsedFields      int
	TotalFields     int
}

// shellResource is one figure of the resource bar, computed here rather than in
// the view: a template has no arithmetic.
type shellResource struct {
	// Slug names both the illustration slot and the colour class.
	Slug string
	// Label names the resource, Initial abbreviates it for the cramped bodies
	// column where three figures share one line each.
	Label    string
	Initial  string
	Amount   int64
	Capacity int64
	Rate     int64
	// Low and High are where the gauge turns amber then red. A template cannot
	// compute a percentage, so it is computed here.
	Low  int64
	High int64
	// Full says the store reached its ceiling and stopped earning, which the
	// interface has to shout about.
	Full bool
	// Unbounded says the body stores without limit, which is what a moon does.
	// The view then shows no capacity instead of a meaningless huge number.
	Unbounded bool
}

// empireTotals sum what the account owns. Capacity is never summed: a moon
// stores without limit, so the total would mean nothing.
type empireTotals struct {
	Resources []shellResource
	Bodies    int
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
	shell := pageShell{
		CSRFToken: token, Username: principal.Username, Section: section, Now: h.clock(),
		Administrator: principal.HasRole(appauth.RoleAdmin),
	}
	if h.reports != nil {
		if alerts, err := h.reports.UnreadHostile(ctx, principal); err == nil {
			shell.Alerts = alerts
		}
	}
	var metal, crystal, deuterium int64
	for _, planet := range planets {
		moon := planet.Kind == building.OnMoon
		link := bodyLink{
			ID: planet.ID, Name: planet.Name, Coordinate: planet.Coordinate.String(),
			Kind: bodyKindName(planet.Kind), IsMoon: moon, Current: planet.ID == currentID,
			ArtSlot:         bodyArtSlot(planet.Coordinate.Position, moon),
			Resources:       bodyResources(planet.Stock, planet.Capacity, planet.Rates, moon),
			EnergyProduced:  planet.Energy.Produced,
			EnergyConsumed:  planet.Energy.Consumed,
			EnergyAvailable: planet.Energy.Produced - planet.Energy.Consumed,
			UsedFields:      planet.UsedFields,
			TotalFields:     planet.TotalFields,
		}
		metal += planet.Stock.Metal
		crystal += planet.Stock.Crystal
		deuterium += planet.Stock.Deuterium
		shell.Bodies = append(shell.Bodies, link)
		if link.Current {
			current := link
			shell.Current = &current
		}
	}
	shell.Totals = empireTotals{
		Bodies: len(shell.Bodies),
		Resources: bodyResources(
			domaineconomy.Resources{Metal: metal, Crystal: crystal, Deuterium: deuterium},
			domaineconomy.Resources{}, domaineconomy.Rates{}, true),
	}
	if shell.Current == nil && len(shell.Bodies) > 0 {
		// The fallback body has to be marked in the list too, or the bodies
		// column would highlight nothing at all.
		shell.Bodies[0].Current = true
		current := shell.Bodies[0]
		shell.Current = &current
	}
	return shell
}

// bodyResources lays the three storable resources out in a fixed order, so the
// bar, the bodies column and the totals never disagree on it.
func bodyResources(stock, capacity domaineconomy.Resources, rates domaineconomy.Rates, unbounded bool) []shellResource {
	return []shellResource{
		storable("metal", "Métal", "M", stock.Metal, capacity.Metal, rates.Metal, unbounded),
		storable("crystal", "Cristal", "C", stock.Crystal, capacity.Crystal, rates.Crystal, unbounded),
		storable("deuterium", "Deutérium", "D", stock.Deuterium, capacity.Deuterium, rates.Deuterium, unbounded),
	}
}

func storable(slug, label, initial string, amount, capacity, rate int64, unbounded bool) shellResource {
	return shellResource{
		Slug: slug, Label: label, Initial: initial, Amount: amount, Capacity: capacity, Rate: rate,
		Low: capacity / 100 * 70, High: capacity / 100 * 90,
		Full: !unbounded && amount >= capacity, Unbounded: unbounded,
	}
}

// bodyArtSlot names the illustration of a body. A planet is drawn from its
// orbital position, the way the reference game does it: the inner orbits burn
// and the outer ones freeze.
func bodyArtSlot(position int, moon bool) string {
	if moon {
		return "moon"
	}
	return "planet-" + strconv.Itoa(position)
}

// rememberBody notes which body the player is looking at, so the shell of a page
// that has no planet of its own still shows the right resources.
func (h *Handler) rememberBody(response http.ResponseWriter, planetID int64) {
	http.SetCookie(response, &http.Cookie{
		Name: bodyCookieName, Value: strconv.FormatInt(planetID, 10), Path: "/",
		HttpOnly: true, Secure: h.secureCookies, SameSite: http.SameSiteLaxMode,
	})
}

// rememberedBody reads that note back. An unknown identifier is harmless: the
// shell only ever matches it against the bodies of the account.
func (h *Handler) rememberedBody(request *http.Request) int64 {
	cookie, err := request.Cookie(bodyCookieName)
	if err != nil {
		return 0
	}
	planetID, err := strconv.ParseInt(cookie.Value, 10, 64)
	if err != nil || planetID <= 0 {
		return 0
	}
	return planetID
}

func rateLimitKey(request *http.Request, username string) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		host = request.RemoteAddr
	}
	return host + "|" + strings.ToLower(strings.TrimSpace(username))
}
