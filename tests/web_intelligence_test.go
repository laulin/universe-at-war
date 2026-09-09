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
	appfleet "universeatwar/internal/app/fleet"
	appclock "universeatwar/internal/clock"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/unit"
	storagesqlite "universeatwar/internal/storage/sqlite"
	webhandler "universeatwar/internal/web"
)

func TestWebGalaxyShowsOnlyPublicInformation(t *testing.T) {
	handler, database, universe, session, csrfCookie := intelligenceHandler(t)
	ctx := context.Background()
	// The neighbour hides a fleet, defenses and a very recognisable stock.
	setUnits(t, ctx, database, 2, "light_fighter", 987654)
	setUnits(t, ctx, database, 2, "rocket_launcher", 876543)
	setResources(t, ctx, database, 2, 765432, 0, 0)

	page := getPage(t, handler, "/galaxy/1/1", session, csrfCookie)
	if !strings.Contains(page, "Bob") || !strings.Contains(page, "1:1:1") {
		t.Fatalf("galaxy page = %q", page)
	}
	for _, secret := range []string{"987654", "876543", "765432", "light_fighter", "rocket_launcher"} {
		if strings.Contains(page, secret) {
			t.Fatalf("the galaxy page leaked %q", secret)
		}
	}
	if !strings.Contains(page, "Espionner") {
		t.Fatalf("the galaxy page offers no quick espionage: %q", page)
	}

	spy := postFormRequest("/galaxy/1/1/1/spy", url.Values{
		"csrf_token": {"csrf-token"}, "planet": {"1"}, "probes": {"2"}, "idempotency_key": {"spy-1"},
	})
	spy.AddCookie(session)
	spy.AddCookie(csrfCookie)
	launched := httptest.NewRecorder()
	handler.ServeHTTP(launched, spy)
	if launched.Code != http.StatusSeeOther || launched.Header().Get("Location") != "/galaxy/1/1" {
		t.Fatalf("POST spy = %d %q", launched.Code, launched.Body.String())
	}
	assertSingleText(t, database, "SELECT mission FROM fleets WHERE id = 1", "espionage")
	_ = universe
}

func TestWebReportsStayPrivateAndHideUnrevealedSections(t *testing.T) {
	handler, database, universe, session, csrfCookie := intelligenceHandler(t)
	ctx := context.Background()
	setUnits(t, ctx, database, 2, "light_fighter", 987654)
	setResources(t, ctx, database, 2, 765432, 0, 0)

	// A single probe against a defender with more espionage reveals nothing.
	setResearch(t, ctx, database, 2, "espionage_technology", 5)
	spy, err := universe.Fleet.Launch(ctx, appauth.Principal{AccountID: 1}, 1, appfleet.LaunchRequest{
		Target: coordinateOf(t, 1, 1, 1), TargetKind: domainfleet.TargetPlanet, Mission: domainfleet.MissionEspionage,
		Composition: domainfleet.Composition{unit.EspionageProbe: 1}, Percent: 100,
	}, "blind-spy")
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	universe.Clock.Set(spy.ArrivesAt)
	if _, err := universe.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}

	list := getPage(t, handler, "/reports", session, csrfCookie)
	if !strings.Contains(list, "espionnage") || !strings.Contains(list, `href="/reports/1"`) {
		t.Fatalf("reports page = %q", list)
	}
	detail := getPage(t, handler, "/reports/1", session, csrfCookie)
	if !strings.Contains(detail, "inconnu") {
		t.Fatalf("a blind report does not say what it ignores: %q", detail)
	}
	for _, secret := range []string{"987654", "765432", "light_fighter"} {
		if strings.Contains(detail, secret) {
			t.Fatalf("a blind report leaked %q", secret)
		}
	}

	read := postFormRequest("/reports/1/read", url.Values{"csrf_token": {"csrf-token"}})
	read.AddCookie(session)
	read.AddCookie(csrfCookie)
	marked := httptest.NewRecorder()
	handler.ServeHTTP(marked, read)
	if marked.Code != http.StatusSeeOther {
		t.Fatalf("POST read = %d", marked.Code)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM reports WHERE read_at IS NOT NULL", 1)

	// Bob owns the detection report and Alice must not reach it.
	assertSingleValue(t, database, "SELECT COUNT(*) FROM reports WHERE recipient_player_id = 2", 1)
	foreign := httptest.NewRequest(http.MethodGet, "/reports/2", nil)
	foreign.AddCookie(session)
	foreign.AddCookie(csrfCookie)
	denied := httptest.NewRecorder()
	handler.ServeHTTP(denied, foreign)
	if denied.Code != http.StatusNotFound {
		t.Fatalf("GET a foreign report = %d, want 404", denied.Code)
	}
}

