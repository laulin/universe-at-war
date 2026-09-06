package web

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	appai "universeatwar/internal/app/ai"
	appalliance "universeatwar/internal/app/alliance"
	appauth "universeatwar/internal/app/authentication"
	appeconomy "universeatwar/internal/app/economy"
	domainai "universeatwar/internal/domain/ai"
	domainalliance "universeatwar/internal/domain/alliance"
)

// artificialService is the administration of the server-driven players.
type artificialService interface {
	Create(ctx context.Context, principal appauth.Principal, request appai.Request) (appai.Profile, error)
	Retire(ctx context.Context, principal appauth.Principal, playerID int64) error
	Enlist(ctx context.Context, principal appauth.Principal, playerID int64, name, tag string) error
	List(ctx context.Context, principal appauth.Principal) ([]appai.Profile, error)
	Inspect(ctx context.Context, principal appauth.Principal, playerID int64) (appai.Profile, error)
}

// artificialPageData lists the artificial players of the universe.
type artificialPageData struct {
	pageShell
	Players    []appai.Profile
	Archetypes []domainai.Archetype
}

// artificialDetailPageData is the omniscient view of one artificial player. It
// is reserved for administrators and never served to a player.
type artificialDetailPageData struct {
	pageShell
	Player appai.Profile
}

// requireAdministrator resolves the caller and refuses anybody who is not an
// administrator by pretending the page does not exist.
func (h *Handler) requireAdministrator(response http.ResponseWriter, request *http.Request) (appauth.Principal, bool) {
	principal, _, ok := h.requirePrincipal(response, request)
	if !ok {
		return appauth.Principal{}, false
	}
	if principal.MustChangePassword {
		http.Redirect(response, request, "/password/change", http.StatusSeeOther)
		return appauth.Principal{}, false
	}
	if !principal.HasRole(appauth.RoleAdmin) {
		http.NotFound(response, request)
		return appauth.Principal{}, false
	}
	return principal, true
}

func (h *Handler) artificialPage(response http.ResponseWriter, request *http.Request) {
	principal, ok := h.requireAdministrator(response, request)
	if !ok || h.artificials == nil {
		if ok {
			http.NotFound(response, request)
		}
		return
	}
	h.renderArtificials(response, request, http.StatusOK, principal, "")
}

func (h *Handler) renderArtificials(response http.ResponseWriter, request *http.Request, status int,
	principal appauth.Principal, message string) {
	players, err := h.artificials.List(request.Context(), principal)
	if err != nil {
		http.Error(response, "artificial players unavailable", http.StatusInternalServerError)
		return
	}
	planets, err := h.economy.Planets(request.Context(), principal)
	if err != nil && !errors.Is(err, appeconomy.ErrNoEmpire) {
		http.Error(response, "artificial players unavailable", http.StatusInternalServerError)
		return
	}
	token, ok := h.ensureCSRF(response, request)
	if !ok {
		return
	}
	data := artificialPageData{
		pageShell:  h.gameShell(request.Context(), token, principal, "admin", planets, 0),
		Players:    players,
		Archetypes: domainai.Archetypes(),
	}
	data.Error = message
	h.render(response, status, "admin-ai", data)
}

func (h *Handler) createArtificial(response http.ResponseWriter, request *http.Request) {
	principal, ok := h.requireAdministrator(response, request)
	if !ok {
		return
	}
	if !h.validCSRF(response, request) {
		return
	}
	start, startErr := strconv.Atoi(request.PostFormValue("start_hour"))
	end, endErr := strconv.Atoi(request.PostFormValue("end_hour"))
	minutes, intervalErr := strconv.Atoi(request.PostFormValue("interval_minutes"))
	if startErr != nil || endErr != nil || intervalErr != nil {
		h.renderArtificials(response, request, http.StatusBadRequest, principal, "Formulaire invalide.")
		return
	}
	_, err := h.artificials.Create(request.Context(), principal, appai.Request{
		Name:      strings.TrimSpace(request.PostFormValue("name")),
		Archetype: domainai.Archetype(request.PostFormValue("archetype")),
		Window:    domainai.Window{Start: start, End: end},
		Interval:  time.Duration(minutes) * time.Minute,
	})
	if err != nil {
		h.renderArtificials(response, request, http.StatusBadRequest, principal, artificialError(err))
		return
	}
	http.Redirect(response, request, "/admin/ai", http.StatusSeeOther)
}

