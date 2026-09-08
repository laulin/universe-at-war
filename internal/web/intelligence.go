package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appeconomy "universeatwar/internal/app/economy"
	appfleet "universeatwar/internal/app/fleet"
	appgalaxy "universeatwar/internal/app/galaxy"
	appreports "universeatwar/internal/app/reports"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/report"
	"universeatwar/internal/domain/unit"
	"universeatwar/internal/domain/universe"
)

// galaxyPageRow is one position of the map prepared for display.
type galaxyPageRow struct {
	Position      int
	Coordinate    string
	PlanetName    string
	OwnerName     string
	Own           bool
	Occupied      bool
	DebrisMetal   int64
	DebrisCrystal int64
	HasDebris     bool
}

type galaxyPageData struct {
	pageShell
	Galaxy       int
	System       int
	Rows         []galaxyPageRow
	PreviousLink string
	NextLink     string
	HomePlanet   int64
	CanAct       bool
}

type reportsPageData struct {
	pageShell
	Reports  []reportPageSummary
	Kind     string
	Unread   bool
	Page     int
	NextPage int
}

type reportPageSummary struct {
	ID         int64
	Kind       string
	KindName   string
	Coordinate string
	OccurredAt time.Time
	Read       bool
	Hostile    bool
	Shared     bool
	Own        bool
	OwnerName  string
	Freshness  string
}

type reportPageData struct {
	pageShell
	Report     reportPageSummary
	Espionage  *report.EspionagePayload
	Detected   *report.DetectedPayload
	Combat     *report.CombatPayload
	Recycling  *report.RecyclingPayload
	Expedition *report.ExpeditionPayload
}

func (h *Handler) galaxyPage(response http.ResponseWriter, request *http.Request) {
	principal, _, ok := h.requirePrincipal(response, request)
	if !ok {
		return
	}
	if principal.MustChangePassword {
		http.Redirect(response, request, "/password/change", http.StatusSeeOther)
		return
	}
	if h.galaxy == nil {
		http.NotFound(response, request)
		return
	}
	galaxy, galaxyErr := strconv.Atoi(request.PathValue("galaxy"))
	system, systemErr := strconv.Atoi(request.PathValue("system"))
	if galaxyErr != nil || systemErr != nil {
		http.NotFound(response, request)
		return
	}
	view, err := h.galaxy.System(request.Context(), principal, galaxy, system)
	if errors.Is(err, appgalaxy.ErrOutsideUniverse) {
		http.NotFound(response, request)
		return
	}
	if err != nil {
		http.Error(response, "galaxy unavailable", http.StatusInternalServerError)
		return
	}
	h.renderGalaxy(response, request, http.StatusOK, principal, view, "")
}

func (h *Handler) renderGalaxy(response http.ResponseWriter, request *http.Request, status int, principal appauth.Principal, view appgalaxy.View, message string) {
	planets, err := h.economy.Planets(request.Context(), principal)
	if err != nil && !errors.Is(err, appeconomy.ErrNoEmpire) {
		http.Error(response, "galaxy unavailable", http.StatusInternalServerError)
		return
	}
	token, ok := h.ensureCSRF(response, request)
	if !ok {
		return
	}
	rows := make([]galaxyPageRow, 0, len(view.Rows))
	for _, row := range view.Rows {
		display := galaxyPageRow{
			Position:   row.Position,
			Coordinate: fmt.Sprintf("%d:%d:%d", view.Galaxy, view.System, row.Position),
			PlanetName: row.PlanetName,
			OwnerName:  row.OwnerName,
			Own:        row.Own,
			Occupied:   row.PlanetID > 0,
		}
		if row.Debris != nil {
			display.HasDebris = true
			display.DebrisMetal = row.Debris.Metal
			display.DebrisCrystal = row.Debris.Crystal
		}
		rows = append(rows, display)
	}
	shell := h.gameShell(request.Context(), token, principal, "galaxy", planets, h.rememberedBody(request))
	shell.Error = message
	data := galaxyPageData{
		pageShell: shell, Galaxy: view.Galaxy, System: view.System, Rows: rows,
		PreviousLink: systemLink(view.Galaxy, view.System-1, view.Limits),
		NextLink:     systemLink(view.Galaxy, view.System+1, view.Limits),
	}
	if len(view.HomePlanets) > 0 {
		data.HomePlanet = view.HomePlanets[0]
		data.CanAct = true
	}
	h.render(response, status, "galaxy", data)
}

// spyFromGalaxy launches an espionage straight from the map.
func (h *Handler) spyFromGalaxy(response http.ResponseWriter, request *http.Request) {
	principal, _, ok := h.requirePrincipal(response, request)
	if !ok {
		return
	}
	if !h.validCSRF(response, request) {
		return
	}
	if h.fleet == nil || h.galaxy == nil {
		http.NotFound(response, request)
		return
	}
	galaxy, galaxyErr := strconv.Atoi(request.PathValue("galaxy"))
	system, systemErr := strconv.Atoi(request.PathValue("system"))
	position, positionErr := strconv.Atoi(request.PathValue("position"))
	planetID, planetErr := strconv.ParseInt(request.PostFormValue("planet"), 10, 64)
	probes, probesErr := strconv.ParseInt(request.PostFormValue("probes"), 10, 64)
	if galaxyErr != nil || systemErr != nil || positionErr != nil || planetErr != nil {
		http.NotFound(response, request)
		return
	}
	if probesErr != nil || probes <= 0 {
		probes = 1
	}
	_, err := h.fleet.Launch(request.Context(), principal, planetID, appfleet.LaunchRequest{
		Target:      universe.Coordinate{Galaxy: galaxy, System: system, Position: position},
		TargetKind:  domainfleet.TargetPlanet,
		Mission:     domainfleet.MissionEspionage,
		Composition: domainfleet.Composition{unit.EspionageProbe: probes},
		Percent:     100,
	}, request.PostFormValue("idempotency_key"))
	if err != nil {
		view, viewErr := h.galaxy.System(request.Context(), principal, galaxy, system)
		if viewErr != nil {
			http.Error(response, "galaxy unavailable", http.StatusInternalServerError)
			return
		}
		h.renderGalaxy(response, request, http.StatusBadRequest, principal, view, fleetError(err))
		return
	}
	http.Redirect(response, request, fmt.Sprintf("/galaxy/%d/%d", galaxy, system), http.StatusSeeOther)
}

