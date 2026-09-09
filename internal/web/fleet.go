package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appeconomy "universeatwar/internal/app/economy"
	appfleet "universeatwar/internal/app/fleet"
	domaineconomy "universeatwar/internal/domain/economy"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/unit"
	"universeatwar/internal/domain/universe"
)

// fleetPageShip is one stationed ship prepared for display.
type fleetPageShip struct {
	ID    unit.ID
	Name  string
	Owned int64
}

// fleetPageMission is one mission in flight prepared for display.
type fleetPageMission struct {
	ID          int64
	Mission     string
	State       string
	Origin      string
	Target      string
	Composition string
	Cargo       domaineconomy.Resources
	ArrivesAt   time.Time
	ArrivesISO  string
	ReturnsAt   *time.Time
	ReturnsISO  string
	Recallable  bool
}

type fleetPageData struct {
	pageShell
	Planet   appeconomy.Planet
	Ships    []fleetPageShip
	Slots    int
	Used     int
	Missions []fleetPageMission
}

type fleetSendPageData struct {
	pageShell
	Planet   appeconomy.Planet
	Ships    []fleetPageShip
	Missions []fleetPageMissionChoice
	Speeds   []int
	Form     fleetForm
}

// fleetPageMissionChoice is one entry of the mission list of the send form.
type fleetPageMissionChoice struct {
	ID   string
	Name string
}

// sendableMissions are the missions the send form offers, in the order it shows
// them. A jump belongs to a gate rather than to a flight, so it is not among
// them, and the domain does not hold it valid here either.
var sendableMissions = []domainfleet.Mission{
	domainfleet.MissionTransport, domainfleet.MissionDeploy, domainfleet.MissionHold,
	domainfleet.MissionAttack, domainfleet.MissionEspionage, domainfleet.MissionRecycle,
	domainfleet.MissionColonize, domainfleet.MissionExpedition,
}

// fleetSpeeds are the shares of full speed a mission may fly at.
var fleetSpeeds = []int{100, 90, 80, 70, 60, 50, 40, 30, 20, 10}

// missionChoices names every sendable mission for the form.
func missionChoices() []fleetPageMissionChoice {
	choices := make([]fleetPageMissionChoice, 0, len(sendableMissions))
	for _, mission := range sendableMissions {
		choices = append(choices, fleetPageMissionChoice{ID: string(mission), Name: missionName(mission)})
	}
	return choices
}

type fleetConfirmPageData struct {
	pageShell
	Planet        appeconomy.Planet
	Form          fleetForm
	Plan          domainfleet.Plan
	ArrivesISO    string
	ReturnsISO    string
	MissionName   string
	LaunchKey     string
	OperationKey  string
	GroupedAttack bool
}

// fleetForm is the transport shape of the send wizard, kept as typed values so
// the confirmation step can repost exactly what was previewed.
type fleetForm struct {
	Galaxy      int
	System      int
	Position    int
	Mission     string
	Speed       int
	Composition map[unit.ID]int64
	CargoMetal  int64
	Crystal     int64
	Deuterium   int64
	HoldUntil   string
}

func (h *Handler) fleetPage(response http.ResponseWriter, request *http.Request) {
	principal, planetID, ok := h.gamePageContext(response, request)
	if !ok {
		return
	}
	h.renderFleet(response, request, http.StatusOK, principal, planetID, "")
}

func (h *Handler) renderFleet(response http.ResponseWriter, request *http.Request, status int, principal appauth.Principal, planetID int64, message string) {
	if h.fleet == nil {
		http.NotFound(response, request)
		return
	}
	overview, err := h.fleet.Overview(request.Context(), principal, planetID)
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
	shell := h.gameShell(request.Context(), token, principal, "fleet", planets, planetID)
	shell.Error = message
	missions := make([]fleetPageMission, 0, len(overview.Fleets))
	for _, mission := range overview.Fleets {
		view := fleetPageMission{
			ID: mission.ID, Mission: missionName(mission.Mission), State: fleetStateName(mission.State),
			Origin: mission.Origin.String(), Target: mission.Target.String(),
			Composition: compositionSummary(mission.Composition), Cargo: mission.Cargo,
			ArrivesAt: mission.ArrivesAt, ArrivesISO: mission.ArrivesAt.Format(time.RFC3339),
			ReturnsAt: mission.ReturnsAt, Recallable: mission.Recallable,
		}
		if mission.ReturnsAt != nil {
			view.ReturnsISO = mission.ReturnsAt.Format(time.RFC3339)
		}
		missions = append(missions, view)
	}
	h.render(response, status, "fleet", fleetPageData{
		pageShell: shell, Planet: overview.Planet, Ships: stationedShips(overview.Stationed, h.shipCatalogue()),
		Slots: overview.Slots, Used: overview.Used, Missions: missions,
	})
}

