package tests

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appclock "universeatwar/internal/clock"
	"universeatwar/internal/domain/building"
	webhandler "universeatwar/internal/web"
)

// queuedWeb serves the game pages of one account whose queue a test fills.
func queuedWeb(t *testing.T) (http.Handler, *world, appauth.Principal, *http.Cookie, *http.Cookie) {
	t.Helper()
	ctx := context.Background()
	database := economyDatabase(t, ctx, 1)
	universeWorld := newWorld(t, database, appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC)))
	principal := appauth.Principal{AccountID: 1, Username: "player1"}
	if _, err := universeWorld.Economy.CreateEmpire(ctx, principal, "Captain"); err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	setResources(t, ctx, database, 1, 5_000_000, 5_000_000, 5_000_000)
	setBuilding(t, ctx, database, 1, "research_lab", 4)
	setBuilding(t, ctx, database, 1, "solar_plant", 20)
	handler, err := webhandler.New(webhandler.Dependencies{
		Authentication: webAuthenticationStub{principal: principal},
		ServerState:    runningStateStub{},
		CSRFSecrets:    sequenceSecret{value: "csrf-token"},
		Economy:        universeWorld.Economy,
		Research:       universeWorld.Research,
		Shipyard:       universeWorld.Shipyard,
	})
	if err != nil {
		t.Fatal(err)
	}
	return handler, universeWorld, principal, &http.Cookie{Name: "uaw_session", Value: "session"}, csrfOf()
}

// The build queue is the liveliest thing on a screen: the running order shows a
// bar that fills and a countdown that ticks, and the orders behind it show when
// their turn is expected.
func TestTheBuildQueueShowsAProgressBarACountdownAndWhatWaits(t *testing.T) {
	ctx := context.Background()
	handler, universeWorld, principal, session, csrf := queuedWeb(t)
	planet, err := universeWorld.Economy.Planet(ctx, principal, 0)
	if err != nil {
		t.Fatal(err)
	}
	for index, id := range []building.ID{building.MetalMine, building.MetalMine} {
		if _, err := universeWorld.Economy.EnqueueBuilding(ctx, principal, planet.ID, id, string(id)+string(rune('a'+index))); err != nil {
			t.Fatalf("EnqueueBuilding(%s) error = %v", id, err)
		}
	}

	page := getPage(t, handler, "/planets/1", session, csrf)
	for _, fragment := range []string{
		"File de construction", "Construction en cours",
		`class="queue-list"`, "<progress", "data-progress", `data-countdown`,
		// The waiting order names itself in French and states its forecast.
		"Mine de métal", `class="queue-entry queue-entry--waiting"`, "Début ≈",
	} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("the queue panel has no %q: %q", fragment, page)
		}
	}
	// A raw domain identifier must never reach the player.
	if strings.Contains(page, "metal_mine niveau") {
		t.Fatalf("the queue panel leaked a domain identifier: %q", page)
	}
	// The policy refuses inline styles, so the bar is filled by attribute.
	if strings.Contains(page, "style=") {
		t.Fatalf("the queue panel carries an inline style: %q", page)
	}
	// A countdown never shouts at a screen reader every second.
	for _, fragment := range regexp.MustCompile(`data-countdown[^>]*`).FindAllString(page, -1) {
		if !strings.Contains(fragment, `aria-live="off"`) {
			t.Fatalf("countdown %q does not silence itself", fragment)
		}
	}
	// The two levels are distinct orders, not one shown twice.
	if got := strings.Count(page, `<li class="queue-entry`); got != 2 {
		t.Fatalf("queue holds %d entries, want 2: %q", got, page)
	}
	if !strings.Contains(page, "niveau 1") || !strings.Contains(page, "niveau 2") {
		t.Fatalf("the queue does not show both levels: %q", page)
	}
}

// The empire table names the construction of each body and says how many orders
// wait behind it.
func TestTheEmpireOverviewSummarisesEachQueue(t *testing.T) {
	ctx := context.Background()
	handler, universeWorld, principal, session, csrf := queuedWeb(t)
	planet, err := universeWorld.Economy.Planet(ctx, principal, 0)
	if err != nil {
		t.Fatal(err)
	}
	for index := range 3 {
		if _, err := universeWorld.Economy.EnqueueBuilding(ctx, principal, planet.ID, building.MetalMine, string(rune('a'+index))); err != nil {
			t.Fatalf("EnqueueBuilding(%d) error = %v", index, err)
		}
	}

	page := getPage(t, handler, "/", session, csrf)
	if !strings.Contains(page, "Mine de métal niveau 1") || !strings.Contains(page, "+2 en file") {
		t.Fatalf("the overview does not summarise the queue: %q", page)
	}
}

