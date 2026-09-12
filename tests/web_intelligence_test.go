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
	handler, database, _, session, csrfCookie := intelligenceHandler(t)
	ctx := context.Background()
	// The neighbour hides a fleet, defenses and a very recognisable stock.
	setUnits(t, ctx, database, 2, "light_fighter", 987654)
	setUnits(t, ctx, database, 2, "rocket_launcher", 876543)
	setResources(t, ctx, database, 2, 765432, 0, 0)

	page := getPage(t, handler, "/galaxy/1/1", session, csrfCookie)
	if !strings.Contains(page, "Bob") || !strings.Contains(page, "1:1:1") {
		t.Fatalf("galaxy page = %q", page)
	}
	if row := galaxyRowOf(t, page, "1:1:1"); !strings.Contains(row, `/art/body/planet-1`) {
		t.Fatalf("the planet in position 1 has no orbital portrait: %q", row)
	}
	for _, secret := range []string{"987654", "876543", "765432", "light_fighter", "rocket_launcher"} {
		if strings.Contains(page, secret) {
			t.Fatalf("the galaxy page leaked %q", secret)
		}
	}
	if !strings.Contains(page, "Espionner") || !strings.Contains(page, "Attaquer") ||
		!strings.Contains(page, "mission=espionage") || !strings.Contains(page, "mission=attack") {
		t.Fatalf("the galaxy page offers no hostile fleet shortcuts: %q", page)
	}

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
	attackDetail := getPage(t, handler, "/reports/1", session, csrfCookie)
	if !strings.Contains(attackDetail, "Préparer une attaque") || !strings.Contains(attackDetail, "report=1") {
		t.Fatalf("an attack report has no new-attack shortcut: %q", attackDetail)
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
		BattleSimulation: universe.BattleSimulation,
	})
	if err != nil {
		t.Fatal(err)
	}
	return handler, database, universe, &http.Cookie{Name: "uaw_session", Value: "session"}, &http.Cookie{Name: "uaw_csrf", Value: "csrf-token"}
}

// A mission that reached every threshold against a planet holding nothing says
// so. Answering "inconnu" there would hide the very thing the probes were spent
// to learn: that the neighbour has neither fleet nor defence.
func TestWebReportsTellAnEmptyPlanetFromAnUnseenOne(t *testing.T) {
	handler, database, universe, session, csrfCookie := intelligenceHandler(t)
	ctx := context.Background()
	// Bob keeps no ship, no defence and no research, and five probes behind four
	// levels of espionage reach every threshold.
	setResearch(t, ctx, database, 1, "espionage_technology", 4)

	spy, err := universe.Fleet.Launch(ctx, appauth.Principal{AccountID: 1}, 1, appfleet.LaunchRequest{
		Target: coordinateOf(t, 1, 1, 1), TargetKind: domainfleet.TargetPlanet, Mission: domainfleet.MissionEspionage,
		Composition: domainfleet.Composition{unit.EspionageProbe: 5}, Percent: 100,
	}, "complete-spy")
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	universe.Clock.Set(spy.ArrivesAt)
	if _, err := universe.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}

	detail := getPage(t, handler, "/reports/1", session, csrfCookie)
	if strings.Contains(detail, "inconnu") {
		t.Fatalf("a report that saw everything claims to ignore something: %q", detail)
	}
	for _, said := range []string{"aucun vaisseau", "aucune défense", "aucune recherche"} {
		if !strings.Contains(detail, said) {
			t.Fatalf("the report does not say %q: %q", said, detail)
		}
	}
	for _, shortcut := range []string{"Préparer une attaque", "mission=attack", "report=1"} {
		if !strings.Contains(detail, shortcut) {
			t.Fatalf("the report has no %q shortcut: %q", shortcut, detail)
		}
	}

	// The attacking composition is chosen in the ordinary fleet wizard. Its
	// confirmation automatically adds the estimate from this report.
	if spy.ReturnsAt == nil {
		t.Fatal("the probes have no return")
	}
	universe.Clock.Set(*spy.ReturnsAt)
	if _, err := universe.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatal(err)
	}
	setUnits(t, ctx, database, 1, "light_fighter", 3)
	preview := postFormRequest("/planets/1/fleet/preview", url.Values{
		"csrf_token":                 {"csrf-token"},
		"report_id":                  {"1"},
		"galaxy":                     {"1"},
		"system":                     {"1"},
		"position":                   {"1"},
		"mission":                    {"attack"},
		"speed":                      {"100"},
		"composition[light_fighter]": {"3"},
	})
	preview.AddCookie(session)
	preview.AddCookie(csrfCookie)
	confirmation := httptest.NewRecorder()
	handler.ServeHTTP(confirmation, preview)
	if confirmation.Code != http.StatusOK {
		t.Fatalf("simulation preview = %d %q", confirmation.Code, confirmation.Body.String())
	}
	for _, shown := range []string{
		"Simulation de bataille", "Victoire 100 %", "Butin estimé", "état actuel de la cible",
		"Aucun vaisseau perdu", "Aucune défense perdue", "Débris issus des vaisseaux", "Débris issus des défenses",
	} {
		if !strings.Contains(confirmation.Body.String(), shown) {
			t.Fatalf("simulation does not show %q: %q", shown, confirmation.Body.String())
		}
	}
	// The document keeps the difference too, or the next reader loses it again.
	assertSingleText(t, database,
		"SELECT json_type(payload, '$.fleet') FROM reports WHERE id = 1", "object")
}

// A report written before the document could tell an empty section from an
// absent one is read through the level it recorded, so an old report repairs
// itself instead of claiming an ignorance it never had.
func TestWebReportsReadAnOlderDocumentThroughItsLevel(t *testing.T) {
	handler, database, _, session, csrfCookie := intelligenceHandler(t)
	if _, err := database.Write().ExecContext(context.Background(), `
		INSERT INTO reports(recipient_player_id, kind, subject_type, subject_id, galaxy, system, position,
			occurred_at, payload_version, payload, created_at)
		VALUES (1, 'espionage', 'fleet', 1, 1, 1, 1, '2042-09-10T11:12:13Z', 1, ?, '2042-09-10T11:12:13Z')
	`, `{"target":{"Galaxy":1,"System":1,"Position":1},"target_player_name":"Bob",`+
		`"target_planet_name":"Planète mère","probes":10,"level":9,"probes_lost":false,`+
		`"resources":{"Metal":100,"Crystal":50,"Deuterium":10},"buildings":{"metal_mine":11}}`); err != nil {
		t.Fatalf("insert an older report: %v", err)
	}

	detail := getPage(t, handler, "/reports/1", session, csrfCookie)
	if strings.Contains(detail, "inconnu") {
		t.Fatalf("a level 9 report still claims ignorance: %q", detail)
	}
	for _, said := range []string{"aucun vaisseau", "aucune défense", "aucune recherche", "metal_mine 11"} {
		if !strings.Contains(detail, said) {
			t.Fatalf("the repaired report does not say %q: %q", said, detail)
		}
	}
}