func (h *Handler) fleetSendPage(response http.ResponseWriter, request *http.Request) {
	principal, planetID, ok := h.gamePageContext(response, request)
	if !ok {
		return
	}
	form := fleetForm{Speed: 100, Mission: string(domainfleet.MissionTransport)}
	form.Galaxy, _ = strconv.Atoi(request.URL.Query().Get("galaxy"))
	form.System, _ = strconv.Atoi(request.URL.Query().Get("system"))
	form.Position, _ = strconv.Atoi(request.URL.Query().Get("position"))
	h.renderFleetSend(response, request, http.StatusOK, principal, planetID, form, "")
}

func (h *Handler) renderFleetSend(response http.ResponseWriter, request *http.Request, status int, principal appauth.Principal, planetID int64, form fleetForm, message string) {
	if h.fleet == nil {
		http.NotFound(response, request)
		return
	}
	overview, err := h.fleet.Overview(request.Context(), principal, planetID)
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
	shell := h.gameShell(request.Context(), token, principal, "fleet", planets, planetID)
	shell.Error = message
	h.render(response, status, "fleet-send", fleetSendPageData{
		pageShell: shell, Planet: overview.Planet,
		Ships:    stationedShips(overview.Stationed, h.shipCatalogue()),
		Missions: missionChoices(), Speeds: fleetSpeeds, Form: form,
	})
}

