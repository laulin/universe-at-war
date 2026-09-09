package tests

import (
	"context"
	"net/http"
	"net/url"
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
		"csrf_token": {"csrf-token"}, "galaxy": {"1"}, "system": {"1"}, "position": {"1"},
		"mission": {"transport"}, "speed": {"100"},
		"cargo_metal": {"0"}, "cargo_crystal": {"0"}, "cargo_deuterium": {"0"},
		"composition[small_cargo]": {"2"},
	}
	for name, value := range overrides {
		values[name] = []string{value}
	}
	return values
}

// The confirmation is where the fuel is known at last, so it is the only place
// that can state the hold the fuel leaves. It used to state it beside a cargo
// locked in hidden fields, which left the player with a figure and no way to
// act on it but to start the mission over.
func TestTheConfirmationLetsTheCargoBeChanged(t *testing.T) {
	handler, database, session, csrfCookie := fleetHandler(t)
	ctx := context.Background()
	setUnits(t, ctx, database, 1, "small_cargo", 4)
	setResources(t, ctx, database, 1, 8_000, 5_000, 3_000)

	page := previewPage(t, handler, session, csrfCookie, missionValues(nil))
	if strings.Contains(betweenMarkers(page, `id="cargo_metal"`, ">"), `type="hidden"`) {
		t.Fatalf("the cargo is still locked away: %q", page)
	}
	for _, field := range []string{"cargo_metal", "cargo_crystal", "cargo_deuterium"} {
		if !strings.Contains(page, `<label for="`+field+`"`) {
			t.Fatalf("the field %s has no label: %q", field, page)
		}
		if !strings.Contains(betweenMarkers(page, `id="`+field+`"`, ">"), `max="`) {
			t.Fatalf("the field %s is bounded by nothing: %q", field, page)
		}
		if !strings.Contains(page, `data-max-for="`+field+`"`) {
			t.Fatalf("the field %s offers no way to reach its maximum: %q", field, page)
		}
	}
	if !strings.Contains(page, "Capacité après carburant") {
		t.Fatalf("the confirmation does not state the hold the fuel leaves: %q", page)
	}
}

// A cargo changed on the confirmation is the cargo that leaves. Nothing of the
// preview is carried over into the launch: the plan is calculated again from
// what the form posts.
func TestTheConfirmationLaunchesTheCargoItWasEditedWith(t *testing.T) {
	handler, database, session, csrfCookie := fleetHandler(t)
	ctx := context.Background()
	setUnits(t, ctx, database, 1, "small_cargo", 4)
	setResources(t, ctx, database, 1, 8_000, 5_000, 3_000)

	mission := missionValues(map[string]string{"cargo_metal": "400"})
	page := previewPage(t, handler, session, csrfCookie, mission)
	key := formValue(t, page, `action="/planets/1/fleet/launch"`, "idempotency_key")

	loaded := missionValues(map[string]string{"cargo_metal": "800"})
	loaded["idempotency_key"] = []string{key}
	postFormStatus(t, handler, "/planets/1/fleet/launch", loaded, session, csrfCookie)
	assertSingleValue(t, database, "SELECT metal FROM fleet_cargo WHERE fleet_id = 1", 800)
}

// A confirmation is signed with the cargo it was rendered with. Now that the
// cargo can be changed there, a step backwards can present a key that belongs
// to another loading — and that has to be told apart from a mission the domain
// refused, or the player reads "the fleet cannot leave" and learns nothing.
func TestAConfirmationReusedWithAnotherCargoSaysSo(t *testing.T) {
	handler, database, session, csrfCookie := fleetHandler(t)
	ctx := context.Background()
	setUnits(t, ctx, database, 1, "small_cargo", 4)
	setResources(t, ctx, database, 1, 8_000, 5_000, 3_000)

	mission := missionValues(map[string]string{"cargo_metal": "400"})
	key := sendThroughWizard(t, handler, session, csrfCookie, urlValues(mission))

	again := missionValues(map[string]string{"cargo_metal": "900"})
	again["idempotency_key"] = []string{key}
	body := postForm(t, handler, "/planets/1/fleet/launch", again, 400, session, csrfCookie)
	if !strings.Contains(body, "Cette confirmation n&#39;est plus valable") {
		t.Fatalf("a key reused with another cargo says nothing useful: %q", body)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM fleets", 1)
}

// Taking a mission back to the form used to reopen it empty: every ship, the
// destination, the mission and the cargo had to be entered a second time.
func TestModifyingTheMissionBringsBackEveryChoice(t *testing.T) {
	handler, database, session, csrfCookie := fleetHandler(t)
	ctx := context.Background()
	setUnits(t, ctx, database, 1, "small_cargo", 4)
	setResources(t, ctx, database, 1, 8_000, 5_000, 3_000)

	mission := missionValues(map[string]string{
		"speed": "50", "composition[small_cargo]": "3", "cargo_metal": "700",
	})
	back := postForm(t, handler, "/planets/1/fleet/send", mission, 200, session, csrfCookie)

	if !strings.Contains(betweenMarkers(back, `id="ship-small_cargo"`, ">"), `value="3"`) {
		t.Fatalf("the form forgot the ships: %q", back)
	}
	if !strings.Contains(betweenMarkers(back, `id="position"`, ">"), `value="1"`) {
		t.Fatalf("the form forgot the destination: %q", back)
	}
	if !strings.Contains(back, `<option value="50" selected>`) {
		t.Fatalf("the form forgot the speed: %q", back)
	}
	if !strings.Contains(betweenMarkers(back, `id="cargo_metal"`, ">"), `value="700"`) {
		t.Fatalf("the form forgot the cargo: %q", back)
	}
}

func previewPage(t *testing.T, handler http.Handler, session, csrf *http.Cookie, mission map[string][]string) string {
	t.Helper()
	return postForm(t, handler, "/planets/1/fleet/preview", mission, http.StatusOK, session, csrf)
}

func urlValues(values map[string][]string) url.Values {
	converted := url.Values{}
	for name, value := range values {
		converted[name] = value
	}
	return converted
}
