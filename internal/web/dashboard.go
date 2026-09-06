package web

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	appadmin "universeatwar/internal/app/administration"
	appauth "universeatwar/internal/app/authentication"
	appmoderation "universeatwar/internal/app/moderation"
)

// dashboardService is the administration view of a universe.
type dashboardService interface {
	Health(context.Context, appauth.Principal) (appadmin.Health, error)
	Accounts(context.Context, appauth.Principal) ([]appadmin.Account, error)
	SetRole(context.Context, appauth.Principal, int64, string, bool) error
	SetStatus(context.Context, appauth.Principal, int64, string) error
}

// invitationService hands out the tickets into a closed universe.
type invitationService interface {
	Create(context.Context, appauth.Principal, string) (appadmin.Invitation, error)
	List(context.Context, appauth.Principal) ([]appadmin.Invitation, error)
	Revoke(context.Context, appauth.Principal, int64) error
}

// backupService puts the universe somewhere safe.
type backupService interface {
	Take(context.Context, appauth.Principal) (appadmin.Snapshot, error)
}

// moderationService applies the sanctions of a universe.
type moderationService interface {
	Apply(context.Context, appauth.Principal, appmoderation.Request) (appmoderation.Ban, error)
	Lift(context.Context, appauth.Principal, int64) error
	List(context.Context, appauth.Principal) ([]appmoderation.Ban, error)
}

// dashboardPageData is what an administrator reads first.
type dashboardPageData struct {
	pageShell
	Health      appadmin.Health
	Accounts    []appadmin.Account
	Invitations []appadmin.Invitation
	Bans        []appmoderation.Ban
	// Code is shown exactly once, right after an invitation is created.
	Code string
	// Playing warns an administrator who also owns an empire.
	Playing bool
}

func (h *Handler) dashboardPage(response http.ResponseWriter, request *http.Request) {
	principal, ok := h.requireAdministrator(response, request)
	if !ok || h.dashboard == nil {
		if ok {
			http.NotFound(response, request)
		}
		return
	}
	h.renderDashboard(response, request, http.StatusOK, principal, "", "")
}

func (h *Handler) renderDashboard(response http.ResponseWriter, request *http.Request, status int,
	principal appauth.Principal, message, code string) {
	ctx := request.Context()
	health, err := h.dashboard.Health(ctx, principal)
	if err != nil {
		http.Error(response, "administration unavailable", http.StatusInternalServerError)
		return
	}
	accounts, err := h.dashboard.Accounts(ctx, principal)
	if err != nil {
		http.Error(response, "administration unavailable", http.StatusInternalServerError)
		return
	}
	token, ok := h.ensureCSRF(response, request)
	if !ok {
		return
	}
	data := dashboardPageData{
		pageShell: h.gameShell(ctx, token, principal, "admin", nil, 0),
		Health:    health, Accounts: accounts, Code: code,
	}
	data.Error = message
	for _, account := range accounts {
		if account.ID == principal.AccountID && account.HasEmpire {
			data.Playing = true
		}
	}
	if h.invitations != nil {
		if invitations, err := h.invitations.List(ctx, principal); err == nil {
			data.Invitations = invitations
		}
	}
	if h.moderation != nil {
		if bans, err := h.moderation.List(ctx, principal); err == nil {
			data.Bans = bans
		}
	}
	h.render(response, status, "admin", data)
}

// takeBackup writes a verified snapshot from the administration page.
func (h *Handler) takeBackup(response http.ResponseWriter, request *http.Request) {
	principal, ok := h.requireAdministrator(response, request)
	if !ok || !h.validCSRF(response, request) {
		return
	}
	if h.backups == nil {
		http.NotFound(response, request)
		return
	}
	if _, err := h.backups.Take(request.Context(), principal); err != nil {
		h.renderDashboard(response, request, http.StatusBadRequest, principal,
			"La sauvegarde a échoué : "+err.Error(), "")
		return
	}
	http.Redirect(response, request, "/admin", http.StatusSeeOther)
}

