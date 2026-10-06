package tests

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// The order refuses a facility another queue is counting on. The page did not
// apply that rule, so it offered a Construire button on the laboratory while a
// research was running, and the refusal that followed was reported by a single
// message that blamed the resources and the prerequisites — neither of which
// was wrong. The card must not offer what the order will refuse, and if an
// order is sent anyway it must be told what actually refused it.
func TestALaboratoryHeldByAResearchOffersNoFormAndSaysWhy(t *testing.T) {
	handler, database, session, csrfCookie := progressionHandler(t)
	ctx := context.Background()
	setBuilding(t, ctx, database, 1, "research_lab", 1)
	setResources(t, ctx, database, 1, 1_000_000, 1_000_000, 1_000_000)

	idle := buildingCardOf(t, getPage(t, handler, "/planets/1", session, csrfCookie), "research_lab")
	if !strings.Contains(idle, "<form") {
		t.Fatalf("an idle laboratory offers no form: %q", idle)
	}

	startResearch(t, handler, session, csrfCookie, "energy_technology")

	held := buildingCardOf(t, getPage(t, handler, "/planets/1", session, csrfCookie), "research_lab")
	if strings.Contains(held, "<form") {
		t.Fatalf("a laboratory held by a research still offers a form: %q", held)
	}
	if !strings.Contains(held, "Une recherche occupe ce laboratoire") {
		t.Fatalf("the card does not say what holds the laboratory: %q", held)
	}
	if strings.Contains(held, "Prérequis") {
		t.Fatalf("the card blames prerequisites for a busy laboratory: %q", held)
	}

	// A form sent from a page rendered before the research started still reaches
	// the order, and the banner must name the rule that refused it.
	refused := postForm(t, handler, "/planets/1/buildings/research_lab",
		url.Values{"csrf_token": {"csrf-token"}, "idempotency_key": {"held"}},
		http.StatusBadRequest, session, csrfCookie)
	if !strings.Contains(refused, "occupe cette installation") {
		t.Fatalf("the refusal does not name the busy installation: %q", refused)
	}
	if strings.Contains(refused, "prérequis") {
		t.Fatalf("the refusal blames prerequisites for a busy installation: %q", refused)
	}
}

// A locked card names its whole dependency, met parts included, because "one
// level short" and "not started" are different things to whoever is choosing
// what to build next.
func TestALockedCardListsEveryDependencyWithTheLevelReached(t *testing.T) {
	handler, database, session, csrfCookie := progressionHandler(t)
	setBuilding(t, context.Background(), database, 1, "robotics_factory", 1)

	card := buildingCardOf(t, getPage(t, handler, "/planets/1", session, csrfCookie), "shipyard")
	if strings.Contains(card, "<form") {
		t.Fatalf("a locked shipyard offers a form: %q", card)
	}
	if !strings.Contains(card, "Nécessite") || !strings.Contains(card, "Usine de robots") {
		t.Fatalf("the card does not name its dependency: %q", card)
	}
	if !strings.Contains(card, "1 / 2") {
		t.Fatalf("the card does not say how far the body has gone: %q", card)
	}
	if !strings.Contains(card, "requirement--unmet") {
		t.Fatalf("the card does not mark an outstanding dependency: %q", card)
	}

	// The nanite factory depends on a building and on a research, and shows both
	// whatever the state of each.
	nanite := buildingCardOf(t, getPage(t, handler, "/planets/1", session, csrfCookie), "nanite_factory")
	if !strings.Contains(nanite, "Usine de robots") || !strings.Contains(nanite, "Technologie ordinateur") {
		t.Fatalf("the card does not list both kinds of dependency: %q", nanite)
	}
	if !strings.Contains(nanite, "1 / 10") || !strings.Contains(nanite, "0 / 10") {
		t.Fatalf("the card does not say how far each dependency has gone: %q", nanite)
	}
}

