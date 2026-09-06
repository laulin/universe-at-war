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

// TestWebAllianceFlowFromFoundingToSharing walks the alliance page the way a
// player does: found, invite, accept, promote, declare, share.
func TestWebAllianceFlowFromFoundingToSharing(t *testing.T) {
	ctx := context.Background()
	database, universeWorld := allianceWeb(t, 2)
	founder := allianceHandler(t, universeWorld, appauth.Principal{AccountID: 1, Username: "player1"})
	guest := allianceHandler(t, universeWorld, appauth.Principal{AccountID: 2, Username: "player2"})
	session := &http.Cookie{Name: "uaw_session", Value: "session"}
	csrfCookie := &http.Cookie{Name: "uaw_csrf", Value: "csrf-token"}

	page := getPage(t, founder, "/alliance", session, csrfCookie)
	if !strings.Contains(page, "Fonder une alliance") {
		t.Fatalf("alliance page = %q", page)
	}
	postForm(t, founder, "/alliance", url.Values{
		"csrf_token": {"csrf-token"}, "name": {"Les Corsaires"}, "tag": {"cor"}, "description": {"Nous volons haut"},
	}, http.StatusSeeOther, session, csrfCookie)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM alliances", 1)

	// A refused action explains itself on the page instead of vanishing.
	body := postForm(t, founder, "/alliance", url.Values{
		"csrf_token": {"csrf-token"}, "name": {"Les Corsaires"}, "tag": {"abc"},
	}, http.StatusBadRequest, session, csrfCookie)
	if !strings.Contains(body, "appartient déjà à une alliance") {
		t.Fatalf("second founding = %q", body)
	}

	postForm(t, founder, "/alliance/invite", url.Values{
		"csrf_token": {"csrf-token"}, "player": {"player2"},
	}, http.StatusSeeOther, session, csrfCookie)
	invited := getPage(t, guest, "/alliance", session, csrfCookie)
	if !strings.Contains(invited, "Les Corsaires") || !strings.Contains(invited, "Accepter") {
		t.Fatalf("invitation page = %q", invited)
	}
	postForm(t, guest, "/alliance/invitations/1", url.Values{
		"csrf_token": {"csrf-token"}, "answer": {"accept"},
	}, http.StatusSeeOther, session, csrfCookie)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM alliance_members", 2)

	// A plain member sees the alliance but is offered none of its levers.
	member := getPage(t, guest, "/alliance", session, csrfCookie)
	if !strings.Contains(member, "Les Corsaires") {
		t.Fatalf("member page = %q", member)
	}
	if strings.Contains(member, "/alliance/invite") || strings.Contains(member, "/alliance/diplomacy") {
		t.Fatalf("a member is offered actions they cannot take: %q", member)
	}
	// And the server refuses them even when the form is forged by hand.
	refused := postForm(t, guest, "/alliance/invite", url.Values{
		"csrf_token": {"csrf-token"}, "player": {"player1"},
	}, http.StatusBadRequest, session, csrfCookie)
	if !strings.Contains(refused, "rang ne permet pas") {
		t.Fatalf("forged invitation = %q", refused)
	}

	postForm(t, founder, "/alliance/members/2/role", url.Values{
		"csrf_token": {"csrf-token"}, "role": {"officer"},
	}, http.StatusSeeOther, session, csrfCookie)
	assertSingleText(t, database, "SELECT role FROM alliance_members WHERE player_id = 2", "officer")

	// Diplomacy, history and the shared shelf all show up on the page.
	full := getPage(t, founder, "/alliance", session, csrfCookie)
	for _, expected := range []string{"Diplomatie", "Historique", "Rapports partagés", "Opérations groupées"} {
		if !strings.Contains(full, expected) {
			t.Fatalf("the alliance page misses %q: %q", expected, full)
		}
	}
	_ = ctx
}

