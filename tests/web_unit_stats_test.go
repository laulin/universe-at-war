package tests

import (
	"context"
	"strings"
	"testing"
)

// A card names a price and a duration but said nothing of what the ship is
// worth in a battle, which is what the player is choosing between.
func TestAShipCardCarriesItsCombatFigures(t *testing.T) {
	handler, database, session, csrfCookie := progressionHandler(t)
	setResearch(t, context.Background(), database, 1, "combustion_drive", 4)

	panel := statsPanel(t, getPage(t, handler, "/planets/1/shipyard", session, csrfCookie), "light_fighter")
	for _, figure := range []string{
		"Arme", ">50<", "Bouclier", ">10<", "Coque", ">400<", "Fret", "Consommation", ">20<",
	} {
		if !strings.Contains(panel, figure) {
			t.Fatalf("the light fighter card holds no %s: %q", figure, panel)
		}
	}
}

// The speed is the one the player's drives actually give, not the one the
// catalogue starts from: everything else on this card already counts what the
// planet has researched.
func TestTheSpeedShownIsTheOneTheDrivesGive(t *testing.T) {
	handler, database, session, csrfCookie := progressionHandler(t)
	setResearch(t, context.Background(), database, 1, "combustion_drive", 4)

	// A light fighter leaves the yard at 12.500 and a combustion drive adds a
	// tenth per level, so four levels make 17.500.
	panel := statsPanel(t, getPage(t, handler, "/planets/1/shipyard", session, csrfCookie), "light_fighter")
	if !strings.Contains(panel, ">17.500<") {
		t.Fatalf("the card shows no upgraded speed: %q", panel)
	}
	if strings.Contains(panel, ">12.500<") {
		t.Fatalf("the card still shows the catalogue speed: %q", panel)
	}
}

// Both directions of the rapid fire table are on the card, because the useful
// question is as much what this ship shreds as what shreds it.
func TestAShipCardNamesWhatItShredsAndWhatShredsIt(t *testing.T) {
	handler, _, session, csrfCookie := progressionHandler(t)

	panel := statsPanel(t, getPage(t, handler, "/planets/1/shipyard", session, csrfCookie), "cruiser")
	if !strings.Contains(panel, "<span>Chasseur léger</span><b>×6</b>") {
		t.Fatalf("the cruiser card does not say what it shreds: %q", panel)
	}
	if !strings.Contains(panel, "<span>Étoile de la mort</span><b>×33</b>") {
		t.Fatalf("the cruiser card does not say what shreds it: %q", panel)
	}
}

// A defence inflicts no rapid fire, so the only half worth printing is the one
// that names its predators.
func TestADefenceCardNamesItsPredators(t *testing.T) {
	handler, _, session, csrfCookie := progressionHandler(t)

	panel := statsPanel(t, getPage(t, handler, "/planets/1/defense", session, csrfCookie), "rocket_launcher")
	if strings.Contains(panel, "Inflige") {
		t.Fatalf("the launcher card claims to inflict rapid fire: %q", panel)
	}
	for _, predator := range []string{"<span>Croiseur</span><b>×10</b>", "<span>Bombardier</span><b>×20</b>", "<span>Étoile de la mort</span><b>×200</b>"} {
		if !strings.Contains(panel, predator) {
			t.Fatalf("the launcher card does not name %s: %q", predator, panel)
		}
	}
}

// statsPanel cuts the technical dialog out of the page, whichever production
// family owns the unit.
func statsPanel(t *testing.T, page, id string) string {
	t.Helper()
	start := -1
	for _, family := range []string{"ship", "defense"} {
		if candidate := strings.Index(page, `id="technology-`+family+`-`+id+`"`); candidate >= 0 {
			start = candidate
			break
		}
	}
	if start < 0 {
		t.Fatalf("the page holds no technical dialog for %s", id)
	}
	panel := page[start:]
	end := strings.Index(panel, "</dialog>")
	if end < 0 {
		t.Fatalf("the technical dialog of %s is never closed: %q", id, panel)
	}
	return panel[:end]
}
