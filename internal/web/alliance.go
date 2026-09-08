package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	appacs "universeatwar/internal/app/acs"
	appalliance "universeatwar/internal/app/alliance"
	appauth "universeatwar/internal/app/authentication"
	appeconomy "universeatwar/internal/app/economy"
	appfleet "universeatwar/internal/app/fleet"
	appreports "universeatwar/internal/app/reports"
	domainacs "universeatwar/internal/domain/acs"
	domainalliance "universeatwar/internal/domain/alliance"
)

type allianceService interface {
	Profile(context.Context, appauth.Principal) (appalliance.Profile, error)
	Invitations(context.Context, appauth.Principal) ([]appalliance.Invitation, error)
	Create(context.Context, appauth.Principal, string, string, string) (appalliance.Profile, error)
	Invite(context.Context, appauth.Principal, string) error
	Accept(context.Context, appauth.Principal, int64) error
	Decline(context.Context, appauth.Principal, int64) error
	Leave(context.Context, appauth.Principal) error
	Expel(context.Context, appauth.Principal, int64) error
	Promote(context.Context, appauth.Principal, int64, domainalliance.Role) error
	Declare(context.Context, appauth.Principal, string, domainalliance.Relation) error
	Describe(context.Context, appauth.Principal, string) error
}

type acsService interface {
	Groups(context.Context, appauth.Principal) ([]appacs.Group, error)
	Group(context.Context, appauth.Principal, int64) (appacs.Group, error)
	Create(context.Context, appauth.Principal, int64, appfleet.LaunchRequest, string) (appacs.Group, error)
	Preview(context.Context, appauth.Principal, int64, int64, appfleet.LaunchRequest) (appacs.Preview, error)
	Join(context.Context, appauth.Principal, int64, int64, appfleet.LaunchRequest, string) (appacs.Group, error)
	Withdraw(context.Context, appauth.Principal, int64, string) error
}

// alliancePageData is the alliance page: who is in, who is invited, who is at
// war and what the alliance has done.
type alliancePageData struct {
	pageShell
	Profile      *appalliance.Profile
	Invitations  []appalliance.Invitation
	Shared       []reportPageSummary
	CanInvite    bool
	CanDiplomacy bool
	CanEdit      bool
	CanExpel     bool
}

// operationsPageData lists the grouped operations of the alliance, and prepares
// the one the player is looking at.
type operationsPageData struct {
	pageShell
	Groups         []appacs.Group
	Group          *appacs.Group
	Preview        *appacs.Preview
	Form           fleetForm
	Ships          []fleetPageShip
	PlanetID       int64
	IdempotencyKey string
}

// inAnAlliance reports whether the player has a team, which decides whether the
// grouped options are offered at all.
func (h *Handler) inAnAlliance(ctx context.Context, principal appauth.Principal) bool {
	if h.alliance == nil {
		return false
	}
	_, err := h.alliance.Profile(ctx, principal)
	return err == nil
}

func (h *Handler) alliancePage(response http.ResponseWriter, request *http.Request) {
	principal, _, ok := h.requirePrincipal(response, request)
	if !ok {
		return
	}
	if principal.MustChangePassword {
		http.Redirect(response, request, "/password/change", http.StatusSeeOther)
		return
	}
	h.renderAlliance(response, request, http.StatusOK, principal, "")
}

