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
	Update(ctx context.Context, principal appauth.Principal, playerID int64, request appai.UpdateRequest) (appai.Profile, error)
	Configured(ctx context.Context, principal appauth.Principal) (int, error)
}

// artificialPageData lists the artificial players of the universe.
type artificialPageData struct {
	pageShell
	Players    []appai.Profile
	Archetypes []domainai.Archetype
	// Configured is what the ruleset ordered, against which Players is read.
	Configured int
}

// artificialDetailPageData is the omniscient view of one artificial player. It
// is reserved for administrators and never served to a player.
type artificialDetailPageData struct {
	pageShell
	Player          appai.Profile
	Tuning          domainai.Tuning
	Archetypes      []domainai.Archetype
	IntervalMinutes int64
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
	// A universe fills up over a few minutes, so a count on its own would leave
	// an administrator wondering whether it had stalled.
	configured, err := h.artificials.Configured(request.Context(), principal)
	if err != nil {
		http.Error(response, "artificial players unavailable", http.StatusInternalServerError)
		return
	}
	data := artificialPageData{
		pageShell:  h.gameShell(request.Context(), token, principal, "admin", planets, h.rememberedBody(request)),
		Players:    players,
		Archetypes: domainai.Archetypes(),
		Configured: configured,
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
	h.renderArtificialDetail(response, request, http.StatusOK, principal, player, "")
}

func (h *Handler) renderArtificialDetail(response http.ResponseWriter, request *http.Request, status int,
	principal appauth.Principal, player appai.Profile, message string) {
	planets, err := h.economy.Planets(request.Context(), principal)
	if err != nil && !errors.Is(err, appeconomy.ErrNoEmpire) {
		http.Error(response, "artificial player unavailable", http.StatusInternalServerError)
		return
	}
	token, ok := h.ensureCSRF(response, request)
	if !ok {
		return
	}
	shell := h.gameShell(request.Context(), token, principal, "admin", planets, h.rememberedBody(request))
	shell.Error = message
	if request.URL.Query().Get("saved") == "1" {
		shell.Notice = "Le comportement de cette IA a été mis à jour."
	}
	h.render(response, status, "admin-ai-detail", artificialDetailPageData{
		pageShell: shell, Player: player, Tuning: player.Behaviour(), Archetypes: domainai.Archetypes(),
		IntervalMinutes: int64(player.Interval / time.Minute),
	})
}

func (h *Handler) updateArtificial(response http.ResponseWriter, request *http.Request) {
	principal, ok := h.requireAdministrator(response, request)
	if !ok || h.artificials == nil || !h.validCSRF(response, request) {
		return
	}
	playerID, err := strconv.ParseInt(request.PathValue("player"), 10, 64)
	if err != nil {
		http.NotFound(response, request)
		return
	}
	current, err := h.artificials.Inspect(request.Context(), principal, playerID)
	if errors.Is(err, appai.ErrNotFound) {
		http.NotFound(response, request)
		return
	}
	if err != nil {
		http.Error(response, "artificial player unavailable", http.StatusInternalServerError)
		return
	}
	version, versionErr := strconv.ParseInt(request.PostFormValue("version"), 10, 64)
	start, startErr := strconv.Atoi(request.PostFormValue("start_hour"))
	end, endErr := strconv.Atoi(request.PostFormValue("end_hour"))
	minutes, intervalErr := strconv.Atoi(request.PostFormValue("interval_minutes"))
	if versionErr != nil || startErr != nil || endErr != nil || intervalErr != nil {
		h.renderArtificialDetail(response, request, http.StatusBadRequest, principal, current, "Formulaire invalide.")
		return
	}
	var custom *domainai.Tuning
	if request.PostFormValue("mode") != "archetype" {
		parsed, parseErr := artificialTuningFromForm(request)
		if parseErr != nil {
			h.renderArtificialDetail(response, request, http.StatusBadRequest, principal, current, parseErr.Error()+".")
			return
		}
		custom = &parsed
	}
	_, err = h.artificials.Update(request.Context(), principal, playerID, appai.UpdateRequest{
		Version: version, Archetype: domainai.Archetype(request.PostFormValue("archetype")),
		Window: domainai.Window{Start: start, End: end}, Interval: time.Duration(minutes) * time.Minute,
		Custom: custom,
	})
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, appai.ErrConflict) {
			status = http.StatusConflict
			if latest, loadErr := h.artificials.Inspect(request.Context(), principal, playerID); loadErr == nil {
				current = latest
			}
		}
		h.renderArtificialDetail(response, request, status, principal, current, artificialError(err))
		return
	}
	http.Redirect(response, request, "/admin/ai/"+strconv.FormatInt(playerID, 10)+"?saved=1", http.StatusSeeOther)
}

func artificialTuningFromForm(request *http.Request) (domainai.Tuning, error) {
	float := func(name string) (float64, error) {
		value, err := strconv.ParseFloat(request.PostFormValue(name), 64)
		if err != nil {
			return 0, errors.New("Le champ " + name + " doit être un nombre")
		}
		return value, nil
	}
	var tuning domainai.Tuning
	var err error
	if tuning.Economy, err = float("economy"); err != nil {
		return domainai.Tuning{}, err
	}
	if tuning.Greed, err = float("greed"); err != nil {
		return domainai.Tuning{}, err
	}
	if tuning.Caution, err = float("caution"); err != nil {
		return domainai.Tuning{}, err
	}
	if tuning.SafetyMargin, err = float("safety_margin"); err != nil {
		return domainai.Tuning{}, err
	}
	if tuning.DefenceShare, err = float("defence_share"); err != nil {
		return domainai.Tuning{}, err
	}
	if tuning.RaidThreshold, err = float("raid_threshold"); err != nil {
		return domainai.Tuning{}, err
	}
	if tuning.Probes, err = strconv.ParseInt(request.PostFormValue("probes"), 10, 64); err != nil {
		return domainai.Tuning{}, errors.New("Le nombre de sondes doit être entier")
	}
	if tuning.SearchRadius, err = strconv.Atoi(request.PostFormValue("search_radius")); err != nil {
		return domainai.Tuning{}, errors.New("Le rayon d'exploration doit être entier")
	}
	if tuning.BatchSize, err = strconv.ParseInt(request.PostFormValue("batch_size"), 10, 64); err != nil {
		return domainai.Tuning{}, errors.New("La taille des lots doit être entière")
	}
	tuning.Fleetsave = domainai.Fleetsave(request.PostFormValue("fleetsave"))
	tuning.AttackEnabled = request.PostFormValue("attack_enabled") == "on"
	tuning.EspionageEnabled = request.PostFormValue("espionage_enabled") == "on"
	tuning.RecycleEnabled = request.PostFormValue("recycle_enabled") == "on"
	if err := tuning.Validate(); err != nil {
		return domainai.Tuning{}, err
	}
	return tuning, nil
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
	case errors.Is(err, appai.ErrConflict):
		return "Cette IA a été modifiée depuis l'ouverture du formulaire. Rechargez ses paramètres."
	case errors.Is(err, domainai.ErrInvalidTuning):
		return "Les réglages de personnalité sont hors des limites autorisées."
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
		return "Action impossible."
	}
}