func (h *Handler) changeRole(response http.ResponseWriter, request *http.Request) {
	principal, ok := h.requireAdministrator(response, request)
	if !ok || !h.validCSRF(response, request) {
		return
	}
	accountID, err := strconv.ParseInt(request.PathValue("account"), 10, 64)
	if err != nil {
		http.NotFound(response, request)
		return
	}
	granted := request.PostFormValue("granted") == "1"
	if err := h.dashboard.SetRole(request.Context(), principal, accountID,
		request.PostFormValue("role"), granted); err != nil {
		h.renderDashboard(response, request, http.StatusBadRequest, principal, administrationError(err), "")
		return
	}
	http.Redirect(response, request, "/admin", http.StatusSeeOther)
}

func (h *Handler) changeStatus(response http.ResponseWriter, request *http.Request) {
	principal, ok := h.requireAdministrator(response, request)
	if !ok || !h.validCSRF(response, request) {
		return
	}
	accountID, err := strconv.ParseInt(request.PathValue("account"), 10, 64)
	if err != nil {
		http.NotFound(response, request)
		return
	}
	if err := h.dashboard.SetStatus(request.Context(), principal, accountID,
		request.PostFormValue("status")); err != nil {
		h.renderDashboard(response, request, http.StatusBadRequest, principal, administrationError(err), "")
		return
	}
	http.Redirect(response, request, "/admin", http.StatusSeeOther)
}

func (h *Handler) createInvitation(response http.ResponseWriter, request *http.Request) {
	principal, ok := h.requireAdministrator(response, request)
	if !ok || !h.validCSRF(response, request) {
		return
	}
	if h.invitations == nil {
		http.NotFound(response, request)
		return
	}
	invitation, err := h.invitations.Create(request.Context(), principal, request.PostFormValue("label"))
	if err != nil {
		h.renderDashboard(response, request, http.StatusBadRequest, principal, administrationError(err), "")
		return
	}
	// The code is shown once, here, and never read back.
	h.renderDashboard(response, request, http.StatusOK, principal, "", invitation.Code)
}

func (h *Handler) revokeInvitation(response http.ResponseWriter, request *http.Request) {
	principal, ok := h.requireAdministrator(response, request)
	if !ok || !h.validCSRF(response, request) {
		return
	}
	if h.invitations == nil {
		http.NotFound(response, request)
		return
	}
	invitationID, err := strconv.ParseInt(request.PathValue("invitation"), 10, 64)
	if err != nil {
		http.NotFound(response, request)
		return
	}
	if err := h.invitations.Revoke(request.Context(), principal, invitationID); err != nil {
		h.renderDashboard(response, request, http.StatusBadRequest, principal, administrationError(err), "")
		return
	}
	http.Redirect(response, request, "/admin", http.StatusSeeOther)
}

// moderationPage is the sanctions page. A moderator reaches it too, which is
// why it does not go through the administrator gate.
func (h *Handler) moderationPage(response http.ResponseWriter, request *http.Request) {
	principal, ok := h.requireModerator(response, request)
	if !ok {
		return
	}
	h.renderModeration(response, request, http.StatusOK, principal, "")
}

func (h *Handler) renderModeration(response http.ResponseWriter, request *http.Request, status int,
	principal appauth.Principal, message string) {
	bans, err := h.moderation.List(request.Context(), principal)
	if err != nil {
		http.Error(response, "moderation unavailable", http.StatusInternalServerError)
		return
	}
	token, ok := h.ensureCSRF(response, request)
	if !ok {
		return
	}
	data := moderationPageData{
		pageShell: h.gameShell(request.Context(), token, principal, "admin", nil, 0),
		Bans:      bans,
		Moderator: !principal.HasRole(appauth.RoleAdmin),
	}
	data.Error = message
	h.render(response, status, "moderation", data)
}

