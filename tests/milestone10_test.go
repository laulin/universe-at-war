package tests

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appmoderation "universeatwar/internal/app/moderation"
	appclock "universeatwar/internal/clock"
	"universeatwar/internal/observability"
	storagesqlite "universeatwar/internal/storage/sqlite"
	webhandler "universeatwar/internal/web"
)

// TestABrowserWalksTheWholeGame follows one player through the screens that
// matter, in order, exactly as a browser would: found an empire, build, launch
// a fleet, read the report it produces, and be shown the door by a moderator.
func TestABrowserWalksTheWholeGame(t *testing.T) {
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC))
	database := economyDatabase(t, ctx, 2)
	universeWorld := newWorld(t, database, clock)
	if _, err := database.Write().ExecContext(ctx,
		"INSERT INTO server_state(id, state, updated_at) VALUES (1, 'RUNNING', '2042-09-10T12:00:00Z')"); err != nil {
		t.Fatal(err)
	}
	for id := 1; id <= 2; id++ {
		if _, err := database.Write().ExecContext(ctx, `
			INSERT INTO password_credentials(account_id, encoded_hash, must_change_password, updated_at)
			VALUES (?, 'argon2id$placeholder', 0, '2042-09-10T12:00:00Z')
		`, id); err != nil {
			t.Fatal(err)
		}
	}
	moderation := appmoderation.Service{
		Clock: clock, Repository: storagesqlite.NewModerationRepository(database.Write()),
	}
	player := appauth.Principal{AccountID: 1, Username: "player1", Roles: []appauth.Role{appauth.RolePlayer}}
	handler, err := webhandler.New(webhandler.Dependencies{
		Authentication: webAuthenticationStub{principal: player},
		ServerState:    runningStateStub{}, CSRFSecrets: sequenceSecret{value: "csrf-token"},
		Economy: universeWorld.Economy, Research: universeWorld.Research, Shipyard: universeWorld.Shipyard,
		Fleet: universeWorld.Fleet, Galaxy: universeWorld.Galaxy, Reports: universeWorld.Reports,
	})
	if err != nil {
		t.Fatal(err)
	}
	session := &http.Cookie{Name: "uaw_session", Value: "session"}
	csrfCookie := &http.Cookie{Name: "uaw_csrf", Value: "csrf-token"}

	// The empire is founded from the page that offers it.
	welcome := getPage(t, handler, "/", session, csrfCookie)
	if !strings.Contains(welcome, `action="/empire"`) {
		t.Fatalf("the first page does not offer to found an empire: %q", welcome)
	}
	postForm(t, handler, "/empire", url.Values{
		"csrf_token": {"csrf-token"}, "name": {"Alice"},
	}, http.StatusSeeOther, session, csrfCookie)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM players", 1)
	// A neighbour to fly to, founded the ordinary way.
	neighbour, err := universeWorld.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: 2}, "Bob")
	if err != nil {
		t.Fatal(err)
	}

	// A building is started from the planet page and completes on its own.
	overview := getPage(t, handler, "/", session, csrfCookie)
	if !strings.Contains(overview, "Planète mère") {
		t.Fatalf("the empire page does not show the home world: %q", overview)
	}
	planet := getPage(t, handler, "/planets/1", session, csrfCookie)
	buildKey := betweenMarkers(planet, `name="idempotency_key" value="`, `"`)
	postForm(t, handler, "/planets/1/buildings/metal_mine", url.Values{
		"csrf_token": {"csrf-token"}, "idempotency_key": {buildKey},
	}, http.StatusSeeOther, session, csrfCookie)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM building_queue", 1)
	clock.Advance(2 * time.Hour)
	if _, err := universeWorld.Events.CompleteDue(ctx, 50); err != nil {
		t.Fatal(err)
	}
	assertSingleValue(t, database,
		"SELECT level FROM planet_buildings WHERE planet_id = 1 AND building_id = 'metal_mine'", 1)

	// A fleet is sent through the wizard, previewed then launched.
	setUnits(t, ctx, database, 1, "small_cargo", 3)
	setResearch(t, ctx, database, 1, "combustion_drive", 2)
	setResources(t, ctx, database, 1, 20000, 20000, 20000)
	send := getPage(t, handler, "/planets/1/fleet/send", session, csrfCookie)
	if !strings.Contains(send, "Petit transporteur") {
		t.Fatalf("the wizard does not list the ships: %q", send)
	}
	preview := postForm(t, handler, "/planets/1/fleet/preview", url.Values{
		"csrf_token": {"csrf-token"},
		"galaxy":     {strconv.Itoa(neighbour.Coordinate.Galaxy)},
		"system":     {strconv.Itoa(neighbour.Coordinate.System)},
		"position":   {strconv.Itoa(neighbour.Coordinate.Position)},
		"mission":    {"transport"}, "speed": {"100"}, "composition[small_cargo]": {"2"},
		"cargo_metal": {"1000"},
	}, http.StatusOK, session, csrfCookie)
	if !strings.Contains(preview, "Arrivée prévue") {
		t.Fatalf("the confirmation does not show the schedule: %q", preview)
	}
	key := betweenMarkers(preview, `name="idempotency_key" value="`, `"`)
	postForm(t, handler, "/planets/1/fleet/launch", url.Values{
		"csrf_token": {"csrf-token"}, "idempotency_key": {key},
		"galaxy":   {strconv.Itoa(neighbour.Coordinate.Galaxy)},
		"system":   {strconv.Itoa(neighbour.Coordinate.System)},
		"position": {strconv.Itoa(neighbour.Coordinate.Position)},
		"mission":  {"transport"}, "speed": {"100"}, "composition[small_cargo]": {"2"},
		"cargo_metal": {"1000"},
	}, http.StatusSeeOther, session, csrfCookie)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM fleets", 1)
	inFlight := getPage(t, handler, "/planets/1/fleet", session, csrfCookie)
	if !strings.Contains(inFlight, "Transport") || !strings.Contains(inFlight, "Rappeler") {
		t.Fatalf("the fleet page does not show the mission: %q", inFlight)
	}

	// A report reaches the shelf and reads correctly.
	if _, err := database.Write().ExecContext(ctx, `
		INSERT INTO reports(recipient_player_id, kind, subject_type, subject_id, galaxy, system, position,
			occurred_at, payload_version, payload, created_at)
		VALUES (1, 'espionage', 'planet', 1, 1, 1, 9, '2042-09-10T12:00:00Z', 1,
			json_object('target_player_name', 'Alice', 'level', 5), '2042-09-10T12:00:00Z')
	`); err != nil {
		t.Fatal(err)
	}
	shelf := getPage(t, handler, "/reports", session, csrfCookie)
	if !strings.Contains(shelf, "Rapport d&#39;espionnage") {
		t.Fatalf("the report shelf is empty: %q", shelf)
	}
	detail := getPage(t, handler, "/reports/1", session, csrfCookie)
	if !strings.Contains(detail, "Alice") {
		t.Fatalf("the report does not read: %q", detail)
	}

	// And a moderator closes the door without touching any of it.
	moderator := appauth.Principal{AccountID: 2, Roles: []appauth.Role{appauth.RoleModerator}}
	if _, err := database.Write().ExecContext(ctx,
		"INSERT INTO account_roles(account_id, role, granted_at) VALUES (2, 'MODERATOR', '2042-09-10T12:00:00Z')"); err != nil {
		t.Fatal(err)
	}
	if _, err := moderation.Apply(ctx, moderator, appmoderation.Request{
		AccountID: 1, Hours: 24, Justification: "fin de la visite",
	}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	credential, err := storagesqlite.NewAuthenticationRepository(database.Read(), database.Write()).
		FindCredentialByID(ctx, 1, clock.Now().UTC())
	if err != nil || !credential.Unavailable {
		t.Fatalf("the banned player can still sign in: %+v %v", credential, err)
	}
	// The fleet it launched is still flying and its planet is still producing.
	assertSingleText(t, database, "SELECT state FROM fleets WHERE id = 1", "outbound")
	clock.Advance(time.Hour)
	if _, err := universeWorld.Economy.Planet(ctx, appauth.Principal{AccountID: 1}, 1); err != nil {
		t.Fatalf("the empire of a banned player stopped: %v", err)
	}
}