func TestWebHostileReportsRaiseAnAlertInTheLayout(t *testing.T) {
	handler, database, universe, session, csrfCookie := intelligenceHandler(t)
	ctx := context.Background()
	setUnits(t, ctx, database, 1, "light_fighter", 5)
	setUnits(t, ctx, database, 2, "rocket_launcher", 1)

	attack, err := universe.Fleet.Launch(ctx, appauth.Principal{AccountID: 1}, 1, appfleet.LaunchRequest{
		Target: coordinateOf(t, 1, 1, 1), TargetKind: domainfleet.TargetPlanet, Mission: domainfleet.MissionAttack,
		Composition: domainfleet.Composition{unit.LightFighter: 5}, Percent: 100,
	}, "attack")
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	universe.Clock.Set(attack.ArrivesAt)
	if _, err := universe.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}

	// Alice attacked, so her own layout shows no alert.
	attacker := getPage(t, handler, "/reports", session, csrfCookie)
	if strings.Contains(attacker, `⚠ 1`) {
		t.Fatalf("the attacker sees a hostile alert: %q", attacker)
	}

	defenderHandler, err := webhandler.New(webhandler.Dependencies{
		Authentication: webAuthenticationStub{principal: appauth.Principal{AccountID: 2, Username: "player2"}},
		ServerState:    runningStateStub{}, CSRFSecrets: sequenceSecret{value: "csrf-token"},
		Economy: universe.Economy, Reports: universe.Reports, Galaxy: universe.Galaxy,
	})
	if err != nil {
		t.Fatal(err)
	}
	defender := getPage(t, defenderHandler, "/reports", session, csrfCookie)
	if !strings.Contains(defender, "⚠ 1") || !strings.Contains(defender, "Rapport de défense") {
		t.Fatalf("the defender sees no hostile alert: %q", defender)
	}
}

func intelligenceHandler(t *testing.T) (http.Handler, *storagesqlite.Database, *world, *http.Cookie, *http.Cookie) {
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
	setResources(t, ctx, database, 1, 5000, 5000, 5000)
	setUnits(t, ctx, database, 1, "espionage_probe", 5)
	handler, err := webhandler.New(webhandler.Dependencies{
		Authentication: webAuthenticationStub{principal: appauth.Principal{AccountID: 1, Username: "player1"}},
		ServerState:    runningStateStub{}, CSRFSecrets: sequenceSecret{value: "csrf-token"},
		Economy: universe.Economy, Fleet: universe.Fleet, Galaxy: universe.Galaxy, Reports: universe.Reports,
	})
	if err != nil {
		t.Fatal(err)
	}
	return handler, database, universe, &http.Cookie{Name: "uaw_session", Value: "session"}, &http.Cookie{Name: "uaw_csrf", Value: "csrf-token"}
}

// The map mints one key per button per rendering, so the same neighbour may be
// spied again from a fresh page. A key made of the target alone turned the
// second click into a replay of the first: no fleet, no report, no error.
func TestWebGalaxySpiesTheSameNeighbourTwice(t *testing.T) {
	handler, database, _, session, csrfCookie := intelligenceHandler(t)
	// A second slot, or the second espionage would be refused for want of one.
	setResearch(t, context.Background(), database, 1, "computer_technology", 1)

	keys := map[string]bool{}
	for range 2 {
		page := getPage(t, handler, "/galaxy/1/1", session, csrfCookie)
		key := formValue(t, page, `action="/galaxy/1/1/1/spy"`, "idempotency_key")
		keys[key] = true
		spy := postFormRequest("/galaxy/1/1/1/spy", url.Values{
			"csrf_token": {"csrf-token"}, "planet": {"1"}, "probes": {"1"}, "idempotency_key": {key},
		})
		spy.AddCookie(session)
		spy.AddCookie(csrfCookie)
		launched := httptest.NewRecorder()
		handler.ServeHTTP(launched, spy)
		if launched.Code != http.StatusSeeOther {
			t.Fatalf("POST spy = %d %q", launched.Code, launched.Body.String())
		}
	}
	if len(keys) != 2 {
		t.Fatal("two renderings of the map carry one espionage key")
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM fleets WHERE mission = 'espionage'", 2)
}
