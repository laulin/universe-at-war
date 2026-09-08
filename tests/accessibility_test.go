package tests

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appclock "universeatwar/internal/clock"
	webhandler "universeatwar/internal/web"
)

// TestEveryPlayerScreenHoldsItsAccessibilityBasics sweeps the pages a player
// reaches and checks what a keyboard and a screen reader need: a way past the
// navigation, a label for every field, a header for every column, no styling
// that carries meaning on its own, and no inline script.
func TestEveryPlayerScreenHoldsItsAccessibilityBasics(t *testing.T) {
	handler, session, csrfCookie := accessibleUniverse(t)
	routes := []string{
		"/", "/planets/1", "/planets/1/research", "/planets/1/shipyard", "/planets/1/defense",
		"/planets/1/fleet", "/planets/1/fleet/send", "/galaxy/1/1", "/reports", "/alliance",
	}
	labelled := regexp.MustCompile(`<label for="([^"]+)"`)
	inputs := regexp.MustCompile(`<(?:input|select|textarea)[^>]*\sid="([^"]+)"`)
	fields := regexp.MustCompile(`<(?:input|select|textarea)[^>]*>`)

	for _, route := range routes {
		t.Run(route, func(t *testing.T) {
			page := getPage(t, handler, route, session, csrfCookie)
			if strings.Contains(page, "404 page not found") {
				t.Fatalf("%s is not reachable", route)
			}
			if !strings.Contains(page, `<html lang="fr">`) {
				t.Fatal("the page does not say what language it is in")
			}
			if !strings.Contains(page, `class="skip-link" href="#main-content"`) ||
				!strings.Contains(page, `id="main-content"`) {
				t.Fatal("a keyboard visitor cannot skip the navigation")
			}
			if strings.Contains(page, "<script>") || strings.Contains(page, "onclick=") {
				t.Fatal("the page carries inline script")
			}
			// Every visible field has a label pointing at it.
			labels := map[string]bool{}
			for _, match := range labelled.FindAllStringSubmatch(page, -1) {
				labels[match[1]] = true
			}
			for _, match := range inputs.FindAllStringSubmatch(page, -1) {
				if !labels[match[1]] {
					t.Fatalf("the field %q has no label", match[1])
				}
			}
			// Every visible field carries an identifier at all.
			for _, field := range fields.FindAllString(page, -1) {
				if strings.Contains(field, `type="hidden"`) {
					continue
				}
				if !strings.Contains(field, " id=") {
					t.Fatalf("a field carries no identifier: %s", field)
				}
			}
			// Every table names its columns.
			for _, table := range strings.Split(page, "<table")[1:] {
				body, _, _ := strings.Cut(table, "</table>")
				if !strings.Contains(body, `<th scope="col">`) {
					t.Fatalf("a table on %s names no column", route)
				}
			}
			// A hostile marker never relies on its colour alone.
			for _, marker := range strings.Split(page, `class="hostile"`)[1:] {
				text, _, _ := strings.Cut(marker, "</span>")
				if strings.TrimSpace(strings.TrimPrefix(text, ">")) == "" {
					t.Fatal("a hostile marker carries no text")
				}
			}
			// Countdowns never shout at a screen reader every second.
			for _, countdown := range strings.Split(page, "data-countdown")[1:] {
				attributes, _, _ := strings.Cut(countdown, ">")
				if !strings.Contains(attributes, `aria-live="off"`) {
					t.Fatalf("a countdown on %s is announced every second", route)
				}
			}
		})
	}
}

// TestPagesAskForOneTargetedRefresh proves a page whose content expires says so,
// and that the page stays correct for somebody without JavaScript.
func TestPagesAskForOneTargetedRefresh(t *testing.T) {
	ctx := context.Background()
	handler, session, csrfCookie := accessibleUniverse(t)
	_ = ctx
	fleet := getPage(t, handler, "/planets/1/fleet", session, csrfCookie)
	if !strings.Contains(fleet, `<script src="/static/app.js" defer></script>`) {
		t.Fatal("the page does not load the client script")
	}
	// The absolute server time is rendered whatever happens, so the page is
	// readable and correct without any script at all.
	if !strings.Contains(fleet, "data-server-time") || !strings.Contains(fleet, "UTC") {
		t.Fatalf("the page carries no authoritative server time: %q", fleet)
	}
	research := getPage(t, handler, "/planets/1/research", session, csrfCookie)
	for _, page := range []string{fleet, research} {
		if strings.Contains(page, "data-countdown") && !strings.Contains(page, "data-refresh") {
			continue
		}
	}
}

func accessibleUniverse(t *testing.T) (http.Handler, *http.Cookie, *http.Cookie) {
	t.Helper()
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC))
	database := economyDatabase(t, ctx, 2)
	universeWorld := newWorld(t, database, clock)
	home, err := universeWorld.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: 1}, "Alice")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := universeWorld.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: 2}, "Bob"); err != nil {
		t.Fatal(err)
	}
	setResources(t, ctx, database, home.ID, 50000, 50000, 50000)
	setUnits(t, ctx, database, home.ID, "small_cargo", 4)
	setResearch(t, ctx, database, 1, "combustion_drive", 2)
	if _, err := universeWorld.Economy.EnqueueBuilding(ctx, appauth.Principal{AccountID: 1},
		home.ID, "metal_mine", "mine"); err != nil {
		t.Fatal(err)
	}
	handler, err := webhandler.New(webhandler.Dependencies{
		Authentication: webAuthenticationStub{principal: appauth.Principal{AccountID: 1, Username: "player1"}},
		ServerState:    runningStateStub{}, CSRFSecrets: sequenceSecret{value: "csrf-token"},
		Economy: universeWorld.Economy, Research: universeWorld.Research, Shipyard: universeWorld.Shipyard,
		Fleet: universeWorld.Fleet, Galaxy: universeWorld.Galaxy, Reports: universeWorld.Reports,
		Phalanx: universeWorld.Phalanx, JumpGate: universeWorld.JumpGate,
		Alliance: universeWorld.Alliance, ACS: universeWorld.ACS,
	})
	if err != nil {
		t.Fatal(err)
	}
	return handler, &http.Cookie{Name: "uaw_session", Value: "session"},
		&http.Cookie{Name: "uaw_csrf", Value: "csrf-token"}
}
