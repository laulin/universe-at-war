package tests

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	appauth "universeatwar/internal/app/authentication"
	"universeatwar/internal/domain/report"
)

func TestWebLunarPageShowsItsFacilitiesParentAndSupplyRoute(t *testing.T) {
	handler, db, _, bodies, session, csrf := expansionHandler(t)
	page := getPage(t, handler, fmt.Sprintf("/planets/%d", bodies.Moon), session, csrf)
	for _, want := range []string{
		"Infrastructure lunaire", "ne produit aucune ressource", "Ravitailler depuis", "kind=moon",
		"/art/body/moon", "/art/surface/moon", "Recherche sur", "body-card--moon",
		fmt.Sprintf(`href="/planets/%d"`, bodies.AliceHome),
		fmt.Sprintf(`href="/planets/%d/research"`, bodies.AliceHome),
	} {
		if !strings.Contains(page, want) {
			t.Errorf("lunar page misses %q", want)
		}
	}
	for _, id := range []string{"lunar_base", "sensor_phalanx", "jump_gate", "robotics_factory", "shipyard", "metal_storage", "crystal_storage", "deuterium_tank"} {
		if !strings.Contains(page, `action="/planets/`+fmt.Sprint(bodies.Moon)+`/buildings/`+id+`"`) && !strings.Contains(page, `/art/building/`+id) {
			t.Errorf("lunar page misses %s", id)
		}
	}
	if strings.Contains(page, `href="/planets/`+fmt.Sprint(bodies.Moon)+`/research"`) {
		t.Fatal("lunar navigation offers a local research lab")
	}
	if got := strings.Count(page, `<article class="card`); got != 8 {
		t.Fatalf("lunar page has %d building cards, want 8", got)
	}
	if _, err := db.Write().ExecContext(context.Background(), "UPDATE planets SET used_fields = total_fields WHERE id = ?", bodies.Moon); err != nil {
		t.Fatal(err)
	}
	full := getPage(t, handler, fmt.Sprintf("/planets/%d", bodies.Moon), session, csrf)
	if !strings.Contains(full, "Toutes les cases") {
		t.Fatal("full moon has no expansion notice")
	}
	// A direct request cannot bypass the ownership check.
	foreign := insertMoon(t, context.Background(), db, 2, bodies.BobHome, 1, 1, 1)
	request := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/planets/%d", foreign), nil)
	request.AddCookie(session)
	request.AddCookie(csrf)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("foreign lunar page: %d", response.Code)
	}
	request = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/planets/%d/phalanx", bodies.AliceHome), nil)
	request.AddCookie(session)
	request.AddCookie(csrf)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("planet offers a phalanx page: %d", response.Code)
	}
	// The public map links directly to an owned moon.
	galaxy := getPage(t, handler, "/galaxy/1/1", session, csrf)
	if !strings.Contains(galaxy, fmt.Sprintf(`href="/planets/%d" title="Ouvrir votre lune"`, bodies.Moon)) {
		t.Fatal("galaxy has no own lunar link")
	}
}

