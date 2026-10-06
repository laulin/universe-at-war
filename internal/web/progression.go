package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appeconomy "universeatwar/internal/app/economy"
	appresearch "universeatwar/internal/app/research"
	appshipyard "universeatwar/internal/app/shipyard"
	domaineconomy "universeatwar/internal/domain/economy"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/prerequisite"
	"universeatwar/internal/domain/research"
	"universeatwar/internal/domain/unit"
)

// researchPageChoice is one research prepared for display.
type researchPageChoice struct {
	ID         research.ID
	Name       string
	Technology technologyView
	// Requirements lists what the technology depends on, each with the level
	// the empire reaches. Technology carries the complete descriptive sheet.
	Requirements      []requirementView
	Level             int
	TargetLevel       int
	CostMetal         int64
	CostCrystal       int64
	CostDeuterium     int64
	Energy            int64
	Duration          time.Duration
	Offered           bool
	AwaitingResources bool
	Reason            string
	IdempotencyKey    string
}

type researchPageData struct {
	pageShell
	Planet     appeconomy.Planet
	Levels     research.Levels
	Queue      queuePanel
	Laboratory int
	Choices    []researchPageChoice
}

// unitPageVolley is one rapid-fire pairing named for the player.
type unitPageVolley struct {
	Name  string
	Shots int
}

// unitPageChoice is one unit prepared for display.
type unitPageChoice struct {
	ID                unit.ID
	Name              string
	Technology        technologyView
	Owned             int64
	CostMetal         int64
	CostCrystal       int64
	CostDeuterium     int64
	Duration          time.Duration
	Maximum           int64
	Offered           bool
	AwaitingResources bool
	// OrderCeiling is the largest batch the domain accepts, which is what the
	// page counts down from when it works out what the stock covers.
	OrderCeiling   int64
	Reason         string
	IdempotencyKey string
}

type productionPageData struct {
	pageShell
	Planet  appeconomy.Planet
	Family  string
	Title   string
	Action  string
	Queue   queuePanel
	Choices []unitPageChoice
}

func (h *Handler) researchPage(response http.ResponseWriter, request *http.Request) {
	principal, planetID, ok := h.gamePageContext(response, request)
	if !ok {
		return
	}
	h.renderResearch(response, request, http.StatusOK, principal, planetID, "")
}

func (h *Handler) startResearch(response http.ResponseWriter, request *http.Request) {
	principal, _, ok := h.requirePrincipal(response, request)
	if !ok {
		return
	}
	if !h.validCSRF(response, request) {
		return
	}
	planetID, ok := h.planetParameter(response, request)
	if !ok || h.research == nil {
		if ok {
			http.NotFound(response, request)
		}
		return
	}
	_, err := h.research.EnqueueResearch(request.Context(), principal, planetID,
		research.ID(request.PathValue("research")), request.PostFormValue("idempotency_key"))
	if err == nil {
		http.Redirect(response, request, fmt.Sprintf("/planets/%d/research", planetID), http.StatusSeeOther)
		return
	}
	if notFound(err) {
		http.NotFound(response, request)
		return
	}
	h.renderResearch(response, request, http.StatusBadRequest, principal, planetID, researchError(err))
}