// Energy is the third figure a mine is decided on, and the page had it nowhere
// but in the bar at the top. It is the change the ordered level makes, so a
// mine reads as a cost and the plant as a gain.
func TestACardCarriesTheEnergyItsNextLevelChanges(t *testing.T) {
	handler, _, session, csrfCookie := progressionHandler(t)
	page := getPage(t, handler, "/planets/1", session, csrfCookie)

	mine := buildingCardOf(t, page, "metal_mine")
	if !strings.Contains(mine, `class="res-energy"`) || !strings.Contains(mine, "-11") {
		t.Fatalf("the mine card does not carry the energy its level costs: %q", mine)
	}
	plant := buildingCardOf(t, page, "solar_plant")
	if !strings.Contains(plant, `class="res-energy"`) || !strings.Contains(plant, "&#43;22") {
		t.Fatalf("the plant card does not carry the energy its level gives: %q", plant)
	}
	storage := buildingCardOf(t, page, "metal_storage")
	if strings.Contains(storage, `class="res-energy"`) {
		t.Fatalf("a storage card carries an energy it does not change: %q", storage)
	}
}

func TestFusionReactorCardNamesItsRequirementsAndEnergy(t *testing.T) {
	handler, database, session, csrfCookie := progressionHandler(t)
	ctx := context.Background()

	locked := buildingCardOf(t, getPage(t, handler, "/planets/1", session, csrfCookie), "fusion_reactor")
	for _, expected := range []string{"Synthétiseur de deutérium", "Technologie énergétique"} {
		if !strings.Contains(locked, expected) {
			t.Fatalf("the locked fusion reactor card misses requirement %q: %q", expected, locked)
		}
	}
	if strings.Contains(locked, "<form") {
		t.Fatalf("the locked fusion reactor offers a construction form: %q", locked)
	}

	setBuilding(t, ctx, database, 1, "deuterium_synthesizer", 5)
	setResearch(t, ctx, database, 1, "energy_technology", 3)

	card := buildingCardOf(t, getPage(t, handler, "/planets/1", session, csrfCookie), "fusion_reactor")
	for _, expected := range []string{
		"Centrale électrique de fusion",
		`class="res-energy"`,
		"&#43;32",
		"Convertit du deutérium en énergie",
		"<form",
	} {
		if !strings.Contains(card, expected) {
			t.Fatalf("the fusion reactor card misses %q: %q", expected, card)
		}
	}
}

// A card that only names a price says nothing about what the building is for.
func TestEveryCardSaysWhatItsBuildingIsFor(t *testing.T) {
	handler, _, session, csrfCookie := progressionHandler(t)
	page := getPage(t, handler, "/planets/1", session, csrfCookie)

	if !strings.Contains(buildingCardOf(t, page, "solar_plant"), "Produit l&#39;énergie que les trois mines consomment") {
		t.Fatalf("the plant card does not say what it is for: %q", buildingCardOf(t, page, "solar_plant"))
	}
	for _, id := range []string{"metal_mine", "crystal_mine", "deuterium_synthesizer", "solar_plant", "fusion_reactor",
		"metal_storage", "crystal_storage", "deuterium_tank", "robotics_factory", "nanite_factory",
		"shipyard", "research_lab", "alliance_depot", "missile_silo", "terraformer"} {
		if !strings.Contains(buildingCardOf(t, page, id), `class="card__more"`) {
			t.Fatalf("the card of %s carries no role", id)
		}
	}
}

// buildingCardOf finds a card by its illustration rather than by its form: a
// locked card carries no form, and the navigation holds a link that looks like
// one for the shipyard.
func buildingCardOf(t *testing.T, page, id string) string {
	t.Helper()
	return cardOf(t, page, "art/building/"+id)
}

func startResearch(t *testing.T, handler http.Handler, session, csrfCookie *http.Cookie, id string) {
	t.Helper()
	page := getPage(t, handler, "/planets/1/research", session, csrfCookie)
	key := formValue(t, page, `action="/planets/1/research/`+id+`"`, "idempotency_key")
	request := postFormRequest("/planets/1/research/"+id, url.Values{"csrf_token": {"csrf-token"}, "idempotency_key": {key}})
	request.AddCookie(session)
	request.AddCookie(csrfCookie)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("starting %s = %d: %s", id, recorder.Code, recorder.Body.String())
	}
}