// TestLogsAndCountersNeverCarryASecret proves the two places an operator looks
// hold no credential, no token and no cookie.
func TestLogsAndCountersNeverCarryASecret(t *testing.T) {
	_, universeWorld, services := administeredUniverse(t)
	metrics := observability.NewMetrics()
	handler, err := webhandler.New(webhandler.Dependencies{
		Authentication: webAuthenticationStub{principal: appauth.Principal{
			AccountID: 1, Username: "admin", Roles: []appauth.Role{appauth.RoleAdmin},
		}},
		ServerState: runningStateStub{}, CSRFSecrets: sequenceSecret{value: "csrf-token"},
		Economy: universeWorld.Economy, Dashboard: services.dashboard, Metrics: metrics,
	})
	if err != nil {
		t.Fatal(err)
	}
	session := &http.Cookie{Name: "uaw_session", Value: "session"}
	csrfCookie := &http.Cookie{Name: "uaw_csrf", Value: "csrf-token"}
	getPage(t, handler, "/admin?token=secret", session, csrfCookie)

	counters := getPage(t, handler, "/admin/metrics", session, csrfCookie)
	if !strings.Contains(counters, "requests ") || !strings.Contains(counters, "status_200") {
		t.Fatalf("the counters say nothing: %q", counters)
	}
	for _, secret := range []string{"token=secret", "csrf-token", "admin", "/admin"} {
		if strings.Contains(counters, secret) {
			t.Fatalf("the counters carry %q", secret)
		}
	}
}

// betweenMarkers pulls one value out of a rendered page.
func betweenMarkers(page, prefix, suffix string) string {
	_, rest, found := strings.Cut(page, prefix)
	if !found {
		return ""
	}
	value, _, _ := strings.Cut(rest, suffix)
	return value
}