func (h *Handler) renderResearch(response http.ResponseWriter, request *http.Request, status int, principal appauth.Principal, planetID int64, message string) {
	if h.research == nil {
		http.NotFound(response, request)
		return
	}
	overview, err := h.research.Overview(request.Context(), principal, planetID)
	if err != nil {
		h.progressionFailure(response, request, err)
		return
	}
	planets, err := h.economy.Planets(request.Context(), principal)
	if err != nil {
		h.progressionFailure(response, request, err)
		return
	}
	token, ok := h.ensureCSRF(response, request)
	if !ok {
		return
	}
	choices := make([]researchPageChoice, 0, len(overview.Choices))
	for _, choice := range overview.Choices {
		// One key per rendered card. Levels belong to the player, so every body
		// offers the same next level at the same moment: a key naming that level
		// was handed out by two pages at once, and the store holds it per account,
		// so whichever body posted second was refused as an invalid request.
		key, keyOK := h.formKey(response, "research")
		if !keyOK {
			return
		}
		view := researchPageChoice{
			ID: choice.Definition.ID, Name: researchName(choice.Definition.ID), Level: choice.Level,
			Technology: researchTechnology(choice.Level, choice.Definition, choice.Requirements,
				overview.Planet, overview.Laboratories),
			TargetLevel: choice.Plan.TargetLevel, CostMetal: choice.Plan.Cost.Metal,
			CostCrystal: choice.Plan.Cost.Crystal, CostDeuterium: choice.Plan.Cost.Deuterium,
			Energy: choice.Plan.Energy, Duration: choice.Plan.Duration,
			Offered: choice.Available,
			// Energy never fills on its own, so a research short of it is not
			// something the page may lift however long it waits.
			AwaitingResources: choice.Available && !choice.Affordable && !choice.EnergyShort,
			Requirements:      requirementViews(choice.Requirements),
			Reason: researchChoiceReason(choice,
				len(overview.Queue) >= overview.Planet.Rules.Progression.QueueLength),
			IdempotencyKey: key,
		}
		choices = append(choices, view)
	}
	shell := h.gameShell(request.Context(), token, principal, "research", planets, planetID)
	shell.Error = message
	shell.Notice = cancellationNotice(request)
	h.render(response, status, "research", researchPageData{
		pageShell: shell, Planet: overview.Planet, Levels: overview.Levels,
		Queue:      researchQueuePanel(overview.Queue, overview.Planet, token, shell.Now),
		Laboratory: overview.Laboratories.Local, Choices: choices,
	})
}

func (h *Handler) shipyardPage(response http.ResponseWriter, request *http.Request) {
	principal, planetID, ok := h.gamePageContext(response, request)
	if !ok {
		return
	}
	h.renderProduction(response, request, http.StatusOK, principal, planetID, unit.Ship, "")
}

func (h *Handler) defensePage(response http.ResponseWriter, request *http.Request) {
	principal, planetID, ok := h.gamePageContext(response, request)
	if !ok {
		return
	}
	h.renderProduction(response, request, http.StatusOK, principal, planetID, unit.Defense, "")
}

func (h *Handler) orderShips(response http.ResponseWriter, request *http.Request) {
	h.orderUnits(response, request, unit.Ship)
}

func (h *Handler) orderDefenses(response http.ResponseWriter, request *http.Request) {
	h.orderUnits(response, request, unit.Defense)
}

func (h *Handler) orderUnits(response http.ResponseWriter, request *http.Request, family unit.Family) {
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
	if h.shipyard == nil {
		http.NotFound(response, request)
		return
	}
	quantity, parseErr := strconv.ParseInt(request.PostFormValue("quantity"), 10, 64)
	if parseErr != nil {
		h.renderProduction(response, request, http.StatusBadRequest, principal, planetID, family,
			"Indiquez une quantité entière.")
		return
	}
	_, err := h.shipyard.OrderFamily(request.Context(), principal, planetID,
		unit.ID(request.PathValue("unit")), family, quantity, request.PostFormValue("idempotency_key"))
	if err == nil {
		http.Redirect(response, request, fmt.Sprintf("/planets/%d/%s", planetID, familyPath(family)), http.StatusSeeOther)
		return
	}
	if notFound(err) {
		http.NotFound(response, request)
		return
	}
	h.renderProduction(response, request, http.StatusBadRequest, principal, planetID, family, productionError(err))
}

// unitSpeed is the speed the player's drives actually give. Whatever has no
// drive at all has no speed to show, which the card reads as an absence rather
// than as a nought.
func unitSpeed(definition unit.Definition, levels research.Levels) int64 {
	speed, err := domainfleet.UnitSpeed(definition, levels)
	if err != nil {
		return 0
	}
	return speed
}

// namedVolleys puts a player's word on each rapid-fire pairing, in the order
// the catalogue already settled.
func namedVolleys(volleys []unit.Volley) []unitPageVolley {
	named := make([]unitPageVolley, 0, len(volleys))
	for _, volley := range volleys {
		named = append(named, unitPageVolley{Name: unitName(volley.Unit), Shots: volley.Shots})
	}
	return named
}

