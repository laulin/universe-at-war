package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appeconomy "universeatwar/internal/app/economy"
	appjumpgate "universeatwar/internal/app/jumpgate"
	appphalanx "universeatwar/internal/app/phalanx"
	"universeatwar/internal/domain/building"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/phalanx"
	"universeatwar/internal/domain/unit"
	"universeatwar/internal/domain/universe"
)

// phalanxPageSighting is one mission the sensor timed, prepared for display.
type phalanxPageSighting struct {
	Mission    string
	Origin     string
	Target     string
	Ships      int64
	ArrivesAt  time.Time
	ArrivesISO string
	ReturnsAt  *time.Time
	ReturnsISO string
}

type phalanxPageData struct {
	pageShell
	Moon      appeconomy.Planet
	Level     int
	Radius    int
	Cost      int64
	Scanned   bool
	Target    string
	Sightings []phalanxPageSighting
	Form      phalanxForm
}

// phalanxForm remembers what the player asked to sweep.
type phalanxForm struct {
	Galaxy   int
	System   int
	Position int
}

type jumpGatePageShip struct {
	ID    unit.ID
	Name  string
	Owned int64
}

type jumpGatePageData struct {
	pageShell
	Moon         appeconomy.Planet
	Level        int
	Ready        bool
	ReadyAt      time.Time
	ReadyISO     string
	Ships        []jumpGatePageShip
	Destinations []appjumpgate.Gate
}

func (h *Handler) phalanxPage(response http.ResponseWriter, request *http.Request) {
	principal, planetID, ok := h.gamePageContext(response, request)
	if !ok {
		return
	}
	form := phalanxForm{}
	form.Galaxy, _ = strconv.Atoi(request.URL.Query().Get("galaxy"))
	form.System, _ = strconv.Atoi(request.URL.Query().Get("system"))
	form.Position, _ = strconv.Atoi(request.URL.Query().Get("position"))
	h.renderPhalanx(response, request, http.StatusOK, principal, planetID, form, nil, "")
}

// scanPhalanx sweeps one position and charges the moon for it.
func (h *Handler) scanPhalanx(response http.ResponseWriter, request *http.Request) {
	principal, _, ok := h.requirePrincipal(response, request)
	if !ok {
		return
	}
	if !h.validCSRF(response, request) {
		return
	}
	planetID, ok := h.planetParameter(response, request)
	if !ok || h.phalanx == nil {
		if ok {
			http.NotFound(response, request)
		}
		return
	}
	form := phalanxForm{}
	var err error
	if form.Galaxy, err = strconv.Atoi(request.PostFormValue("galaxy")); err == nil {
		form.System, err = strconv.Atoi(request.PostFormValue("system"))
	}
	if err == nil {
		form.Position, err = strconv.Atoi(request.PostFormValue("position"))
	}
	if err != nil {
		h.renderPhalanx(response, request, http.StatusBadRequest, principal, planetID, form, nil, "Coordonnées invalides.")
		return
	}
	scan, err := h.phalanx.Scan(request.Context(), principal,
		planetID, universe.Coordinate{Galaxy: form.Galaxy, System: form.System, Position: form.Position})
	if err != nil {
		if notFound(err) {
			http.NotFound(response, request)
			return
		}
		h.renderPhalanx(response, request, http.StatusBadRequest, principal, planetID, form, nil, phalanxError(err))
		return
	}
	h.renderPhalanx(response, request, http.StatusOK, principal, planetID, form, &scan, "")
}

func (h *Handler) renderPhalanx(response http.ResponseWriter, request *http.Request, status int,
	principal appauth.Principal, planetID int64, form phalanxForm, scan *appphalanx.Scan, message string) {
	if h.phalanx == nil {
		http.NotFound(response, request)
		return
	}
	planets, err := h.economy.Planets(request.Context(), principal)
	if err != nil {
		h.progressionFailure(response, request, err)
		return
	}
	_, moon, _, err := h.planetView(request.Context(), principal, planetID)
	if err != nil {
		h.progressionFailure(response, request, err)
		return
	}
	token, ok := h.ensureCSRF(response, request)
	if !ok {
		return
	}
	shell := h.gameShell(request.Context(), token, principal, "phalanx", planets, planetID)
	shell.Error = message
	data := phalanxPageData{
		pageShell: shell, Moon: moon, Form: form,
		Level:  moon.Levels[building.SensorPhalanx],
		Radius: phalanx.Radius(moon.Levels[building.SensorPhalanx]),
		Cost:   moon.Rules.Expansion.PhalanxScanCost,
	}
	if scan != nil {
		data.Scanned = true
		data.Moon = scan.Moon
		data.Level = scan.Level
		data.Radius = scan.Radius
		data.Cost = scan.Cost
		data.Target = scan.Target.String()
		for _, sighting := range scan.Sightings {
			view := phalanxPageSighting{
				Mission: missionName(sighting.Mission), Origin: sighting.Origin.String(),
				Target: sighting.Target.String(), Ships: sighting.Ships,
				ArrivesAt: sighting.ArrivesAt, ArrivesISO: sighting.ArrivesAt.Format(time.RFC3339),
				ReturnsAt: sighting.ReturnsAt,
			}
			if sighting.ReturnsAt != nil {
				view.ReturnsISO = sighting.ReturnsAt.Format(time.RFC3339)
			}
			data.Sightings = append(data.Sightings, view)
		}
	}
	h.render(response, status, "phalanx", data)
}