// A queue entry carries its own cancel button, and cancelling says what came
// back before sending the player on.
func TestCancellingFromTheQueuePanelRefundsAndReportsIt(t *testing.T) {
	ctx := context.Background()
	handler, universeWorld, principal, session, csrf := queuedWeb(t)
	planet, err := universeWorld.Economy.Planet(ctx, principal, 0)
	if err != nil {
		t.Fatal(err)
	}
	for index := range 2 {
		if _, err := universeWorld.Economy.EnqueueBuilding(ctx, principal, planet.ID, building.MetalMine, string(rune('a'+index))); err != nil {
			t.Fatal(err)
		}
	}
	queued, err := universeWorld.Economy.Planet(ctx, principal, planet.ID)
	if err != nil {
		t.Fatal(err)
	}
	waiting := queued.Queue[1]

	page := getPage(t, handler, "/planets/1", session, csrf)
	target := fmt.Sprintf("/planets/1/queue/building/%d/cancel", waiting.ID)
	if !strings.Contains(page, target) || !strings.Contains(page, ">Annuler<") {
		t.Fatalf("the queue panel offers no cancellation: %q", page)
	}

	request := postFormRequest(target, url.Values{"csrf_token": {"csrf-token"}})
	request.AddCookie(session)
	request.AddCookie(csrf)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("POST cancel = %d %q", recorder.Code, recorder.Body.String())
	}
	location := recorder.Header().Get("Location")
	if !strings.HasPrefix(location, "/planets/1?cancelled=1") {
		t.Fatalf("cancellation redirected to %q", location)
	}

	after := getPage(t, handler, location, session, csrf)
	if !strings.Contains(after, "1 ordre annulé et remboursé.") {
		t.Fatalf("the page does not report the cancellation: %q", after)
	}
	if got := strings.Count(after, `<li class="queue-entry`); got != 1 {
		t.Fatalf("queue holds %d entries after the cancellation, want 1", got)
	}
	// Cancelling the same order twice never doubles a refund.
	replay := postFormRequest(target, url.Values{"csrf_token": {"csrf-token"}})
	replay.AddCookie(session)
	replay.AddCookie(csrf)
	refused := httptest.NewRecorder()
	handler.ServeHTTP(refused, replay)
	if refused.Code != http.StatusBadRequest || !strings.Contains(refused.Body.String(), "n&#39;est plus dans la file") {
		t.Fatalf("second cancellation = %d %q", refused.Code, refused.Body.String())
	}
}

// Ordering the same building twice from the page must queue two levels. The
// card plans against what the queue already reaches, so its price, its target
// and its idempotency key all move with the queue; otherwise the second post
// looks like a replay of the first and is silently swallowed.
func TestOrderingTheSameBuildingTwiceQueuesTwoLevels(t *testing.T) {
	handler, _, _, session, csrf := queuedWeb(t)

	for attempt := range 3 {
		page := getPage(t, handler, "/planets/1", session, csrf)
		key := formValue(t, page, `action="/planets/1/buildings/metal_mine"`, "idempotency_key")
		request := postFormRequest("/planets/1/buildings/metal_mine",
			url.Values{"csrf_token": {"csrf-token"}, "idempotency_key": {key}})
		request.AddCookie(session)
		request.AddCookie(csrf)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusSeeOther {
			t.Fatalf("order %d = %d %q", attempt, recorder.Code, recorder.Body.String())
		}
	}

	page := getPage(t, handler, "/planets/1", session, csrf)
	if got := strings.Count(page, `<li class="queue-entry`); got != 3 {
		t.Fatalf("three orders left %d entries in the queue: %q", got, page)
	}
	for _, level := range []string{"niveau 1", "niveau 2", "niveau 3"} {
		if !strings.Contains(page, level) {
			t.Fatalf("the queue does not show %q: %q", level, page)
		}
	}
	// The card now offers the level after the queue, at its own price.
	if !strings.Contains(page, `value="csrf-token:metal_mine:4"`) {
		t.Fatalf("the card still offers a level the queue already reaches: %q", page)
	}
}

// The same holds for the laboratory: a second order must be a new research, not
// a replay of the first.
func TestOrderingTheSameResearchTwiceQueuesTwoLevels(t *testing.T) {
	ctx := context.Background()
	handler, universeWorld, principal, session, csrf := queuedWeb(t)
	planet, err := universeWorld.Economy.Planet(ctx, principal, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := universeWorld.Economy.Planet(ctx, principal, planet.ID); err != nil {
		t.Fatal(err)
	}

	for attempt := range 2 {
		page := getPage(t, handler, "/planets/1/research", session, csrf)
		key := formValue(t, page, `action="/planets/1/research/energy_technology"`, "idempotency_key")
		request := postFormRequest("/planets/1/research/energy_technology",
			url.Values{"csrf_token": {"csrf-token"}, "idempotency_key": {key}})
		request.AddCookie(session)
		request.AddCookie(csrf)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusSeeOther {
			t.Fatalf("research order %d = %d %q", attempt, recorder.Code, recorder.Body.String())
		}
	}
	page := getPage(t, handler, "/planets/1/research", session, csrf)
	if got := strings.Count(page, `<li class="queue-entry`); got != 2 {
		t.Fatalf("two research orders left %d entries: %q", got, page)
	}
}

// formValue reads one hidden field out of the form whose action is given.
func formValue(t *testing.T, page, action, field string) string {
	t.Helper()
	start := strings.Index(page, action)
	if start < 0 {
		t.Fatalf("no form with %s in %q", action, page)
	}
	form := page[start:]
	if end := strings.Index(form, "</form>"); end >= 0 {
		form = form[:end]
	}
	marker := `name="` + field + `" value="`
	from := strings.Index(form, marker)
	if from < 0 {
		t.Fatalf("form %s carries no %s: %q", action, field, form)
	}
	rest := form[from+len(marker):]
	return html.UnescapeString(rest[:strings.Index(rest, `"`)])
}