// moderationPageData is the sanctions page.
type moderationPageData struct {
	pageShell
	Bans []appmoderation.Ban
	// Moderator says the reader is a moderator rather than an administrator,
	// which the page states plainly.
	Moderator bool
}

func (h *Handler) applyBan(response http.ResponseWriter, request *http.Request) {
	principal, ok := h.requireModerator(response, request)
	if !ok || !h.validCSRF(response, request) {
		return
	}
	accountID, err := strconv.ParseInt(request.PostFormValue("account"), 10, 64)
	if err != nil {
		h.renderModeration(response, request, http.StatusBadRequest, principal, "Formulaire invalide.")
		return
	}
	hours, _ := strconv.Atoi(request.PostFormValue("hours"))
	if _, err := h.moderation.Apply(request.Context(), principal, appmoderation.Request{
		AccountID: accountID, Hours: hours,
		Justification: strings.TrimSpace(request.PostFormValue("justification")),
	}); err != nil {
		h.renderModeration(response, request, http.StatusBadRequest, principal, moderationError(err))
		return
	}
	http.Redirect(response, request, "/admin/moderation", http.StatusSeeOther)
}

func (h *Handler) liftBan(response http.ResponseWriter, request *http.Request) {
	principal, ok := h.requireModerator(response, request)
	if !ok || !h.validCSRF(response, request) {
		return
	}
	banID, err := strconv.ParseInt(request.PathValue("ban"), 10, 64)
	if err != nil {
		http.NotFound(response, request)
		return
	}
	if err := h.moderation.Lift(request.Context(), principal, banID); err != nil {
		h.renderModeration(response, request, http.StatusBadRequest, principal, moderationError(err))
		return
	}
	http.Redirect(response, request, "/admin/moderation", http.StatusSeeOther)
}

// requireModerator resolves somebody allowed to apply sanctions.
func (h *Handler) requireModerator(response http.ResponseWriter, request *http.Request) (appauth.Principal, bool) {
	principal, _, ok := h.requirePrincipal(response, request)
	if !ok {
		return appauth.Principal{}, false
	}
	if principal.MustChangePassword {
		http.Redirect(response, request, "/password/change", http.StatusSeeOther)
		return appauth.Principal{}, false
	}
	if h.moderation == nil ||
		(!principal.HasRole(appauth.RoleAdmin) && !principal.HasRole(appauth.RoleModerator)) {
		http.NotFound(response, request)
		return appauth.Principal{}, false
	}
	return principal, true
}

func administrationError(err error) string {
	switch {
	case errors.Is(err, appadmin.ErrAccountNotFound):
		return "Ce compte n'existe pas."
	case errors.Is(err, appadmin.ErrInvitationNotFound):
		return "Cette invitation n'existe plus."
	case errors.Is(err, appadmin.ErrForbidden):
		return "Vous ne pouvez pas retirer vos propres droits."
	default:
		return "Action impossible."
	}
}

func moderationError(err error) string {
	switch {
	case errors.Is(err, appmoderation.ErrProtected):
		return "Ce compte est hors de portée d'un modérateur."
	case errors.Is(err, appmoderation.ErrNotFound):
		return "Ce compte n'existe pas."
	case errors.Is(err, appmoderation.ErrInvalidRequest):
		return "Une sanction demande une justification."
	case errors.Is(err, appmoderation.ErrBanNotFound):
		return "Cette sanction n'est plus active."
	default:
		return "Action impossible."
	}
}

// metricsPage serves the counters of this process as plain lines. It is
// reserved for administrators: a local operator reads it, nobody else.
func (h *Handler) metricsPage(response http.ResponseWriter, request *http.Request) {
	if _, ok := h.requireAdministrator(response, request); !ok {
		return
	}
	if h.metrics == nil {
		http.NotFound(response, request)
		return
	}
	response.Header().Set("Content-Type", "text/plain; charset=utf-8")
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write([]byte(h.metrics.Read().String()))
}
