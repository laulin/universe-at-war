package tests

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appclock "universeatwar/internal/clock"
	storagesqlite "universeatwar/internal/storage/sqlite"
	webhandler "universeatwar/internal/web"
)

func TestWebFleetLaunchAndRecallWorkflow(t *testing.T) {
	handler, database, session, csrfCookie := fleetHandler(t)
	ctx := context.Background()
	setUnits(t, ctx, database, 1, "small_cargo", 2)
	setResources(t, ctx, database, 1, 5000, 500, 200)

	page := getPage(t, handler, "/planets/1/fleet", session, csrfCookie)
	if !strings.Contains(page, "Petit transporteur") || !strings.Contains(page, "Aucune flotte en vol") {
		t.Fatalf("fleet page = %q", page)
	}
	send := getPage(t, handler, "/planets/1/fleet/send", session, csrfCookie)
	if !strings.Contains(send, `name="composition[small_cargo]"`) || !strings.Contains(send, `name="galaxy"`) {
		t.Fatalf("send page = %q", send)
	}

	form := url.Values{
		"csrf_token": {"csrf-token"}, "galaxy": {"1"}, "system": {"1"}, "position": {"1"},
		"mission": {"transport"}, "speed": {"100"}, "composition[small_cargo]": {"2"},
		"cargo_metal": {"400"}, "cargo_crystal": {"0"}, "cargo_deuterium": {"0"},
	}
	preview := postFormRequest("/planets/1/fleet/preview", form)
	preview.AddCookie(session)
	preview.AddCookie(csrfCookie)
	confirmation := httptest.NewRecorder()
	handler.ServeHTTP(confirmation, preview)
	if confirmation.Code != http.StatusOK {
		t.Fatalf("POST preview = %d %q", confirmation.Code, confirmation.Body.String())
	}
	body := confirmation.Body.String()
	if !strings.Contains(body, "Arrivée prévue") || !strings.Contains(body, "Carburant") {
		t.Fatalf("confirmation = %q", body)
	}
	key := hiddenValue(t, body, "idempotency_key")

	launchForm := url.Values{}
	for name, values := range form {
		launchForm[name] = values
	}
	launchForm.Set("idempotency_key", key)
	launch := postFormRequest("/planets/1/fleet/launch", launchForm)
	launch.AddCookie(session)
	launch.AddCookie(csrfCookie)
	launched := httptest.NewRecorder()
	handler.ServeHTTP(launched, launch)
	if launched.Code != http.StatusSeeOther || launched.Header().Get("Location") != "/planets/1/fleet" {
		t.Fatalf("POST launch = %d %q", launched.Code, launched.Header().Get("Location"))
	}

	inFlight := getPage(t, handler, "/planets/1/fleet", session, csrfCookie)
	if !strings.Contains(inFlight, "Transport") || !strings.Contains(inFlight, "data-countdown") {
		t.Fatalf("fleet page in flight = %q", inFlight)
	}
	if !strings.Contains(inFlight, "Rappeler") {
		t.Fatalf("fleet page does not offer a recall: %q", inFlight)
	}

	recall := postFormRequest("/fleets/1/recall", url.Values{"csrf_token": {"csrf-token"}, "idempotency_key": {"recall-1"}})
	recall.AddCookie(session)
	recall.AddCookie(csrfCookie)
	recalled := httptest.NewRecorder()
	handler.ServeHTTP(recalled, recall)
	if recalled.Code != http.StatusSeeOther {
		t.Fatalf("POST recall = %d %q", recalled.Code, recalled.Body.String())
	}
	after := getPage(t, handler, "/planets/1/fleet", session, csrfCookie)
	if strings.Contains(after, "Rappeler") {
		t.Fatalf("a recalled fleet still offers a recall: %q", after)
	}
	// Recalled at the very instant it left, the fleet is already home again.
	assertSingleValue(t, database, "SELECT COUNT(*) FROM fleet_transitions WHERE to_state = 'recalled'", 1)
	assertSingleValue(t, database, "SELECT quantity FROM planet_units WHERE planet_id = 1 AND unit_id = 'small_cargo'", 2)
}

func TestWebFleetRefusesInvalidRequestsAndForeignFleets(t *testing.T) {
	handler, database, session, csrfCookie := fleetHandler(t)
	ctx := context.Background()
	setUnits(t, ctx, database, 1, "small_cargo", 2)
	setResources(t, ctx, database, 1, 5000, 500, 200)

	heavy := url.Values{
		"csrf_token": {"csrf-token"}, "galaxy": {"1"}, "system": {"1"}, "position": {"1"},
		"mission": {"transport"}, "speed": {"100"}, "composition[small_cargo]": {"2"},
		"cargo_metal": {"99999"},
	}
	request := postFormRequest("/planets/1/fleet/preview", heavy)
	request.AddCookie(session)
	request.AddCookie(csrfCookie)
	refused := httptest.NewRecorder()
	handler.ServeHTTP(refused, request)
	if refused.Code != http.StatusBadRequest || !strings.Contains(refused.Body.String(), "capacité") {
		t.Fatalf("overloaded preview = %d %q", refused.Code, refused.Body.String())
	}

	withoutCSRF := postFormRequest("/planets/1/fleet/launch", url.Values{"galaxy": {"1"}})
	withoutCSRF.AddCookie(session)
	denied := httptest.NewRecorder()
	handler.ServeHTTP(denied, withoutCSRF)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("launch without CSRF = %d, want 403", denied.Code)
	}

	stranger, err := webhandler.New(webhandler.Dependencies{
		Authentication: webAuthenticationStub{principal: appauth.Principal{AccountID: 2, Username: "player2"}},
		ServerState:    runningStateStub{}, CSRFSecrets: sequenceSecret{value: "csrf-token"},
		Economy: newWorldFor(t, database).Economy, Fleet: newWorldFor(t, database).Fleet,
	})
	if err != nil {
		t.Fatal(err)
	}
	foreign := httptest.NewRequest(http.MethodGet, "/planets/1/fleet", nil)
	foreign.AddCookie(session)
	foreign.AddCookie(csrfCookie)
	response := httptest.NewRecorder()
	stranger.ServeHTTP(response, foreign)
	if response.Code != http.StatusNotFound {
		t.Fatalf("GET a foreign fleet page = %d, want 404", response.Code)
	}
	if strings.Contains(response.Body.String(), "small_cargo") {
		t.Fatalf("foreign fleet page leaked a composition: %q", response.Body.String())
	}
}

func fleetHandler(t *testing.T) (http.Handler, *storagesqlite.Database, *http.Cookie, *http.Cookie) {
	t.Helper()
	ctx := context.Background()
	database := economyDatabase(t, ctx, 2)
	universe := newWorld(t, database, appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC)))
	if _, err := universe.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: 1}, "Alice"); err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	if _, err := universe.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: 2}, "Bob"); err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	handler, err := webhandler.New(webhandler.Dependencies{
		Authentication: webAuthenticationStub{principal: appauth.Principal{AccountID: 1, Username: "player1"}},
		ServerState:    runningStateStub{}, CSRFSecrets: sequenceSecret{value: "csrf-token"},
		Economy: universe.Economy, Fleet: universe.Fleet,
	})
	if err != nil {
		t.Fatal(err)
	}
	return handler, database, &http.Cookie{Name: "uaw_session", Value: "session"}, &http.Cookie{Name: "uaw_csrf", Value: "csrf-token"}
}