// renderAlliance draws the alliance page, with or without an alliance, and with
// the message of the action that just failed.
func (h *Handler) renderAlliance(response http.ResponseWriter, request *http.Request, status int,
	principal appauth.Principal, message string) {
	if h.alliance == nil {
		http.NotFound(response, request)
		return
	}
	ctx := request.Context()
	planets, err := h.economy.Planets(ctx, principal)
	if err != nil && !errors.Is(err, appeconomy.ErrNoEmpire) {
		http.Error(response, "alliance unavailable", http.StatusInternalServerError)
		return
	}
	token, ok := h.ensureCSRF(response, request)
	if !ok {
		return
	}
	data := alliancePageData{
		pageShell: h.gameShell(ctx, token, principal, "alliance", planets, h.rememberedBody(request)),
	}
	data.Error = message
	profile, err := h.alliance.Profile(ctx, principal)
	switch {
	case err == nil:
		data.Profile = &profile
		data.CanInvite = profile.Role.Can(domainalliance.Invite)
		data.CanExpel = profile.Role.Can(domainalliance.Expel)
		data.CanDiplomacy = profile.Diplomacy && profile.Role.Can(domainalliance.Diplomacy)
		data.CanEdit = profile.Role.Can(domainalliance.EditProfile)
		if h.reports != nil {
			if shared, sharedErr := h.reports.SharedWithAlliance(ctx, principal); sharedErr == nil {
				for _, summary := range shared {
					data.Shared = append(data.Shared, summaryView(summary))
				}
			}
		}
	case errors.Is(err, appalliance.ErrNotAMember):
		invitations, invitationErr := h.alliance.Invitations(ctx, principal)
		if invitationErr != nil && !errors.Is(invitationErr, appalliance.ErrDisabled) {
			http.Error(response, "alliance unavailable", http.StatusInternalServerError)
			return
		}
		data.Invitations = invitations
	case errors.Is(err, appalliance.ErrDisabled), errors.Is(err, appeconomy.ErrNoEmpire):
		http.NotFound(response, request)
		return
	default:
		http.Error(response, "alliance unavailable", http.StatusInternalServerError)
		return
	}
	h.render(response, status, "alliance", data)
}

// allianceAction runs one alliance command and comes back to the page, so every
// refusal is explained where the player asked for it.
func (h *Handler) allianceAction(response http.ResponseWriter, request *http.Request,
	action func(context.Context, appauth.Principal) error) {
	principal, _, ok := h.requirePrincipal(response, request)
	if !ok {
		return
	}
	if !h.validCSRF(response, request) {
		return
	}
	if h.alliance == nil {
		http.NotFound(response, request)
		return
	}
	if err := action(request.Context(), principal); err != nil {
		if errors.Is(err, appalliance.ErrDisabled) {
			http.NotFound(response, request)
			return
		}
		h.renderAlliance(response, request, http.StatusBadRequest, principal, allianceError(err))
		return
	}
	http.Redirect(response, request, "/alliance", http.StatusSeeOther)
}

func (h *Handler) createAlliance(response http.ResponseWriter, request *http.Request) {
	h.allianceAction(response, request, func(ctx context.Context, principal appauth.Principal) error {
		_, err := h.alliance.Create(ctx, principal, request.PostFormValue("name"),
			request.PostFormValue("tag"), request.PostFormValue("description"))
		return err
	})
}

func (h *Handler) inviteToAlliance(response http.ResponseWriter, request *http.Request) {
	h.allianceAction(response, request, func(ctx context.Context, principal appauth.Principal) error {
		return h.alliance.Invite(ctx, principal, request.PostFormValue("player"))
	})
}

func (h *Handler) answerInvitation(response http.ResponseWriter, request *http.Request) {
	h.allianceAction(response, request, func(ctx context.Context, principal appauth.Principal) error {
		invitationID, err := strconv.ParseInt(request.PathValue("invitation"), 10, 64)
		if err != nil {
			return appalliance.ErrNoInvitation
		}
		if request.PostFormValue("answer") == "accept" {
			return h.alliance.Accept(ctx, principal, invitationID)
		}
		return h.alliance.Decline(ctx, principal, invitationID)
	})
}

func (h *Handler) leaveAlliance(response http.ResponseWriter, request *http.Request) {
	h.allianceAction(response, request, func(ctx context.Context, principal appauth.Principal) error {
		return h.alliance.Leave(ctx, principal)
	})
}

func (h *Handler) expelMember(response http.ResponseWriter, request *http.Request) {
	h.allianceAction(response, request, func(ctx context.Context, principal appauth.Principal) error {
		playerID, err := strconv.ParseInt(request.PathValue("player"), 10, 64)
		if err != nil {
			return appalliance.ErrNoSuchPlayer
		}
		return h.alliance.Expel(ctx, principal, playerID)
	})
}

func (h *Handler) promoteMember(response http.ResponseWriter, request *http.Request) {
	h.allianceAction(response, request, func(ctx context.Context, principal appauth.Principal) error {
		playerID, err := strconv.ParseInt(request.PathValue("player"), 10, 64)
		if err != nil {
			return appalliance.ErrNoSuchPlayer
		}
		return h.alliance.Promote(ctx, principal, playerID, domainalliance.Role(request.PostFormValue("role")))
	})
}