func (h *Handler) renderProduction(response http.ResponseWriter, request *http.Request, status int, principal appauth.Principal, planetID int64, family unit.Family, message string) {
	if h.shipyard == nil {
		http.NotFound(response, request)
		return
	}
	overview, err := h.shipyard.Ships(request.Context(), principal, planetID)
	if family == unit.Defense {
		overview, err = h.shipyard.Defenses(request.Context(), principal, planetID)
	}
	if err != nil {
		h.progressionFailure(response, request, err)
		return
	}
	planets, err := h.economy.Planets(request.Context(), principal)
	if err != nil {
		h.progressionFailure(response, request, err)
		return
	}
	token, ok := h.ensureCSRF(response, request)
	if !ok {
		return
	}
	catalogue := unit.DefaultCatalogue()
	choices := make([]unitPageChoice, 0, len(overview.Choices))
	for _, choice := range overview.Choices {
		// One key per rendered card. Counting what the body had ever ordered told
		// two batches apart on one world, but two worlds that had ordered nothing
		// both stood at zero, and the store holds the key per account: the second
		// body's batch was refused as an invalid request.
		key, keyOK := h.formKey(response, "unit")
		if !keyOK {
			return
		}
		speed := unitSpeed(choice.Definition, overview.Planet.Researches)
		inflicts := namedVolleys(catalogue.RapidFireOf(choice.Definition.ID))
		suffers := namedVolleys(catalogue.RapidFireAgainst(choice.Definition.ID))
		view := unitPageChoice{
			ID: choice.Definition.ID, Name: unitName(choice.Definition.ID), Owned: choice.Owned,
			CostMetal: choice.UnitCost.Metal, CostCrystal: choice.UnitCost.Crystal,
			CostDeuterium: choice.UnitCost.Deuterium, Duration: choice.UnitDuration,
			Maximum:           choice.MaximumAffordable,
			Offered:           choice.Available,
			AwaitingResources: choice.Available && choice.MaximumAffordable == 0,
			OrderCeiling:      unit.MaximumOrderQuantity,
			Reason: choiceReason(choice.Missing, choice.Reason,
				len(overview.Queue) >= overview.Planet.Rules.Progression.QueueLength,
				choice.Available && choice.MaximumAffordable == 0),
			IdempotencyKey: key,
		}
		view.Technology = unitTechnology(choice.Definition, choice.Owned, choice.UnitCost,
			choice.UnitDuration, overview.Planet, speed, inflicts, suffers)
		choices = append(choices, view)
	}
	section := "shipyard"
	title := "Chantier spatial"
	if family == unit.Defense {
		section = "defense"
		title = "Défense"
	}
	shell := h.gameShell(request.Context(), token, principal, section, planets, planetID)
	shell.Error = message
	shell.Notice = cancellationNotice(request)
	h.render(response, status, "production", productionPageData{
		pageShell: shell, Planet: overview.Planet, Family: string(family), Title: title,
		Action: familyPath(family), Queue: productionQueuePanel(overview.Queue, overview.Planet, token, shell.Now),
		Choices: choices,
	})
}

// gamePageContext resolves the signed-in player and the planet of a game page.
func (h *Handler) gamePageContext(response http.ResponseWriter, request *http.Request) (appauth.Principal, int64, bool) {
	principal, _, ok := h.requirePrincipal(response, request)
	if !ok {
		return appauth.Principal{}, 0, false
	}
	if principal.MustChangePassword {
		http.Redirect(response, request, "/password/change", http.StatusSeeOther)
		return appauth.Principal{}, 0, false
	}
	planetID, ok := h.planetParameter(response, request)
	if !ok {
		return appauth.Principal{}, 0, false
	}
	return principal, planetID, true
}

func (h *Handler) progressionFailure(response http.ResponseWriter, request *http.Request, err error) {
	if notFound(err) {
		http.NotFound(response, request)
		return
	}
	http.Error(response, "progression unavailable", http.StatusInternalServerError)
}

// notFound reports the errors that must not prove a planet exists.
func notFound(err error) bool {
	return errors.Is(err, appeconomy.ErrPlanetNotFound) || errors.Is(err, appeconomy.ErrNoEmpire)
}

