package tests

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// The order refuses every research while the laboratory sits in the building
// queue. The page did not apply that rule, so it offered a Rechercher button
// whose only outcome was a refusal — the same fault the planet page had about a
// laboratory held by a research, read from the other end.
func TestALaboratoryUnderConstructionOffersNoResearchAndSaysWhy(t *testing.T) {
	handler, database, session, csrfCookie := progressionHandler(t)
	ctx := context.Background()
	setBuilding(t, ctx, database, 1, "research_lab", 1)
	setResources(t, ctx, database, 1, 1_000_000, 1_000_000, 1_000_000)

	idle := researchCardOf(t, getPage(t, handler, "/planets/1/research", session, csrfCookie), "energy_technology")
	if !strings.Contains(idle, "<form") {
		t.Fatalf("an idle laboratory offers no research: %q", idle)
	}

	queueLaboratory(t, handler, session, csrfCookie)

	held := researchCardOf(t, getPage(t, handler, "/planets/1/research", session, csrfCookie), "energy_technology")
	if strings.Contains(held, "<form") {
		t.Fatalf("a laboratory under construction still offers a research: %q", held)
	}
	if !strings.Contains(held, "Le laboratoire est dans la file de construction") {
		t.Fatalf("the card does not say what holds the laboratory: %q", held)
	}

	refused := postForm(t, handler, "/planets/1/research/energy_technology",
		url.Values{"csrf_token": {"csrf-token"}, "idempotency_key": {"held"}},
		http.StatusBadRequest, session, csrfCookie)
	if !strings.Contains(refused, "Le laboratoire est dans la file de construction") {
		t.Fatalf("the refusal does not name the laboratory: %q", refused)
	}
}

// A locked technology names its whole dependency with the level the empire has
// reached, as a locked building does.
func TestALockedResearchListsEveryDependencyWithTheLevelReached(t *testing.T) {
	handler, database, session, csrfCookie := progressionHandler(t)
	setBuilding(t, context.Background(), database, 1, "research_lab", 1)

	card := researchCardOf(t, getPage(t, handler, "/planets/1/research", session, csrfCookie), "plasma_technology")
	if strings.Contains(card, "<form") {
		t.Fatalf("a locked technology offers a form: %q", card)
	}
	if !strings.Contains(card, "Nécessite") {
		t.Fatalf("the card does not name its dependencies: %q", card)
	}
	for _, want := range []string{"Laboratoire de recherche", "Technologie énergétique", "Technologie laser", "Technologie à ions"} {
		if !strings.Contains(card, want) {
			t.Fatalf("the card omits the dependency %s: %q", want, card)
		}
	}
	if !strings.Contains(card, "1 / 4") || !strings.Contains(card, "0 / 10") {
		t.Fatalf("the card does not say how far each dependency has gone: %q", card)
	}
	if strings.Contains(card, "Prérequis manquants") {
		t.Fatalf("the card states its dependencies twice: %q", card)
	}
}

// A price and a duration do not say what a technology is for, and a graph of
// sixteen entries is where guessing goes wrong.
func TestEveryResearchCardSaysWhatItsTechnologyIsFor(t *testing.T) {
	handler, _, session, csrfCookie := progressionHandler(t)
	page := getPage(t, handler, "/planets/1/research", session, csrfCookie)

	if !strings.Contains(researchCardOf(t, page, "computer_technology"), "Ajoute une flotte simultanée par niveau") {
		t.Fatalf("the computer card does not say what it is for: %q", researchCardOf(t, page, "computer_technology"))
	}
	for _, id := range []string{"energy_technology", "laser_technology", "ion_technology", "hyperspace_technology",
		"plasma_technology", "combustion_drive", "impulse_drive", "hyperspace_drive", "espionage_technology",
		"computer_technology", "astrophysics", "intergalactic_research_network", "weapons_technology",
		"shielding_technology", "armour_technology", "graviton_technology"} {
		if !strings.Contains(researchCardOf(t, page, id), `class="card__more"`) {
			t.Fatalf("the card of %s carries no role", id)
		}
	}
}

func researchCardOf(t *testing.T, page, id string) string {
	t.Helper()
	return cardOf(t, page, "art/research/"+id)
}

func queueLaboratory(t *testing.T, handler http.Handler, session, csrfCookie *http.Cookie) {
	t.Helper()
	page := getPage(t, handler, "/planets/1", session, csrfCookie)
	key := formValue(t, page, `action="/planets/1/buildings/research_lab"`, "idempotency_key")
	request := postFormRequest("/planets/1/buildings/research_lab", url.Values{"csrf_token": {"csrf-token"}, "idempotency_key": {key}})
	request.AddCookie(session)
	request.AddCookie(csrfCookie)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("queueing the laboratory = %d: %s", recorder.Code, recorder.Body.String())
	}
}
