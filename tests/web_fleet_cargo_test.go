package tests

import (
	"context"
	"strings"
	"testing"
)

// The send form asked for a cargo without ever saying how much the fleet could
// carry, and bounded the three fields by nothing at all. A player loaded blind
// and only learned the hold at the confirmation, where the cargo could no
// longer be changed.
func TestTheSendFormSaysWhatTheFleetCanCarry(t *testing.T) {
	handler, database, session, csrfCookie := fleetHandler(t)
	ctx := context.Background()
	setUnits(t, ctx, database, 1, "small_cargo", 4)
	setResources(t, ctx, database, 1, 8_000, 5_000, 3_000)

	page := getPage(t, handler, "/planets/1/fleet/send", session, csrfCookie)
	if !strings.Contains(page, "Soute de la flotte") {
		t.Fatalf("the form does not say what the fleet can carry: %q", page)
	}
	// The hold of a ship travels with the field that picks it, which is what the
	// page adds up as ships are chosen.
	small := betweenMarkers(page, `id="ship-small_cargo"`, ">")
	if !strings.Contains(small, `data-cargo="5000"`) {
		t.Fatalf("the field does not carry the hold of its ship: %q", small)
	}
	for field, stock := range map[string]string{
		"cargo_metal": "8000", "cargo_crystal": "5000", "cargo_deuterium": "3000",
	} {
		bounded := betweenMarkers(page, `id="`+field+`"`, ">")
		if !strings.Contains(bounded, `max="`+stock+`"`) {
			t.Fatalf("%s is not bounded by the stores: %q", field, bounded)
		}
		if !strings.Contains(bounded, `data-stock-max="`+stock+`"`) {
			t.Fatalf("%s does not carry the ceiling the stores put on it: %q", field, bounded)
		}
	}
}

// The figures are rendered rather than left to the script, so they are right on
// a page without one — and right again when a refused mission comes back with
// the answers the player gave.
func TestTheHoldOfARefusedMissionComesBackWithIt(t *testing.T) {
	handler, database, session, csrfCookie := fleetHandler(t)
	ctx := context.Background()
	setUnits(t, ctx, database, 1, "small_cargo", 4)
	setResources(t, ctx, database, 1, 8_000, 5_000, 3_000)

	// An espionage refuses a composition of cargo ships, so the form comes back.
	refused := postForm(t, handler, "/planets/1/fleet/preview", missionValues(map[string]string{
		"mission": "espionage", "composition[small_cargo]": "3", "cargo_metal": "4000",
	}), 400, session, csrfCookie)

	hold := betweenMarkers(refused, `class="hold"`, "</p>")
	if !strings.Contains(hold, "15.000") {
		t.Fatalf("the returned form forgot the hold of the composition: %q", hold)
	}
	if !strings.Contains(hold, "4.000") || !strings.Contains(hold, "11.000") {
		t.Fatalf("the returned form forgot what was loaded and what is left: %q", hold)
	}
}

func missionValues(overrides map[string]string) map[string][]string {
	values := map[string][]string{
		"csrf_token": {"csrf-token"}, "galaxy": {"1"}, "system": {"1"}, "position": {"2"},
		"mission": {"transport"}, "speed": {"100"},
		"cargo_metal": {"0"}, "cargo_crystal": {"0"}, "cargo_deuterium": {"0"},
		"composition[small_cargo]": {"1"},
	}
	for name, value := range overrides {
		values[name] = []string{value}
	}
	return values
}