func (h *Handler) jumpGatePage(response http.ResponseWriter, request *http.Request) {
	principal, planetID, ok := h.gamePageContext(response, request)
	if !ok {
		return
	}
	h.renderJumpGate(response, request, http.StatusOK, principal, planetID, "")
}

// jumpShips moves the chosen ships to another moon of the player.
func (h *Handler) jumpShips(response http.ResponseWriter, request *http.Request) {
	principal, _, ok := h.requirePrincipal(response, request)
	if !ok {
		return
	}
	if !h.validCSRF(response, request) {
		return
	}
	planetID, ok := h.planetParameter(response, request)
	if !ok || h.jumpGate == nil {
		if ok {
			http.NotFound(response, request)
		}
		return
	}
	destination, err := strconv.ParseInt(request.PostFormValue("destination"), 10, 64)
	if err != nil {
		h.renderJumpGate(response, request, http.StatusBadRequest, principal, planetID, "Destination invalide.")
		return
	}
	composition := domainfleet.Composition{}
	for id, quantity := range parseComposition(request) {
		composition[id] = quantity
	}
	if _, err := h.jumpGate.Jump(request.Context(), principal, planetID, destination, composition,
		request.PostFormValue("idempotency_key")); err != nil {
		if notFound(err) {
			http.NotFound(response, request)
			return
		}
		h.renderJumpGate(response, request, http.StatusBadRequest, principal, planetID, jumpGateError(err))
		return
	}
	http.Redirect(response, request, fmt.Sprintf("/planets/%d/jump", planetID), http.StatusSeeOther)
}

func (h *Handler) renderJumpGate(response http.ResponseWriter, request *http.Request, status int,
	principal appauth.Principal, planetID int64, message string) {
	if h.jumpGate == nil {
		http.NotFound(response, request)
		return
	}
	overview, err := h.jumpGate.Overview(request.Context(), principal, planetID)
	if err != nil {
		if errors.Is(err, appjumpgate.ErrNotAMoon) {
			http.NotFound(response, request)
			return
		}
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
	shell := h.gameShell(request.Context(), token, principal, "jump", planets, planetID)
	shell.Error = message
	h.render(response, status, "jump-gate", jumpGatePageData{
		pageShell: shell, Moon: overview.Moon, Level: overview.Level, Ready: overview.Ready,
		ReadyAt: overview.ReadyAt, ReadyISO: overview.ReadyAt.Format(time.RFC3339),
		Ships:        stationedJumpShips(overview.Stationed, unit.DefaultCatalogue().Definitions(unit.Ship)),
		Destinations: overview.Destinations,
	})
}

func stationedJumpShips(inventory unit.Inventory, definitions []unit.Definition) []jumpGatePageShip {
	ships := make([]jumpGatePageShip, 0, len(definitions))
	for _, definition := range definitions {
		if quantity := inventory[definition.ID]; quantity > 0 && definition.BaseSpeed > 0 {
			ships = append(ships, jumpGatePageShip{ID: definition.ID, Name: unitName(definition.ID), Owned: quantity})
		}
	}
	return ships
}

func phalanxError(err error) string {
	switch {
	case errors.Is(err, phalanx.ErrNoPhalanx):
		return "Cette lune ne porte pas de phalange."
	case errors.Is(err, phalanx.ErrOutOfRange):
		return "Cette position est hors de portée de la phalange."
	case errors.Is(err, phalanx.ErrOtherGalaxy):
		return "Une phalange n'observe jamais une autre galaxie."
	case errors.Is(err, appphalanx.ErrNotAMoon):
		return "Une phalange ne se tient que sur une lune."
	default:
		return "Le balayage n'a pas pu être effectué : vérifiez le deutérium de la lune."
	}
}

func jumpGateError(err error) string {
	switch {
	case errors.Is(err, appjumpgate.ErrCoolingDown):
		return "Une des deux portes est encore en recharge."
	case errors.Is(err, appjumpgate.ErrNoGate):
		return "Les deux lunes doivent porter une porte de saut."
	case errors.Is(err, appjumpgate.ErrSameMoon):
		return "Une porte ne saute pas vers elle-même."
	case errors.Is(err, appjumpgate.ErrNotAMoon):
		return "Une porte de saut relie deux lunes."
	case errors.Is(err, domainfleet.ErrInsufficientUnits):
		return "Cette lune ne possède pas ces vaisseaux."
	case errors.Is(err, domainfleet.ErrImmobileUnit):
		return "Seuls des vaisseaux passent par une porte de saut."
	case errors.Is(err, domainfleet.ErrEmptyComposition):
		return "Choisissez au moins un vaisseau."
	default:
		return "Le saut n'a pas pu être effectué."
	}
}