// previewFleet validates the wizard and shows the exact mission before it is
// committed. Nothing is written at this point.
func (h *Handler) previewFleet(response http.ResponseWriter, request *http.Request) {
	principal, _, ok := h.requirePrincipal(response, request)
	if !ok {
		return
	}
	if !h.validCSRF(response, request) {
		return
	}
	planetID, ok := h.planetParameter(response, request)
	if !ok || h.fleet == nil {
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
	plan, err := h.fleet.Preview(request.Context(), principal, planetID, launchRequest)
	if err != nil {
		if notFound(err) {
			http.NotFound(response, request)
			return
		}
		h.renderFleetSend(response, request, http.StatusBadRequest, principal, planetID, form, fleetError(err))
		return
	}
	planets, err := h.economy.Planets(request.Context(), principal)
	if err != nil {
		h.progressionFailure(response, request, err)
		return
	}
	overview, err := h.fleet.Overview(request.Context(), principal, planetID)
	if err != nil {
		h.progressionFailure(response, request, err)
		return
	}
	token, ok := h.ensureCSRF(response, request)
	if !ok {
		return
	}
	// The two forms of the confirmation are two different decisions, and a grouped
	// attack reaches the same launch through the alliance. One key for both would
	// let the second button replay the first launch instead of doing its own work.
	launchKey, ok := h.formKey(response, "launch")
	if !ok {
		return
	}
	operationKey, ok := h.formKey(response, "acs-open")
	if !ok {
		return
	}
	shell := h.gameShell(request.Context(), token, principal, "fleet", planets, planetID)
	data := fleetConfirmPageData{
		pageShell: shell, Planet: overview.Planet, Form: form, Plan: plan,
		ArrivesISO: plan.ArrivesAt.Format(time.RFC3339), MissionName: missionName(domainfleet.Mission(form.Mission)),
		LaunchKey: launchKey, OperationKey: operationKey,
	}
	if plan.ReturnsAt != nil {
		data.ReturnsISO = plan.ReturnsAt.Format(time.RFC3339)
	}
	data.GroupedAttack = domainfleet.Mission(form.Mission) == domainfleet.MissionAttack &&
		h.inAnAlliance(request.Context(), principal)
	h.render(response, http.StatusOK, "fleet-confirm", data)
}

func (h *Handler) launchFleet(response http.ResponseWriter, request *http.Request) {
	principal, _, ok := h.requirePrincipal(response, request)
	if !ok {
		return
	}
	if !h.validCSRF(response, request) {
		return
	}
	planetID, ok := h.planetParameter(response, request)
	if !ok || h.fleet == nil {
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
	if _, err := h.fleet.Launch(request.Context(), principal, planetID, launchRequest, request.PostFormValue("idempotency_key")); err != nil {
		if notFound(err) {
			http.NotFound(response, request)
			return
		}
		h.renderFleetSend(response, request, http.StatusBadRequest, principal, planetID, form, fleetError(err))
		return
	}
	http.Redirect(response, request, fmt.Sprintf("/planets/%d/fleet", planetID), http.StatusSeeOther)
}

func (h *Handler) recallFleet(response http.ResponseWriter, request *http.Request) {
	principal, _, ok := h.requirePrincipal(response, request)
	if !ok {
		return
	}
	if !h.validCSRF(response, request) {
		return
	}
	if h.fleet == nil {
		http.NotFound(response, request)
		return
	}
	fleetID, err := strconv.ParseInt(request.PathValue("fleet"), 10, 64)
	if err != nil || fleetID <= 0 {
		http.NotFound(response, request)
		return
	}
	recalled, err := h.fleet.Recall(request.Context(), principal, fleetID, request.PostFormValue("idempotency_key"))
	if errors.Is(err, appfleet.ErrNotFound) {
		http.NotFound(response, request)
		return
	}
	if err != nil {
		// The fleet exists but can no longer be recalled: show the fleet page.
		http.Redirect(response, request, "/", http.StatusSeeOther)
		return
	}
	http.Redirect(response, request, fmt.Sprintf("/planets/%d/fleet", recalled.OriginID), http.StatusSeeOther)
}

// parseFleetForm reads the send wizard without trusting any of its values.
func parseFleetForm(request *http.Request) (fleetForm, error) {
	form := fleetForm{Composition: map[unit.ID]int64{}}
	var err error
	if form.Galaxy, err = strconv.Atoi(request.PostFormValue("galaxy")); err != nil {
		return form, err
	}
	if form.System, err = strconv.Atoi(request.PostFormValue("system")); err != nil {
		return form, err
	}
	if form.Position, err = strconv.Atoi(request.PostFormValue("position")); err != nil {
		return form, err
	}
	if form.Speed, err = strconv.Atoi(request.PostFormValue("speed")); err != nil {
		return form, err
	}
	form.Mission = request.PostFormValue("mission")
	form.CargoMetal = optionalQuantity(request, "cargo_metal")
	form.Crystal = optionalQuantity(request, "cargo_crystal")
	form.Deuterium = optionalQuantity(request, "cargo_deuterium")
	form.Composition = parseComposition(request)
	form.HoldUntil = strings.TrimSpace(request.PostFormValue("hold_until"))
	return form, nil
}

// parseComposition reads the ship counts of a form, ignoring anything that is
// not a positive quantity of a known field.
func parseComposition(request *http.Request) map[unit.ID]int64 {
	composition := map[unit.ID]int64{}
	for name, values := range request.PostForm {
		id, found := strings.CutPrefix(name, "composition[")
		if !found || !strings.HasSuffix(id, "]") || len(values) == 0 {
			continue
		}
		quantity, err := strconv.ParseInt(values[0], 10, 64)
		if err != nil || quantity <= 0 {
			continue
		}
		composition[unit.ID(strings.TrimSuffix(id, "]"))] = quantity
	}
	return composition
}

func optionalQuantity(request *http.Request, name string) int64 {
	value, err := strconv.ParseInt(request.PostFormValue(name), 10, 64)
	if err != nil || value < 0 {
		return 0
	}
	return value
}

// request converts the form into the application command.
func (f fleetForm) request() (appfleet.LaunchRequest, error) {
	mission := domainfleet.Mission(f.Mission)
	if !mission.Valid() {
		return appfleet.LaunchRequest{}, domainfleet.ErrInvalidMission
	}
	composition := domainfleet.Composition{}
	for id, quantity := range f.Composition {
		composition[id] = quantity
	}
	launch := appfleet.LaunchRequest{
		Target:      universe.Coordinate{Galaxy: f.Galaxy, System: f.System, Position: f.Position},
		TargetKind:  mission.Target(),
		Mission:     mission,
		Composition: composition,
		Cargo:       domaineconomy.Resources{Metal: f.CargoMetal, Crystal: f.Crystal, Deuterium: f.Deuterium},
		Percent:     f.Speed,
	}
	if mission.Defends() {
		until, err := parseHold(f.HoldUntil)
		if err != nil {
			return appfleet.LaunchRequest{}, err
		}
		launch.HoldUntil = until
	}
	return launch, nil
}

// parseHold reads what a datetime-local field sent. Browsers disagree on whether
// such a field carries its seconds, and a mission must not be refused over the
// half of the value the player never typed.
func parseHold(value string) (time.Time, error) {
	for _, layout := range []string{"2006-01-02T15:04", "2006-01-02T15:04:05"} {
		if until, err := time.Parse(layout, value); err == nil {
			return until.UTC(), nil
		}
	}
	return time.Time{}, domainfleet.ErrInvalidHold
}

func sortedUnitIDs(composition map[unit.ID]int64) []unit.ID {
	identifiers := make([]unit.ID, 0, len(composition))
	for id := range composition {
		identifiers = append(identifiers, id)
	}
	for first := 1; first < len(identifiers); first++ {
		for second := first; second > 0 && identifiers[second] < identifiers[second-1]; second-- {
			identifiers[second], identifiers[second-1] = identifiers[second-1], identifiers[second]
		}
	}
	return identifiers
}

// stationedShips lists the ships a planet may send, in catalogue order.
func stationedShips(inventory unit.Inventory, definitions []unit.Definition) []fleetPageShip {
	ships := make([]fleetPageShip, 0, len(definitions))
	for _, definition := range definitions {
		quantity := inventory[definition.ID]
		if quantity <= 0 || definition.BaseSpeed <= 0 {
			continue
		}
		ships = append(ships, fleetPageShip{ID: definition.ID, Name: unitName(definition.ID), Owned: quantity})
	}
	return ships
}

func compositionSummary(composition domainfleet.Composition) string {
	summary := ""
	for _, id := range sortedUnitIDs(composition) {
		if summary != "" {
			summary += ", "
		}
		summary += fmt.Sprintf("%d × %s", composition[id], unitName(id))
	}
	return summary
}

func missionName(mission domainfleet.Mission) string {
	switch mission {
	case domainfleet.MissionTransport:
		return "Transport"
	case domainfleet.MissionDeploy:
		return "Déploiement"
	case domainfleet.MissionAttack:
		return "Attaque"
	case domainfleet.MissionEspionage:
		return "Espionnage"
	case domainfleet.MissionRecycle:
		return "Recyclage"
	case domainfleet.MissionColonize:
		return "Colonisation"
	case domainfleet.MissionHold:
		return "Défense alliée"
	case domainfleet.MissionExpedition:
		return "Expédition"
	default:
		return string(mission)
	}
}

func fleetStateName(state domainfleet.State) string {
	switch state {
	case domainfleet.Outbound:
		return "En route"
	case domainfleet.Returning:
		return "Retour"
	case domainfleet.Recalled:
		return "Rappelée"
	case domainfleet.Completed:
		return "Terminée"
	case domainfleet.Destroyed:
		return "Détruite"
	default:
		return string(state)
	}
}

func fleetError(err error) string {
	switch {
	case errors.Is(err, domainfleet.ErrEmptyComposition):
		return "Choisissez au moins un vaisseau."
	case errors.Is(err, domainfleet.ErrInsufficientUnits):
		return "Cette planète ne possède pas ces vaisseaux."
	case errors.Is(err, domainfleet.ErrCargoExceedsCapacity):
		return "Le cargo dépasse la capacité disponible après le carburant."
	case errors.Is(err, domainfleet.ErrInsufficientFuel):
		return "Deutérium insuffisant pour ce trajet."
	case errors.Is(err, domaineconomy.ErrInsufficientResources):
		return "Ressources insuffisantes pour ce cargo."
	case errors.Is(err, domainfleet.ErrNoFleetSlot):
		return "Aucun emplacement de flotte disponible."
	case errors.Is(err, domainfleet.ErrNoColonySlot):
		return "Aucun emplacement de colonie disponible : développez l'astrophysique."
	case errors.Is(err, domainfleet.ErrCompositionMismatch):
		return "Cette composition ne convient pas à cette mission."
	case errors.Is(err, domainfleet.ErrInvalidTarget):
		return "Destination invalide pour cette mission."
	case errors.Is(err, domainfleet.ErrNoExpeditionSlot):
		return "Aucun emplacement d'expédition libre : développez l'astrophysique."
	case errors.Is(err, domainfleet.ErrExpeditionsDisabled):
		return "Les expéditions sont désactivées dans cet univers."
	case errors.Is(err, domainfleet.ErrInvalidHold):
		return "La fin de garde doit tomber après l'arrivée et dans la fenêtre autorisée."
	case errors.Is(err, domainfleet.ErrInvalidSpeed):
		return "Vitesse invalide."
	case errors.Is(err, domainfleet.ErrInvalidMission), errors.Is(err, domainfleet.ErrImmobileUnit):
		return "Mission ou composition invalide."
	case errors.Is(err, appfleet.ErrNotRecallable):
		return "Cette flotte ne peut plus être rappelée."
	default:
		return "La flotte ne peut pas partir."
	}
}
