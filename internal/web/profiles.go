package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	appauth "universeatwar/internal/app/authentication"
	appsetup "universeatwar/internal/app/setup"
	"universeatwar/internal/domain/rules"
)

// maximumImport bounds what an administrator may paste in: a ruleset document
// is a few kilobytes, never a megabyte.
const maximumImport = 64 << 10

// profilesPageData is the page where a universe is chosen rather than typed.
type profilesPageData struct {
	pageShell
	Profiles    []rules.Profile
	Differences []rules.Difference
	Document    string
	Version     int64
	Step        int
}

func (h *Handler) profilesPage(response http.ResponseWriter, request *http.Request) {
	principal, ok := h.requireSetupAdministrator(response, request)
	if !ok {
		return
	}
	h.renderProfiles(response, request, http.StatusOK, principal, "")
}

func (h *Handler) renderProfiles(response http.ResponseWriter, request *http.Request, status int,
	principal appauth.Principal, message string) {
	ctx := request.Context()
	draft, err := h.setup.Load(ctx, principal)
	if err != nil {
		h.setupFailure(response, request, err)
		return
	}
	profiles, err := h.setup.Profiles(ctx, principal)
	if err != nil {
		h.setupFailure(response, request, err)
		return
	}
	differences, err := h.setup.Differences(ctx, principal)
	if err != nil {
		h.setupFailure(response, request, err)
		return
	}
	document, err := h.setup.Export(ctx, principal)
	if err != nil {
		h.setupFailure(response, request, err)
		return
	}
	token, ok := h.ensureCSRF(response, request)
	if !ok {
		return
	}
	data := profilesPageData{
		pageShell: pageShell{CSRFToken: token, Username: principal.Username, Now: h.clock()},
		Profiles:  profiles, Differences: differences, Document: string(document),
		Version: draft.Version, Step: draft.CurrentStep,
	}
	data.Error = message
	h.render(response, status, "profiles", data)
}

// applyProfile replaces the whole draft with one of the profiles this build
// carries. The wizard stays where it was.
func (h *Handler) applyProfile(response http.ResponseWriter, request *http.Request) {
	principal, ok := h.requireSetupAdministrator(response, request)
	if !ok {
		return
	}
	if !h.validCSRF(response, request) {
		return
	}
	version, err := strconv.ParseInt(request.PostFormValue("version"), 10, 64)
	if err != nil {
		h.renderProfiles(response, request, http.StatusBadRequest, principal, "Formulaire invalide.")
		return
	}
	if _, err := h.setup.Apply(request.Context(), principal, version, request.PostFormValue("profile")); err != nil {
		h.renderProfiles(response, request, http.StatusBadRequest, principal, profileError(err))
		return
	}
	http.Redirect(response, request, "/setup/profiles", http.StatusSeeOther)
}

// importProfile reads a ruleset written elsewhere. A document this build would
// refuse leaves the draft exactly as it was.
func (h *Handler) importProfile(response http.ResponseWriter, request *http.Request) {
	principal, ok := h.requireSetupAdministrator(response, request)
	if !ok {
		return
	}
	if !h.validCSRF(response, request) {
		return
	}
	version, err := strconv.ParseInt(request.PostFormValue("version"), 10, 64)
	if err != nil {
		h.renderProfiles(response, request, http.StatusBadRequest, principal, "Formulaire invalide.")
		return
	}
	document := request.PostFormValue("document")
	if len(document) == 0 || len(document) > maximumImport {
		h.renderProfiles(response, request, http.StatusBadRequest, principal,
			"Le document est vide ou trop volumineux.")
		return
	}
	if _, err := h.setup.Import(request.Context(), principal, version, []byte(document)); err != nil {
		h.renderProfiles(response, request, http.StatusBadRequest, principal, profileError(err))
		return
	}
	http.Redirect(response, request, "/setup/profiles", http.StatusSeeOther)
}

// exportProfile hands the current draft back as a document.
func (h *Handler) exportProfile(response http.ResponseWriter, request *http.Request) {
	principal, ok := h.requireSetupAdministrator(response, request)
	if !ok {
		return
	}
	document, err := h.setup.Export(request.Context(), principal)
	if err != nil {
		h.setupFailure(response, request, err)
		return
	}
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Content-Disposition", `attachment; filename="universe-rules.json"`)
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write(document)
}

// requireSetupAdministrator resolves an administrator allowed to configure the
// universe, and refuses everybody else without saying why.
func (h *Handler) requireSetupAdministrator(response http.ResponseWriter, request *http.Request) (appauth.Principal, bool) {
	principal, _, ok := h.requirePrincipal(response, request)
	if !ok {
		return appauth.Principal{}, false
	}
	if principal.MustChangePassword {
		http.Redirect(response, request, "/password/change", http.StatusSeeOther)
		return appauth.Principal{}, false
	}
	if h.setup == nil || !principal.HasRole(appauth.RoleAdmin) {
		http.NotFound(response, request)
		return appauth.Principal{}, false
	}
	return principal, true
}

// setupFailure turns a refusal of the setup service into an answer.
func (h *Handler) setupFailure(response http.ResponseWriter, request *http.Request, err error) {
	if errors.Is(err, appsetup.ErrAlreadyCompleted) {
		http.Redirect(response, request, "/", http.StatusSeeOther)
		return
	}
	if errors.Is(err, appsetup.ErrForbidden) {
		http.NotFound(response, request)
		return
	}
	http.Error(response, "setup unavailable", http.StatusInternalServerError)
}

// profileError turns a refusal into a sentence an administrator can act on.
func profileError(err error) string {
	switch {
	case errors.Is(err, rules.ErrUnknownProfile):
		return "Ce profil n'existe pas dans cette version."
	case errors.Is(err, rules.ErrFutureSchema):
		return "Ce document vient d'une version plus récente du jeu."
	case errors.Is(err, appsetup.ErrInvalidDocument):
		return "Ce document n'est pas un ruleset que cette version accepte."
	case errors.Is(err, appsetup.ErrConflict):
		return "La configuration a changé entre-temps : rechargez la page."
	default:
		return fmt.Sprintf("Impossible d'appliquer cette configuration : %v", err)
	}
}