func familyPath(family unit.Family) string {
	if family == unit.Defense {
		return "defense"
	}
	return "shipyard"
}

// researchChoiceReason explains why a research cannot be started, in the order
// that matters: what is missing outranks what is merely occupied, and a card
// that lists its dependencies says nothing about them a second time. It is only
// read by a card that offers no form, so a shortfall the page is waiting on has
// no place here — that card carries a disabled button instead.
func researchChoiceReason(choice appresearch.Choice, queueFull bool) string {
	switch {
	case len(choice.Missing) > 0:
		return ""
	case choice.LaboratoryBusy:
		return "Le laboratoire est dans la file de construction : terminez-la ou annulez-la d'abord."
	case queueFull:
		return "Une autre progression est déjà en cours."
	default:
		return researchRefusal(choice.Refusal)
	}
}

// researchRefusal says why a technology has no plan at all. Left untranslated
// it would reach the player as the domain's own English, as the building page
// let a full planet do.
func researchRefusal(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, research.ErrUnknownResearch):
		return "Cette technologie ne figure pas au catalogue."
	default:
		return "Cette recherche n'est pas disponible."
	}
}

// choiceReason explains in French why an entry cannot be started.
func choiceReason(missing []prerequisite.Requirement, fallback string, busy, poor bool) string {
	if len(missing) > 0 {
		return "Prérequis manquants : " + requirementList(missing) + "."
	}
	if busy {
		return "Une autre progression est déjà en cours."
	}
	if poor {
		return "Ressources insuffisantes."
	}
	return fallback
}

func requirementList(missing []prerequisite.Requirement) string {
	names := make([]string, 0, len(missing))
	for _, requirement := range missing {
		names = append(names, requirementName(requirement))
	}
	return joinWithComma(names)
}

func joinWithComma(values []string) string {
	result := ""
	for index, value := range values {
		if index > 0 {
			result += ", "
		}
		result += value
	}
	return result
}

func researchError(err error) string {
	switch {
	case errors.Is(err, appresearch.ErrQueueFull):
		return "La file de recherche est pleine."
	case errors.Is(err, appresearch.ErrQueueBusy):
		return "La file de recherche vient de changer : réessayez."
	case errors.Is(err, appresearch.ErrLaboratoryBusy):
		return "Le laboratoire est dans la file de construction : terminez-la ou annulez-la d'abord."
	case errors.Is(err, appresearch.ErrInsufficientEnergy):
		return "Énergie disponible insuffisante."
	case errors.Is(err, domaineconomy.ErrInsufficientResources):
		return "Ressources insuffisantes."
	case errors.Is(err, prerequisite.ErrUnmet):
		return "Les prérequis de cette recherche ne sont pas remplis : la carte en donne le détail."
	case errors.Is(err, appresearch.ErrInvalidRequest):
		return "La demande de recherche est invalide."
	default:
		return "La recherche ne peut pas démarrer."
	}
}

func productionError(err error) string {
	switch {
	case errors.Is(err, appshipyard.ErrQueueFull):
		return "La file de production est pleine."
	case errors.Is(err, appshipyard.ErrQueueBusy):
		return "La file de production vient de changer : réessayez."
	case errors.Is(err, appshipyard.ErrFacilityBusy):
		return "Le chantier spatial est dans la file de construction."
	case errors.Is(err, appshipyard.ErrInvalidQuantity), errors.Is(err, unit.ErrInvalidQuantity):
		return "Indiquez une quantité comprise entre 1 et 1 000 000."
	case errors.Is(err, unit.ErrQuantityLimit):
		return "Quantité maximale déjà atteinte pour cette unité."
	case errors.Is(err, unit.ErrSiloCapacity):
		return "Le silo à missiles n'a plus de place."
	case errors.Is(err, domaineconomy.ErrInsufficientResources):
		return "Ressources insuffisantes."
	case errors.Is(err, prerequisite.ErrUnmet):
		return "Les prérequis de cette recherche ne sont pas remplis : la carte en donne le détail."
	case errors.Is(err, appshipyard.ErrWrongFamily), errors.Is(err, unit.ErrUnknownUnit):
		return "Cette unité ne se produit pas depuis cette page."
	default:
		return "La production ne peut pas démarrer."
	}
}