func (h *Handler) declareRelation(response http.ResponseWriter, request *http.Request) {
	h.allianceAction(response, request, func(ctx context.Context, principal appauth.Principal) error {
		return h.alliance.Declare(ctx, principal, request.PostFormValue("tag"),
			domainalliance.Relation(request.PostFormValue("relation")))
	})
}

func (h *Handler) describeAlliance(response http.ResponseWriter, request *http.Request) {
	h.allianceAction(response, request, func(ctx context.Context, principal appauth.Principal) error {
		return h.alliance.Describe(ctx, principal, request.PostFormValue("description"))
	})
}

// shareReport puts one of the player's own reports at the disposal of their
// alliance, or takes it back.
func (h *Handler) shareReport(response http.ResponseWriter, request *http.Request) {
	principal, _, ok := h.requirePrincipal(response, request)
	if !ok {
		return
	}
	if !h.validCSRF(response, request) {
		return
	}
	if h.reports == nil {
		http.NotFound(response, request)
		return
	}
	reportID, err := strconv.ParseInt(request.PathValue("report"), 10, 64)
	if err != nil {
		http.NotFound(response, request)
		return
	}
	err = h.reports.Share(request.Context(), principal, reportID, request.PostFormValue("shared") == "1")
	switch {
	case errors.Is(err, appreports.ErrNotFound), errors.Is(err, appreports.ErrForbidden),
		errors.Is(err, appreports.ErrNotTheOwner), errors.Is(err, appreports.ErrSharingOff),
		errors.Is(err, appreports.ErrNotInAlliance):
		// A report that is not the player's own to share simply is not there.
		http.NotFound(response, request)
		return
	case err != nil:
		http.Error(response, "report unavailable", http.StatusInternalServerError)
		return
	}
	http.Redirect(response, request, fmt.Sprintf("/reports/%d", reportID), http.StatusSeeOther)
}

func (h *Handler) operationsPage(response http.ResponseWriter, request *http.Request) {
	principal, _, ok := h.requirePrincipal(response, request)
	if !ok {
		return
	}
	if principal.MustChangePassword {
		http.Redirect(response, request, "/password/change", http.StatusSeeOther)
		return
	}
	h.renderOperations(response, request, http.StatusOK, principal, 0, 0, fleetForm{}, nil, "")
}

func (h *Handler) operationPage(response http.ResponseWriter, request *http.Request) {
	principal, _, ok := h.requirePrincipal(response, request)
	if !ok {
		return
	}
	groupID, err := strconv.ParseInt(request.PathValue("group"), 10, 64)
	if err != nil {
		http.NotFound(response, request)
		return
	}
	origin, _ := strconv.ParseInt(request.URL.Query().Get("planet"), 10, 64)
	h.renderOperations(response, request, http.StatusOK, principal, groupID, origin, fleetForm{}, nil, "")
}

// renderOperations draws the operations of the alliance and, when one is asked
// for, what joining it would cost the team in time.
func (h *Handler) renderOperations(response http.ResponseWriter, request *http.Request, status int,
	principal appauth.Principal, groupID, originID int64, form fleetForm, preview *appacs.Preview, message string) {
	if h.acs == nil {
		http.NotFound(response, request)
		return
	}
	ctx := request.Context()
	groups, err := h.acs.Groups(ctx, principal)
	if err != nil {
		if operationHidden(err) {
			http.NotFound(response, request)
			return
		}
		http.Error(response, "operations unavailable", http.StatusInternalServerError)
		return
	}
	planets, err := h.economy.Planets(ctx, principal)
	if err != nil && !errors.Is(err, appeconomy.ErrNoEmpire) {
		http.Error(response, "operations unavailable", http.StatusInternalServerError)
		return
	}
	token, ok := h.ensureCSRF(response, request)
	if !ok {
		return
	}
	data := operationsPageData{
		pageShell: h.gameShell(ctx, token, principal, "alliance", planets, h.rememberedBody(request)),
		Groups:    groups, Form: form, Preview: preview,
	}
	data.Error = message
	if originID == 0 && len(planets) > 0 {
		originID = planets[0].ID
	}
	data.PlanetID = originID
	if originID > 0 && h.fleet != nil {
		overview, overviewErr := h.fleet.Overview(ctx, principal, originID)
		if overviewErr == nil {
			data.Ships = stationedShips(overview.Stationed, h.shipCatalogue())
		}
	}
	if groupID > 0 {
		group, groupErr := h.acs.Group(ctx, principal, groupID)
		if groupErr != nil {
			if operationHidden(groupErr) {
				http.NotFound(response, request)
				return
			}
			http.Error(response, "operations unavailable", http.StatusInternalServerError)
			return
		}
		data.Group = &group
		data.IdempotencyKey = fmt.Sprintf("%s:acs-join:%d:%s", token, groupID, form.signature())
	}
	h.render(response, status, "operations", data)
}

