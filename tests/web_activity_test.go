package tests

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	appauth "universeatwar/internal/app/authentication"
	webhandler "universeatwar/internal/web"
)

func TestBodyColumnIndicatorsAndIncomingFleetWarning(t *testing.T) {
	fixture := newActivityFixture(t)
	session := &http.Cookie{Name: "uaw_session", Value: "session"}
	csrf := &http.Cookie{Name: "uaw_csrf", Value: "csrf-token"}

	aliceHandler := activityHandler(t, fixture,
		appauth.Principal{AccountID: fixture.alice.AccountID, Username: "alice"})
	alicePage := getPage(t, aliceHandler, fmt.Sprintf("/planets/%d", fixture.aliceHome.ID), session, csrf)
	for _, indicator := range []string{
		`title="Constructions : 1"`, `title="Recherches : 1"`,
		`title="Vaisseaux en construction : 3"`, `title="Défenses en construction : 4"`,
		`title="Flottes en vol depuis ce corps : 1"`,
	} {
		if !strings.Contains(alicePage, indicator) {
			t.Fatalf("Alice body column has no %s: %q", indicator, alicePage)
		}
	}
	if strings.Contains(alicePage, "Attaques ennemies en approche :") {
		t.Fatalf("Alice sees an attack aimed at another player: %q", alicePage)
	}

	bobHandler := activityHandler(t, fixture,
		appauth.Principal{AccountID: fixture.bob.AccountID, Username: "bob"})
	bobFleet := getPage(t, bobHandler, fmt.Sprintf("/planets/%d/fleet", fixture.bobHome.ID), session, csrf)
	for _, content := range []string{
		`body-activity__item--danger`, `title="Attaques ennemies en approche : 1"`,
		"Flottes ennemies en approche", "Alice", "2 × Chasseur léger",
		fixture.aliceHome.Coordinate.String() + " → " + fixture.bobHome.Coordinate.String(),
		`data-done="impact"`,
	} {
		if !strings.Contains(bobFleet, content) {
			t.Fatalf("Bob fleet page has no %q: %q", content, bobFleet)
		}
	}
	if strings.Contains(bobFleet, "Aucune flotte ennemie en approche.") {
		t.Fatalf("Bob's incoming attack is hidden: %q", bobFleet)
	}
}

func activityHandler(t *testing.T, fixture activityFixture, principal appauth.Principal) http.Handler {
	t.Helper()
	handler, err := webhandler.New(webhandler.Dependencies{
		Authentication: webAuthenticationStub{principal: principal},
		ServerState:    runningStateStub{}, CSRFSecrets: sequenceSecret{value: "csrf-token"},
		Economy: fixture.game.Economy, Fleet: fixture.game.Fleet, Activity: fixture.game.Activity,
	})
	if err != nil {
		t.Fatal(err)
	}
	return handler
}
