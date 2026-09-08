package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	appauth "universeatwar/internal/app/authentication"
	appeconomy "universeatwar/internal/app/economy"
	appresearch "universeatwar/internal/app/research"
	appshipyard "universeatwar/internal/app/shipyard"
	"universeatwar/internal/domain/unit"
)

// cancelBuilding drops one construction and every level of the same building
// queued above it.
func (h *Handler) cancelBuilding(response http.ResponseWriter, request *http.Request) {
	principal, planetID, entryID, ok := h.cancelContext(response, request)
	if !ok {
		return
	}
	cancellation, err := h.economy.CancelBuilding(request.Context(), principal, planetID, entryID)
	if err != nil {
		if notFound(err) {
			http.NotFound(response, request)
			return
		}
		planets, planet, choices, loadErr := h.planetView(request.Context(), principal, planetID)
		if loadErr != nil {
			http.Error(response, "construction unavailable", http.StatusBadRequest)
			return
		}
		h.renderEconomy(response, request, http.StatusBadRequest, principal, planets, planet, choices, cancelError(err))
		return
	}
	h.redirectAfterCancel(response, request, fmt.Sprintf("/planets/%d", planetID), cancellation)
}

// cancelResearch drops one research and every level of the same technology
// queued above it.
func (h *Handler) cancelResearch(response http.ResponseWriter, request *http.Request) {
	principal, planetID, entryID, ok := h.cancelContext(response, request)
	if !ok {
		return
	}
	if h.research == nil {
		http.NotFound(response, request)
		return
	}
	cancellation, err := h.research.CancelResearch(request.Context(), principal, planetID, entryID)
	if err != nil {
		if notFound(err) {
			http.NotFound(response, request)
			return
		}
		h.renderResearch(response, request, http.StatusBadRequest, principal, planetID, cancelError(err))
		return
	}
	h.redirectAfterCancel(response, request, fmt.Sprintf("/planets/%d/research", planetID), cancellation)
}

// cancelUnits drops one batch of ships or defences.
func (h *Handler) cancelUnits(response http.ResponseWriter, request *http.Request) {
	principal, planetID, entryID, ok := h.cancelContext(response, request)
	if !ok {
		return
	}
	if h.shipyard == nil {
		http.NotFound(response, request)
		return
	}
	family := unit.Ship
	if request.PostFormValue("family") == string(unit.Defense) {
		family = unit.Defense
	}
	cancellation, err := h.shipyard.CancelOrder(request.Context(), principal, planetID, entryID)
	if err != nil {
		if notFound(err) {
			http.NotFound(response, request)
			return
		}
		h.renderProduction(response, request, http.StatusBadRequest, principal, planetID, family, cancelError(err))
		return
	}
	h.redirectAfterCancel(response, request, fmt.Sprintf("/planets/%d/%s", planetID, familyPath(family)), cancellation)
}

// cancelContext runs the checks every cancellation shares: a signed-in player,
// a valid token, a planet of theirs and a queue entry identifier.
func (h *Handler) cancelContext(response http.ResponseWriter, request *http.Request) (appauth.Principal, int64, int64, bool) {
	principal, _, ok := h.requirePrincipal(response, request)
	if !ok {
		return appauth.Principal{}, 0, 0, false
	}
	if !h.validCSRF(response, request) {
		return appauth.Principal{}, 0, 0, false
	}
	planetID, ok := h.planetParameter(response, request)
	if !ok {
		return appauth.Principal{}, 0, 0, false
	}
	entryID, err := strconv.ParseInt(request.PathValue("entry"), 10, 64)
	if err != nil || entryID <= 0 {
		http.NotFound(response, request)
		return appauth.Principal{}, 0, 0, false
	}
	return principal, planetID, entryID, true
}

// redirectAfterCancel sends the player back to the screen they came from, with
// what the cancellation gave back written into the query rather than a cookie.
func (h *Handler) redirectAfterCancel(response http.ResponseWriter, request *http.Request, target string, cancellation appeconomy.Cancellation) {
	http.Redirect(response, request, target+cancellationQuery(cancellation), http.StatusSeeOther)
}

func cancelError(err error) string {
	switch {
	case errors.Is(err, appeconomy.ErrQueueEntryNotFound),
		errors.Is(err, appresearch.ErrQueueEntryNotFound),
		errors.Is(err, appshipyard.ErrQueueEntryNotFound):
		return "Cet ordre n'est plus dans la file."
	default:
		return "L'annulation n'a pas abouti."
	}
}