// TestWebReportSharingIsExplicit proves a report reaches the alliance only when
// its owner says so, and never the other way round.
func TestWebReportSharingIsExplicit(t *testing.T) {
	ctx := context.Background()
	database, universeWorld := allianceWeb(t, 2)
	founder := allianceHandler(t, universeWorld, appauth.Principal{AccountID: 1, Username: "player1"})
	guest := allianceHandler(t, universeWorld, appauth.Principal{AccountID: 2, Username: "player2"})
	session := &http.Cookie{Name: "uaw_session", Value: "session"}
	csrfCookie := &http.Cookie{Name: "uaw_csrf", Value: "csrf-token"}
	joinAlliance(t, ctx, universeWorld, appauth.Principal{AccountID: 1}, appauth.Principal{AccountID: 2}, "player2")

	if _, err := database.Write().ExecContext(ctx, `
		INSERT INTO reports(recipient_player_id, kind, subject_type, subject_id, galaxy, system, position,
			occurred_at, payload_version, payload, created_at)
		VALUES (1, 'espionage', 'planet', 2, 1, 1, 2, '2042-09-10T11:12:13Z', 1,
			json_object('target_planet_name', 'Planète mère'), '2042-09-10T11:12:13Z')
	`); err != nil {
		t.Fatal(err)
	}

	page := getPage(t, founder, "/reports/1", session, csrfCookie)
	if !strings.Contains(page, "Partager avec l'alliance") {
		t.Fatalf("report page = %q", page)
	}
	// Until it is shared, the ally cannot open it at all.
	if code := statusOf(t, guest, "/reports/1", session, csrfCookie); code != http.StatusNotFound {
		t.Fatalf("GET an unshared report = %d", code)
	}
	postForm(t, founder, "/reports/1/share", url.Values{
		"csrf_token": {"csrf-token"}, "shared": {"1"},
	}, http.StatusSeeOther, session, csrfCookie)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM reports WHERE shared_alliance_id IS NOT NULL", 1)

	shared := getPage(t, guest, "/alliance", session, csrfCookie)
	if !strings.Contains(shared, "/reports/1") {
		t.Fatalf("the shared shelf is empty: %q", shared)
	}
	if code := statusOf(t, guest, "/reports/1", session, csrfCookie); code != http.StatusOK {
		t.Fatalf("GET a shared report = %d", code)
	}
	// A stranger's report is never sharable by somebody else.
	if code := postFormStatus(t, guest, "/reports/1/share", url.Values{
		"csrf_token": {"csrf-token"}, "shared": {"0"},
	}, session, csrfCookie); code != http.StatusNotFound {
		t.Fatalf("POST share on somebody else's report = %d", code)
	}
}