// openOperation turns a prepared attack into a grouped one, from the same form
// the confirmation page carries.
func (h *Handler) openOperation(response http.ResponseWriter, request *http.Request) {
	principal, _, ok := h.requirePrincipal(response, request)
	if !ok {
		return
	}
	if !h.validCSRF(response, request) {
		return
	}
	planetID, ok := h.planetParameter(response, request)
	if !ok || h.acs == nil {
		if ok {
			http.NotFound(response, request)
		}
		return
	}
	form, err := parseFleetForm(request)
	if err != nil {
		h.renderFleetSend(response, request, http.StatusBadRequest, principal, planetID, form, "Formulaire invalide.")
		return
	}
	launchRequest, err := form.request()
	if err != nil {
		h.renderFleetSend(response, request, http.StatusBadRequest, principal, planetID, form, fleetError(err))
		return
	}
	group, err := h.acs.Create(request.Context(), principal, planetID, launchRequest,
		request.PostFormValue("idempotency_key"))
	if err != nil {
		if operationHidden(err) {
			http.NotFound(response, request)
			return
		}
		h.renderFleetSend(response, request, http.StatusBadRequest, principal, planetID, form, operationError(err))
		return
	}
	http.Redirect(response, request, fmt.Sprintf("/alliance/operations/%d", group.ID), http.StatusSeeOther)
}

// previewOperation shows what one more fleet would do to the schedule before
// the player commits to it.
func (h *Handler) previewOperation(response http.ResponseWriter, request *http.Request) {
	h.joinOperationWith(response, request, false)
}

func (h *Handler) joinOperation(response http.ResponseWriter, request *http.Request) {
	h.joinOperationWith(response, request, true)
}

func (h *Handler) joinOperationWith(response http.ResponseWriter, request *http.Request, commit bool) {
	principal, _, ok := h.requirePrincipal(response, request)
	if !ok {
		return
	}
	if !h.validCSRF(response, request) {
		return
	}
	if h.acs == nil {
		http.NotFound(response, request)
		return
	}
	groupID, err := strconv.ParseInt(request.PathValue("group"), 10, 64)
	if err != nil {
		http.NotFound(response, request)
		return
	}
	form, err := parseFleetForm(request)
	if err != nil {
		h.renderOperations(response, request, http.StatusBadRequest, principal, groupID, 0, form, nil, "Formulaire invalide.")
		return
	}
	planetID, err := strconv.ParseInt(request.PostFormValue("planet"), 10, 64)
	if err != nil {
		h.renderOperations(response, request, http.StatusBadRequest, principal, groupID, 0, form, nil, "Formulaire invalide.")
		return
	}
	launchRequest, err := form.request()
	if err != nil {
		h.renderOperations(response, request, http.StatusBadRequest, principal, groupID, planetID, form, nil, fleetError(err))
		return
	}
	if !commit {
		preview, previewErr := h.acs.Preview(request.Context(), principal, groupID, planetID, launchRequest)
		if previewErr != nil {
			if operationHidden(previewErr) {
				http.NotFound(response, request)
				return
			}
			h.renderOperations(response, request, http.StatusBadRequest, principal, groupID, planetID, form, nil, operationError(previewErr))
			return
		}
		h.renderOperations(response, request, http.StatusOK, principal, groupID, planetID, form, &preview, "")
		return
	}
	if _, err := h.acs.Join(request.Context(), principal, groupID, planetID, launchRequest,
		request.PostFormValue("idempotency_key")); err != nil {
		if operationHidden(err) {
			http.NotFound(response, request)
			return
		}
		h.renderOperations(response, request, http.StatusBadRequest, principal, groupID, planetID, form, nil, operationError(err))
		return
	}
	http.Redirect(response, request, fmt.Sprintf("/alliance/operations/%d", groupID), http.StatusSeeOther)
}