func (h *Handler) retireArtificial(response http.ResponseWriter, request *http.Request) {
	principal, ok := h.requireAdministrator(response, request)
	if !ok {
		return
	}
	if !h.validCSRF(response, request) {
		return
	}
	playerID, err := strconv.ParseInt(request.PathValue("player"), 10, 64)
	if err != nil {
		http.NotFound(response, request)
		return
	}
	if err := h.artificials.Retire(request.Context(), principal, playerID); err != nil {
		if errors.Is(err, appai.ErrNotFound) {
			http.NotFound(response, request)
			return
		}
		h.renderArtificials(response, request, http.StatusBadRequest, principal, artificialError(err))
		return
	}
	http.Redirect(response, request, "/admin/ai", http.StatusSeeOther)
}

// enlistArtificial puts an artificial player into an alliance, founding it when
// it does not exist yet.
func (h *Handler) enlistArtificial(response http.ResponseWriter, request *http.Request) {
	principal, ok := h.requireAdministrator(response, request)
	if !ok {
		return
	}
	if !h.validCSRF(response, request) {
		return
	}
	playerID, err := strconv.ParseInt(request.PathValue("player"), 10, 64)
	if err != nil {
		http.NotFound(response, request)
		return
	}
	if err := h.artificials.Enlist(request.Context(), principal, playerID,
		strings.TrimSpace(request.PostFormValue("alliance")), strings.TrimSpace(request.PostFormValue("tag"))); err != nil {
		if errors.Is(err, appai.ErrNotFound) {
			http.NotFound(response, request)
			return
		}
		h.renderArtificials(response, request, http.StatusBadRequest, principal, artificialError(err))
		return
	}
	http.Redirect(response, request, "/admin/ai", http.StatusSeeOther)
}

// artificialDetailPage opens the diary and the memory of one artificial player.
func (h *Handler) artificialDetailPage(response http.ResponseWriter, request *http.Request) {
	principal, ok := h.requireAdministrator(response, request)
	if !ok {
		return
	}
	playerID, err := strconv.ParseInt(request.PathValue("player"), 10, 64)
	if err != nil {
		http.NotFound(response, request)
		return
	}
	player, err := h.artificials.Inspect(request.Context(), principal, playerID)
	if errors.Is(err, appai.ErrNotFound) {
		http.NotFound(response, request)
		return
	}
	if err != nil {
		http.Error(response, "artificial player unavailable", http.StatusInternalServerError)
		return
	}
	planets, err := h.economy.Planets(request.Context(), principal)
	if err != nil && !errors.Is(err, appeconomy.ErrNoEmpire) {
		http.Error(response, "artificial player unavailable", http.StatusInternalServerError)
		return
	}
	token, ok := h.ensureCSRF(response, request)
	if !ok {
		return
	}
	h.render(response, http.StatusOK, "admin-ai-detail", artificialDetailPageData{
		pageShell: h.gameShell(request.Context(), token, principal, "admin", planets, 0),
		Player:    player,
	})
}

// artificialError turns a refusal into a sentence an administrator can act on.
func artificialError(err error) string {
	switch {
	case errors.Is(err, appai.ErrNameTaken):
		return "Ce nom est déjà pris."
	case errors.Is(err, appai.ErrInvalidRequest):
		return "Un nom compte de 3 à 32 caractères."
	case errors.Is(err, domainai.ErrUnknownArchetype):
		return "Archétype inconnu."
	case errors.Is(err, domainai.ErrInvalidWindow):
		return "Les heures d'activité vont de 0 à 23."
	case errors.Is(err, domainai.ErrInvalidInterval):
		return "L'intervalle de réflexion doit être positif."
	case errors.Is(err, appeconomy.ErrUniverseFull):
		return "L'univers est plein : aucune position libre."
	case errors.Is(err, appalliance.ErrNameTaken):
		return "Ce nom ou cette étiquette d'alliance est déjà pris."
	case errors.Is(err, appalliance.ErrAlreadyAMember):
		return "Ce joueur appartient déjà à une alliance."
	case errors.Is(err, appalliance.ErrFull):
		return "Cette alliance est complète."
	case errors.Is(err, domainalliance.ErrInvalidTag):
		return "Une étiquette compte de 2 à 8 caractères parmi A-Z et 0-9."
	default:
		return "Création impossible."
	}
}