func (h *Handler) reportsPage(response http.ResponseWriter, request *http.Request) {
	principal, _, ok := h.requirePrincipal(response, request)
	if !ok {
		return
	}
	if principal.MustChangePassword {
		http.Redirect(response, request, "/password/change", http.StatusSeeOther)
		return
	}
	if h.reports == nil {
		http.NotFound(response, request)
		return
	}
	page, _ := strconv.Atoi(request.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	filter := appreports.Filter{
		Kind:       report.Kind(request.URL.Query().Get("kind")),
		UnreadOnly: request.URL.Query().Get("unread") == "1",
		Page:       page,
	}
	if filter.Kind != "" && !filter.Kind.Valid() {
		http.NotFound(response, request)
		return
	}
	summaries, err := h.reports.List(request.Context(), principal, filter)
	if err != nil {
		http.Error(response, "reports unavailable", http.StatusInternalServerError)
		return
	}
	planets, err := h.economy.Planets(request.Context(), principal)
	if err != nil && !errors.Is(err, appeconomy.ErrNoEmpire) {
		http.Error(response, "reports unavailable", http.StatusInternalServerError)
		return
	}
	token, ok := h.ensureCSRF(response, request)
	if !ok {
		return
	}
	views := make([]reportPageSummary, 0, len(summaries))
	for _, summary := range summaries {
		views = append(views, summaryView(summary))
	}
	shell := h.gameShell(request.Context(), token, principal, "reports", planets, h.rememberedBody(request))
	next := 0
	if len(summaries) == appreports.PageSize {
		next = page + 1
	}
	h.render(response, http.StatusOK, "reports", reportsPageData{
		pageShell: shell, Reports: views, Kind: string(filter.Kind),
		Unread: filter.UnreadOnly, Page: page, NextPage: next,
	})
}

func (h *Handler) reportPage(response http.ResponseWriter, request *http.Request) {
	principal, _, ok := h.requirePrincipal(response, request)
	if !ok {
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
	detail, err := h.reports.Get(request.Context(), principal, reportID)
	if errors.Is(err, appreports.ErrNotFound) {
		http.NotFound(response, request)
		return
	}
	if err != nil {
		http.Error(response, "report unavailable", http.StatusInternalServerError)
		return
	}
	planets, err := h.economy.Planets(request.Context(), principal)
	if err != nil && !errors.Is(err, appeconomy.ErrNoEmpire) {
		http.Error(response, "report unavailable", http.StatusInternalServerError)
		return
	}
	token, ok := h.ensureCSRF(response, request)
	if !ok {
		return
	}
	data := reportPageData{
		pageShell: h.gameShell(request.Context(), token, principal, "reports", planets, h.rememberedBody(request)),
		Report:    summaryView(detail.Summary),
	}
	switch payload := detail.Payload.(type) {
	case report.EspionagePayload:
		data.Espionage = &payload
	case report.DetectedPayload:
		data.Detected = &payload
	case report.CombatPayload:
		data.Combat = &payload
	case report.RecyclingPayload:
		data.Recycling = &payload
	case report.ExpeditionPayload:
		data.Expedition = &payload
	}
	h.render(response, http.StatusOK, "report", data)
}

func (h *Handler) markReportRead(response http.ResponseWriter, request *http.Request) {
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
	if err := h.reports.MarkRead(request.Context(), principal, reportID); errors.Is(err, appreports.ErrNotFound) {
		http.NotFound(response, request)
		return
	} else if err != nil {
		http.Error(response, "report unavailable", http.StatusInternalServerError)
		return
	}
	http.Redirect(response, request, "/reports", http.StatusSeeOther)
}

func summaryView(summary appreports.Summary) reportPageSummary {
	return reportPageSummary{
		ID: summary.ID, Kind: string(summary.Kind), KindName: reportKindName(summary.Kind),
		Coordinate: summary.Coordinate.String(), OccurredAt: summary.OccurredAt, Read: summary.Read,
		Hostile: summary.Kind.Hostile(), Shared: summary.Shared, Own: summary.Own, OwnerName: summary.OwnerName,
		Freshness: freshnessName(summary.Freshness),
	}
}

func systemLink(galaxy, system int, limits universe.Limits) string {
	if system < 1 || system > limits.Systems {
		return ""
	}
	return fmt.Sprintf("/galaxy/%d/%d", galaxy, system)
}

func reportKindName(kind report.Kind) string {
	switch kind {
	case report.Espionage:
		return "Rapport d'espionnage"
	case report.EspionageDetected:
		return "Espionnage détecté"
	case report.Expedition:
		return "Rapport d'expédition"
	case report.CombatAttack:
		return "Rapport d'attaque"
	case report.CombatDefense:
		return "Rapport de défense"
	case report.Recycling:
		return "Rapport de recyclage"
	default:
		return string(kind)
	}
}

func freshnessName(freshness report.Freshness) string {
	switch freshness {
	case report.Recent:
		return "récent"
	case report.Old:
		return "ancien"
	default:
		return "inconnu"
	}
}