func (h *Handler) withdrawFromOperation(response http.ResponseWriter, request *http.Request) {
	principal, _, ok := h.requirePrincipal(response, request)
	if !ok {
		return
	}
	if !h.validCSRF(response, request) {
		return
	}
	if h.acs == nil {
		http.NotFound(response, request)
		return
	}
	fleetID, err := strconv.ParseInt(request.PathValue("fleet"), 10, 64)
	if err != nil {
		http.NotFound(response, request)
		return
	}
	key := fmt.Sprintf("%s:acs-withdraw:%d", request.PostFormValue("csrf_token"), fleetID)
	if err := h.acs.Withdraw(request.Context(), principal, fleetID, key); err != nil {
		if operationHidden(err) {
			http.NotFound(response, request)
			return
		}
		h.renderOperations(response, request, http.StatusBadRequest, principal, 0, 0, fleetForm{}, nil, operationError(err))
		return
	}
	http.Redirect(response, request, "/alliance/operations", http.StatusSeeOther)
}

// operationHidden reports whether a refusal must look like a missing page: an
// operation of another alliance simply does not exist for the player.
func operationHidden(err error) bool {
	return errors.Is(err, appacs.ErrNotFound) || errors.Is(err, appacs.ErrDisabled) ||
		errors.Is(err, appacs.ErrForbidden) || errors.Is(err, domainacs.ErrNotAMember) ||
		errors.Is(err, appeconomy.ErrNoEmpire)
}

// allianceError turns a refusal into a sentence the player can act on.
func allianceError(err error) string {
	switch {
	case errors.Is(err, appalliance.ErrForbidden):
		return "Votre rang ne permet pas cette action."
	case errors.Is(err, appalliance.ErrAlreadyAMember):
		return "Ce joueur appartient déjà à une alliance."
	case errors.Is(err, appalliance.ErrNotAMember):
		return "Vous n'appartenez à aucune alliance."
	case errors.Is(err, appalliance.ErrNameTaken):
		return "Ce nom ou cette étiquette est déjà pris."
	case errors.Is(err, appalliance.ErrFull):
		return "Cette alliance est complète."
	case errors.Is(err, appalliance.ErrNoSuchPlayer):
		return "Aucun joueur de ce nom."
	case errors.Is(err, appalliance.ErrNoInvitation):
		return "Cette invitation n'existe plus."
	case errors.Is(err, appalliance.ErrInvitationEnded):
		return "Cette invitation a expiré."
	case errors.Is(err, appalliance.ErrLastFounder):
		return "Transmettez la charge avant de partir."
	case errors.Is(err, appalliance.ErrNotFound):
		return "Aucune alliance ne porte cette étiquette."
	case errors.Is(err, domainalliance.ErrInvalidName):
		return "Un nom compte de 3 à 32 caractères."
	case errors.Is(err, domainalliance.ErrInvalidTag):
		return "Une étiquette compte de 2 à 8 caractères parmi A-Z et 0-9."
	case errors.Is(err, domainalliance.ErrUnknownRole):
		return "Rang inconnu."
	case errors.Is(err, domainalliance.ErrUnknownRelation):
		return "Relation diplomatique inconnue."
	default:
		return "Action impossible."
	}
}

// operationError explains why a grouped operation refused a fleet.
func operationError(err error) string {
	switch {
	case errors.Is(err, domainacs.ErrNotForming):
		return "Cette opération n'accepte plus de flotte."
	case errors.Is(err, domainacs.ErrTooLate):
		return "L'opération est déjà arrivée."
	case errors.Is(err, domainacs.ErrGroupFull):
		return "Cette opération est complète."
	case errors.Is(err, appacs.ErrInvalidRequest):
		return "Une opération groupée est une attaque."
	default:
		return fleetError(err)
	}
}