// TestWebOperationPageShowsParticipantsAndTiming proves the preparation page
// tells the player who is in and what their fleet does to the schedule, and
// that it stays invisible to anybody outside the alliance.
func TestWebOperationPageShowsParticipantsAndTiming(t *testing.T) {
	ctx := context.Background()
	database, universeWorld := allianceWeb(t, 3)
	alice := appauth.Principal{AccountID: 1}
	bob := appauth.Principal{AccountID: 2}
	joinAlliance(t, ctx, universeWorld, alice, bob, "player2")
	aliceHome := homeOf(t, ctx, universeWorld, alice)
	bobHome := homeOf(t, ctx, universeWorld, bob)
	targetBody := homeOf(t, ctx, universeWorld, appauth.Principal{AccountID: 3})
	for _, planet := range []int64{aliceHome.ID, bobHome.ID} {
		setUnits(t, ctx, database, planet, "cruiser", 8)
		setResources(t, ctx, database, planet, 50000, 50000, 50000)
	}
	group, err := universeWorld.ACS.Create(ctx, alice, aliceHome.ID,
		fleetOf(targetBody.Coordinate, "cruiser", 4), "open")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	leader := allianceHandler(t, universeWorld, appauth.Principal{AccountID: 1, Username: "player1"})
	ally := allianceHandler(t, universeWorld, appauth.Principal{AccountID: 2, Username: "player2"})
	stranger := allianceHandler(t, universeWorld, appauth.Principal{AccountID: 3, Username: "player3"})
	session := &http.Cookie{Name: "uaw_session", Value: "session"}
	csrfCookie := &http.Cookie{Name: "uaw_csrf", Value: "csrf-token"}

	page := getPage(t, ally, "/alliance/operations/1", session, csrfCookie)
	if !strings.Contains(page, "player1") || !strings.Contains(page, targetBody.Coordinate.String()) {
		t.Fatalf("operation page = %q", page)
	}
	if !strings.Contains(page, "Calculer l'impact") {
		t.Fatalf("the operation page offers no preparation: %q", page)
	}

	preview := postForm(t, ally, "/alliance/operations/1/preview", url.Values{
		"csrf_token": {"csrf-token"}, "planet": {"2"}, "galaxy": {"1"}, "system": {"1"},
		"position": {"3"}, "mission": {"attack"}, "speed": {"50"}, "composition[cruiser]": {"4"},
	}, http.StatusOK, session, csrfCookie)
	if !strings.Contains(preview, "Arrivée du groupe") || !strings.Contains(preview, "retarde tout le groupe") {
		t.Fatalf("preview = %q", preview)
	}

	postForm(t, ally, "/alliance/operations/1/join", url.Values{
		"csrf_token": {"csrf-token"}, "idempotency_key": {"join-1"}, "planet": {"2"},
		"galaxy": {"1"}, "system": {"1"}, "position": {"3"}, "mission": {"attack"},
		"speed": {"50"}, "composition[cruiser]": {"4"},
	}, http.StatusSeeOther, session, csrfCookie)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM acs_participants", 2)
	joined := getPage(t, leader, "/alliance/operations/1", session, csrfCookie)
	if !strings.Contains(joined, "player2") || !strings.Contains(joined, "Retirer ma flotte") {
		t.Fatalf("joined page = %q", joined)
	}

	// Nobody outside the alliance may look at the operation, or slip into it.
	if code := statusOf(t, stranger, "/alliance/operations/1", session, csrfCookie); code != http.StatusNotFound {
		t.Fatalf("GET an operation of another alliance = %d", code)
	}
	if code := postFormStatus(t, stranger, "/alliance/operations/1/join", url.Values{
		"csrf_token": {"csrf-token"}, "idempotency_key": {"intruder"}, "planet": {"3"},
		"galaxy": {"1"}, "system": {"1"}, "position": {"3"}, "mission": {"attack"},
		"speed": {"100"}, "composition[cruiser]": {"1"},
	}, session, csrfCookie); code != http.StatusNotFound {
		t.Fatalf("POST join from another alliance = %d", code)
	}
	// A withdrawal takes one's own fleet out, never somebody else's.
	if code := postFormStatus(t, leader, "/fleets/2/withdraw", url.Values{
		"csrf_token": {"csrf-token"},
	}, session, csrfCookie); code != http.StatusNotFound {
		t.Fatalf("POST withdraw of another fleet = %d", code)
	}
	postForm(t, ally, "/fleets/2/withdraw", url.Values{"csrf_token": {"csrf-token"}},
		http.StatusSeeOther, session, csrfCookie)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM acs_participants", 1)
	_ = group
}

func allianceWeb(t *testing.T, players int) (*storagesqlite.Database, *world) {
	t.Helper()
	ctx := context.Background()
	database := economyDatabase(t, ctx, players)
	universeWorld := newWorld(t, database, appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC)))
	for index := 1; index <= players; index++ {
		if _, err := universeWorld.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: int64(index)},
			playerDisplayName(index)); err != nil {
			t.Fatalf("CreateEmpire() error = %v", err)
		}
	}
	return database, universeWorld
}

func allianceHandler(t *testing.T, universeWorld *world, principal appauth.Principal) http.Handler {
	t.Helper()
	handler, err := webhandler.New(webhandler.Dependencies{
		Authentication: webAuthenticationStub{principal: principal},
		ServerState:    runningStateStub{}, CSRFSecrets: sequenceSecret{value: "csrf-token"},
		Economy: universeWorld.Economy, Fleet: universeWorld.Fleet, Reports: universeWorld.Reports,
		Alliance: universeWorld.Alliance, ACS: universeWorld.ACS,
	})
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

// postForm posts a form, checks the status and returns the body.
func postForm(t *testing.T, handler http.Handler, target string, values url.Values, want int, cookies ...*http.Cookie) string {
	t.Helper()
	request := postFormRequest(target, values)
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != want {
		t.Fatalf("POST %s = %d, want %d: %s", target, recorder.Code, want, recorder.Body.String())
	}
	return recorder.Body.String()
}

func postFormStatus(t *testing.T, handler http.Handler, target string, values url.Values, cookies ...*http.Cookie) int {
	t.Helper()
	request := postFormRequest(target, values)
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder.Code
}

func statusOf(t *testing.T, handler http.Handler, target string, cookies ...*http.Cookie) int {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder.Code
}
