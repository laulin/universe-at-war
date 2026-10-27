package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	appauth "universeatwar/internal/app/authentication"
	appmissile "universeatwar/internal/app/missile"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/missile"
	"universeatwar/internal/domain/research"
	"universeatwar/internal/domain/unit"
	"universeatwar/internal/domain/universe"
)

type missileChoice struct {
	ID       unit.ID
	Name     string
	Selected bool
}
type missilePageData struct {
	pageShell
	Overview appmissile.Overview
	Form     appmissile.Request
	Plan     *appmissile.Plan
	Confirm  bool
	Stock    int64
	Range    int
	Key      string
	Choices  []missileChoice
}

func (h *Handler) missilePage(w http.ResponseWriter, r *http.Request) {
	principal, planetID, ok := h.gamePageContext(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	galaxy, _ := strconv.Atoi(query.Get("galaxy"))
	system, _ := strconv.Atoi(query.Get("system"))
	position, _ := strconv.Atoi(query.Get("position"))
	kind := domainfleet.TargetKind(query.Get("kind"))
	if kind == "" {
		kind = domainfleet.TargetPlanet
	}
	h.renderMissiles(w, r, http.StatusOK, principal, planetID, appmissile.Request{
		Target: universe.Coordinate{Galaxy: galaxy, System: system, Position: position}, TargetKind: kind, Count: 1,
	}, false, "")
}

func parseMissileRequest(r *http.Request) (appmissile.Request, error) {
	var request appmissile.Request
	values := []*int{&request.Target.Galaxy, &request.Target.System, &request.Target.Position}
	for i, name := range []string{"galaxy", "system", "position"} {
		value, err := strconv.Atoi(r.PostFormValue(name))
		if err != nil {
			return request, appmissile.ErrInvalid
		}
		*values[i] = value
	}
	var err error
	request.Count, err = strconv.ParseInt(r.PostFormValue("count"), 10, 64)
	request.TargetKind = domainfleet.TargetKind(r.PostFormValue("kind"))
	request.Priority = unit.ID(r.PostFormValue("priority"))
	return request, err
}

func (h *Handler) previewMissiles(w http.ResponseWriter, r *http.Request) {
	h.postMissiles(w, r, false)
}
func (h *Handler) launchMissiles(w http.ResponseWriter, r *http.Request) { h.postMissiles(w, r, true) }

func (h *Handler) postMissiles(w http.ResponseWriter, r *http.Request, launch bool) {
	principal, planetID, ok := h.gamePageContext(w, r)
	if !ok {
		return
	}
	if !h.validCSRF(w, r) {
		return
	}
	if h.missiles == nil {
		http.NotFound(w, r)
		return
	}
	form, err := parseMissileRequest(r)
	if err != nil {
		h.renderMissiles(w, r, http.StatusBadRequest, principal, planetID, form, false, "Quantité ou coordonnées invalides.")
		return
	}
	if launch {
		_, err = h.missiles.Launch(r.Context(), principal, planetID, form, r.PostFormValue("idempotency_key"))
	} else {
		_, err = h.missiles.Preview(r.Context(), principal, planetID, form)
	}
	if err != nil {
		if notFound(err) {
			http.NotFound(w, r)
			return
		}
		h.renderMissiles(w, r, http.StatusBadRequest, principal, planetID, form, false, missileError(err))
		return
	}
	if launch {
		http.Redirect(w, r, fmt.Sprintf("/planets/%d/missiles?sent=1", planetID), http.StatusSeeOther)
		return
	}
	h.renderMissiles(w, r, http.StatusOK, principal, planetID, form, true, "")
}

func (h *Handler) renderMissiles(w http.ResponseWriter, r *http.Request, status int, principal appauth.Principal, planetID int64, form appmissile.Request, confirm bool, message string) {
	if h.missiles == nil {
		http.NotFound(w, r)
		return
	}
	overview, err := h.missiles.Overview(r.Context(), principal, planetID)
	if err != nil {
		h.progressionFailure(w, r, err)
		return
	}
	planets, err := h.economy.Planets(r.Context(), principal)
	if err != nil {
		h.progressionFailure(w, r, err)
		return
	}
	token, ok := h.ensureCSRF(w, r)
	if !ok {
		return
	}
	key, ok := h.formKey(w, "missile")
	if !ok {
		return
	}
	shell := h.gameShell(r.Context(), token, principal, "galaxy", planets, planetID)
	shell.Error = message
	if r.URL.Query().Get("sent") == "1" {
		shell.Notice = "Salve lancée. Vous pouvez suivre son trajet ci-dessous."
	}
	data := missilePageData{pageShell: shell, Overview: overview, Form: form, Confirm: confirm, Key: key,
		Stock: overview.Planet.Units[unit.InterplanetaryMissile], Range: missile.Range(overview.Planet.Researches[research.ImpulseDrive])}
	if form.Target.Galaxy > 0 {
		plan, err := h.missiles.Preview(r.Context(), principal, planetID, form)
		if err == nil {
			data.Plan = &plan
		} else {
			data.Confirm = false
			if data.Error == "" {
				data.Error = missileError(err)
			}
		}
	}
	for _, id := range missile.Targets() {
		data.Choices = append(data.Choices, missileChoice{ID: id, Name: unitName(id), Selected: id == form.Priority})
	}
	h.render(w, status, "missiles", data)
}

func missileError(err error) string {
	switch {
	case errors.Is(err, appmissile.ErrDisabled):
		return "Les attaques par missiles sont désactivées dans cet univers."
	case errors.Is(err, appmissile.ErrOrigin):
		return "Le tir nécessite une planète équipée d’un silo à missiles de niveau 4."
	case errors.Is(err, appmissile.ErrTarget):
		return "Choisissez une planète ou une lune appartenant à un autre joueur."
	case errors.Is(err, appmissile.ErrStock):
		return "La planète ne dispose pas d’assez de missiles interplanétaires."
	case errors.Is(err, missile.ErrRange):
		return "La cible est hors de portée : vérifiez la galaxie et le niveau du réacteur à impulsion."
	case errors.Is(err, appmissile.ErrInvalid), errors.Is(err, missile.ErrInvalid):
		return "Vérifiez la quantité, les coordonnées et la défense prioritaire."
	default:
		return "Le tir n’a pas pu être préparé. Réessayez."
	}
}