func TestWebLunarDestinationSurvivesPreviewEditingAndLaunch(t *testing.T) {
	handler, db, w, bodies, session, csrf := expansionHandler(t)
	ctx := context.Background()
	setUnits(t, ctx, db, bodies.AliceHome, "small_cargo", 1)
	setResources(t, ctx, db, bodies.AliceHome, 5000, 5000, 5000)
	page := getPage(t, handler, fmt.Sprintf("/planets/%d/fleet/send?galaxy=1&system=1&position=8&kind=moon&mission=transport", bodies.AliceHome), session, csrf)
	if !strings.Contains(page, `<option value="moon" selected>Lune</option>`) {
		t.Fatal("supply shortcut lost its lunar destination")
	}
	form := url.Values{
		"csrf_token": {"csrf-token"}, "galaxy": {"1"}, "system": {"1"}, "position": {"8"},
		"kind": {"moon"}, "mission": {"transport"}, "speed": {"100"},
		"composition[small_cargo]": {"1"}, "cargo_metal": {"321"},
	}
	post := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		r := postFormRequest(path, form)
		r.AddCookie(session)
		r.AddCookie(csrf)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, r)
		return response
	}
	preview := post(fmt.Sprintf("/planets/%d/fleet/preview", bodies.AliceHome))
	if preview.Code != http.StatusOK {
		t.Fatalf("lunar preview: %d %s", preview.Code, preview.Body.String())
	}
	if hiddenValue(t, preview.Body.String(), "kind") != "moon" {
		t.Fatal("confirmation lost the lunar target")
	}
	form.Set("idempotency_key", hiddenValue(t, preview.Body.String(), "idempotency_key"))
	edited := post(fmt.Sprintf("/planets/%d/fleet/send", bodies.AliceHome))
	if edited.Code != http.StatusOK || !strings.Contains(edited.Body.String(), `<option value="moon" selected>Lune</option>`) {
		t.Fatalf("editing lost destination: %d %s", edited.Code, edited.Body.String())
	}
	launched := post(fmt.Sprintf("/planets/%d/fleet/launch", bodies.AliceHome))
	if launched.Code != http.StatusSeeOther {
		t.Fatalf("lunar launch: %d %s", launched.Code, launched.Body.String())
	}
	assertSingleText(t, db, "SELECT target_kind FROM fleets", "moon")
	assertSingleValue(t, db, "SELECT target_planet_id FROM fleets", int(bodies.Moon))
	flights, err := w.Fleet.Overview(ctx, appauth.Principal{AccountID: 1}, bodies.AliceHome)
	if err != nil {
		t.Fatal(err)
	}
	w.Clock.Set(flights.Fleets[0].ArrivesAt)
	if _, err := w.Events.CompleteDue(ctx, 50); err != nil {
		t.Fatal(err)
	}
	assertSingleValue(t, db, "SELECT metal FROM planet_resources WHERE planet_id = ?", 321, bodies.Moon)
}

func TestWebCombatExplainsMoonFormationAndReadsOldReports(t *testing.T) {
	handler, db, w, bodies, session, csrf := expansionHandler(t)
	ctx := context.Background()
	for i, test := range []struct {
		formation *report.MoonFormation
		text      string
		link      bool
	}{
		{&report.MoonFormation{Outcome: report.MoonCreated, MoonID: bodies.Moon}, "Une lune s'est formée", true},
		{&report.MoonFormation{Outcome: report.MoonFailed}, "Le tirage n'a pas créé", false},
		{&report.MoonFormation{Outcome: report.MoonAlreadyExists, MoonID: bodies.Moon}, "possède déjà une lune", true},
		{&report.MoonFormation{Outcome: report.MoonInsufficientDebris}, "Débris insuffisants", false},
		{&report.MoonFormation{Outcome: report.MoonTargetNotPlanet}, "ne crée pas de nouvelle lune", false},
		{nil, "Résultat non enregistré", false},
	} {
		id := insertSimulationReport(t, ctx, db, 1, report.CombatDefense, bodies.AliceHome, coordinateOf(t, 1, 1, 8), w.Clock.Now(),
			report.CombatPayload{Coordinate: coordinateOf(t, 1, 1, 8), MoonChance: .2, Moon: test.formation})
		page := getPage(t, handler, fmt.Sprintf("/reports/%d", id), session, csrf)
		if !strings.Contains(html.UnescapeString(page), test.text) || !strings.Contains(page, "20 %") {
			t.Fatalf("case %d: missing result %q", i, test.text)
		}
		if strings.Contains(page, "Ouvrir votre lune") != test.link {
			t.Fatalf("case %d: incorrect moon link", i)
		}
	}
	foreign := insertMoon(t, ctx, db, 2, bodies.BobHome, 1, 1, 1)
	id := insertSimulationReport(t, ctx, db, 1, report.CombatAttack, bodies.BobHome, coordinateOf(t, 1, 1, 1), w.Clock.Now(),
		report.CombatPayload{Coordinate: coordinateOf(t, 1, 1, 1), TargetKind: "moon", Moon: &report.MoonFormation{Outcome: report.MoonAlreadyExists, MoonID: foreign}})
	page := getPage(t, handler, fmt.Sprintf("/reports/%d", id), session, csrf)
	if strings.Contains(page, "Ouvrir votre lune") || !strings.Contains(page, "kind=moon") {
		t.Fatal("foreign lunar report exposes management or loses its target")
	}
}
