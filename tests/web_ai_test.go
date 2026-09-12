package tests

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	appai "universeatwar/internal/app/ai"
	appauth "universeatwar/internal/app/authentication"
	appclock "universeatwar/internal/clock"
	domainai "universeatwar/internal/domain/ai"
	storagesqlite "universeatwar/internal/storage/sqlite"
	webhandler "universeatwar/internal/web"
)

// TestWebArtificialAdministrationIsReservedToAdministrators proves the pages
// that create, retire and inspect artificial players answer to nobody else.
func TestWebArtificialAdministrationIsReservedToAdministrators(t *testing.T) {
	database, universeWorld := artificialWeb(t)
	admin := artificialHandler(t, universeWorld, appauth.Principal{
		AccountID: 1, Username: "player1", Roles: []appauth.Role{appauth.RoleAdmin, appauth.RolePlayer},
	})
	player := artificialHandler(t, universeWorld, appauth.Principal{
		AccountID: 2, Username: "player2", Roles: []appauth.Role{appauth.RolePlayer},
	})
	session := &http.Cookie{Name: "uaw_session", Value: "session"}
	csrfCookie := &http.Cookie{Name: "uaw_csrf", Value: "csrf-token"}

	page := getPage(t, admin, "/admin/ai", session, csrfCookie)
	if !strings.Contains(page, "Ajouter un joueur artificiel") || !strings.Contains(page, "cautious_miner") {
		t.Fatalf("administration page = %q", page)
	}
	// A plain player cannot even tell the pages exist.
	for _, route := range []string{"/admin/ai", "/admin/ai/1"} {
		if code := statusOf(t, player, route, session, csrfCookie); code != http.StatusNotFound {
			t.Fatalf("GET %s as a player = %d", route, code)
		}
	}
	if code := postFormStatus(t, player, "/admin/ai", url.Values{
		"csrf_token": {"csrf-token"}, "name": {"Intrus"}, "archetype": {"raider"},
		"start_hour": {"0"}, "end_hour": {"0"}, "interval_minutes": {"5"},
	}, session, csrfCookie); code != http.StatusNotFound {
		t.Fatalf("POST /admin/ai as a player = %d", code)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM ai_profiles", 0)

	postForm(t, admin, "/admin/ai", url.Values{
		"csrf_token": {"csrf-token"}, "name": {"Kepler"}, "archetype": {"cautious_miner"},
		"start_hour": {"8"}, "end_hour": {"23"}, "interval_minutes": {"5"},
	}, http.StatusSeeOther, session, csrfCookie)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM ai_profiles WHERE state = 'active'", 1)

	// A refusal is explained on the page rather than silently swallowed.
	body := postForm(t, admin, "/admin/ai", url.Values{
		"csrf_token": {"csrf-token"}, "name": {"Kepler"}, "archetype": {"cautious_miner"},
		"start_hour": {"8"}, "end_hour": {"23"}, "interval_minutes": {"5"},
	}, http.StatusBadRequest, session, csrfCookie)
	if !strings.Contains(body, "déjà pris") {
		t.Fatalf("duplicate name = %q", body)
	}
	body = postForm(t, admin, "/admin/ai", url.Values{
		"csrf_token": {"csrf-token"}, "name": {"Bulldozer"}, "archetype": {"bulldozer"},
		"start_hour": {"8"}, "end_hour": {"23"}, "interval_minutes": {"5"},
	}, http.StatusBadRequest, session, csrfCookie)
	if !strings.Contains(body, "Archétype inconnu") {
		t.Fatalf("unknown archetype = %q", body)
	}

	// The omniscient view says what it is, and lists the diary.
	detail := getPage(t, admin, "/admin/ai/3", session, csrfCookie)
	if !strings.Contains(detail, "vue omnisciente") || !strings.Contains(detail, "Journal des décisions") {
		t.Fatalf("detail page = %q", detail)
	}
	if !strings.Contains(detail, "Aucun joueur n'y a accès") {
		t.Fatalf("the debug view is not marked as reserved: %q", detail)
	}
	for _, expected := range []string{"Personnalité et comportement", "Rayon d'exploration", "Autoriser les attaques"} {
		if !strings.Contains(detail, expected) {
			t.Fatalf("the AI settings miss %q: %q", expected, detail)
		}
	}
	postForm(t, admin, "/admin/ai/3/settings", url.Values{
		"csrf_token": {"csrf-token"}, "version": {hiddenValue(t, detail, "version")},
		"archetype": {"raider"}, "start_hour": {"6"}, "end_hour": {"22"}, "interval_minutes": {"3"},
		"economy": {"0.35"}, "greed": {"2.5"}, "caution": {"0.5"}, "safety_margin": {"1.2"},
		"raid_threshold": {"500"}, "defence_share": {"0.05"}, "probes": {"4"},
		"search_radius": {"20"}, "batch_size": {"40"}, "fleetsave": {"loaded"},
		"attack_enabled": {"on"}, "espionage_enabled": {"on"}, "recycle_enabled": {"on"},
		"mode": {"custom"},
	}, http.StatusSeeOther, session, csrfCookie)
	assertSingleText(t, database, "SELECT archetype FROM ai_profiles WHERE player_id = 3", "raider")
	assertSingleValue(t, database,
		"SELECT json_extract(tuning, '$.search_radius') FROM ai_profiles WHERE player_id = 3", 20)
	customized := getPage(t, admin, "/admin/ai/3?saved=1", session, csrfCookie)
	if !strings.Contains(customized, "Réglages personnalisés actifs") || !strings.Contains(customized, "comportement de cette IA a été mis à jour") {
		t.Fatalf("the customized personality is not reported: %q", customized)
	}

	postForm(t, admin, "/admin/ai/3/retire", url.Values{"csrf_token": {"csrf-token"}},
		http.StatusSeeOther, session, csrfCookie)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM ai_profiles WHERE state = 'retired'", 1)
	if code := postFormStatus(t, admin, "/admin/ai/999/retire", url.Values{"csrf_token": {"csrf-token"}},
		session, csrfCookie); code != http.StatusNotFound {
		t.Fatalf("retiring nobody = %d", code)
	}
	// Without the token nothing happens at all.
	if code := postFormStatus(t, admin, "/admin/ai", url.Values{
		"name": {"Sans jeton"}, "archetype": {"raider"},
		"start_hour": {"0"}, "end_hour": {"0"}, "interval_minutes": {"5"},
	}, session); code != http.StatusForbidden {
		t.Fatalf("POST without a token = %d", code)
	}
}

// TestWebArtificialDiaryShowsWhatWasDecided proves an administrator can follow
// what an artificial player tried and why.
func TestWebArtificialDiaryShowsWhatWasDecided(t *testing.T) {
	ctx := context.Background()
	database, universeWorld := artificialWeb(t)
	admin := appauth.Principal{AccountID: 1, Username: "player1", Roles: []appauth.Role{appauth.RoleAdmin}}
	if _, err := universeWorld.AI.Create(ctx, admin, appai.Request{
		Name: "Kepler", Archetype: domainai.CautiousMiner,
		Window: domainai.Window{Start: 0, End: 0}, Interval: 5 * time.Minute,
	}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	setResources(t, ctx, database, 3, 5000, 5000, 5000)
	think(t, ctx, universeWorld)

	handler := artificialHandler(t, universeWorld, admin)
	page := getPage(t, handler, "/admin/ai/3",
		&http.Cookie{Name: "uaw_session", Value: "session"},
		&http.Cookie{Name: "uaw_csrf", Value: "csrf-token"})
	if !strings.Contains(page, "build metal_mine") && !strings.Contains(page, "build ") {
		t.Fatalf("the diary shows no construction: %q", page)
	}
	if !strings.Contains(page, "strategic") {
		t.Fatalf("the diary shows no layer: %q", page)
	}
}

func artificialWeb(t *testing.T) (*storagesqlite.Database, *world) {
	t.Helper()
	ctx := context.Background()
	database := economyDatabase(t, ctx, 2)
	universeWorld := newWorld(t, database, appclock.NewFake(time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC)))
	for index := 1; index <= 2; index++ {
		if _, err := universeWorld.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: int64(index)},
			playerDisplayName(index)); err != nil {
			t.Fatalf("CreateEmpire() error = %v", err)
		}
	}
	return database, universeWorld
}

func artificialHandler(t *testing.T, universeWorld *world, principal appauth.Principal) http.Handler {
	t.Helper()
	handler, err := webhandler.New(webhandler.Dependencies{
		Authentication: webAuthenticationStub{principal: principal},
		ServerState:    runningStateStub{}, CSRFSecrets: sequenceSecret{value: "csrf-token"},
		Economy: universeWorld.Economy, Artificials: universeWorld.AI,
	})
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

// TestWebArtificialDebugShowsTheAllianceAndItsProvenance proves an
// administrator can see what a team of machines is doing and where each of its
// beliefs came from, and that no player can.
func TestWebArtificialDebugShowsTheAllianceAndItsProvenance(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, _, players := alliedArtificials(t)
	scout := players[0]
	setUnits(t, ctx, database, scout.bodyID, "espionage_probe", 6)
	setResearch(t, ctx, database, scout.playerID, "espionage_technology", 3)
	setResearch(t, ctx, database, scout.playerID, "computer_technology", 3)
	setResources(t, ctx, database, scout.bodyID, 200000, 200000, 200000)
	setResources(t, ctx, database, 1, 90000, 60000, 20000)

	think(t, ctx, universeWorld)
	flyEverything(t, ctx, universeWorld)
	universeWorld.Clock.Advance(time.Minute)
	think(t, ctx, universeWorld)

	admin := appauth.Principal{AccountID: 1, Username: "player1", Roles: []appauth.Role{appauth.RoleAdmin}}
	handler := artificialHandler(t, universeWorld, admin)
	session := &http.Cookie{Name: "uaw_session", Value: "session"}
	csrfCookie := &http.Cookie{Name: "uaw_csrf", Value: "csrf-token"}

	page := getPage(t, handler, "/admin/ai/2", session, csrfCookie)
	for _, expected := range []string{
		"Alliance Les Machines", "Mémoire commune et provenance", "Eclaireur", "vue omnisciente", "raid sur",
	} {
		if !strings.Contains(page, expected) {
			t.Fatalf("the debug view misses %q", expected)
		}
	}

	// A player sees none of it.
	player := artificialHandler(t, universeWorld, appauth.Principal{
		AccountID: 1, Username: "player1", Roles: []appauth.Role{appauth.RolePlayer},
	})
	if code := statusOf(t, player, "/admin/ai/2", session, csrfCookie); code != http.StatusNotFound {
		t.Fatalf("GET the debug view as a player = %d", code)
	}
	// And the plans of the machines are journalled with their alliance.
	assertSingleValue(t, database,
		"SELECT COUNT(*) > 0 FROM game_event_log WHERE event_type = 'ai_objective_opened' AND entity_type = 'alliance'", 1)
}

// TestWebAdministratorAssignsAnAlliance proves an administrator can put a
// machine into a team from the page, and that the ordinary alliance rules still
// decide whether it gets in.
func TestWebAdministratorAssignsAnAlliance(t *testing.T) {
	ctx := context.Background()
	database, universeWorld := artificialWeb(t)
	admin := appauth.Principal{AccountID: 1, Username: "player1", Roles: []appauth.Role{appauth.RoleAdmin}}
	handler := artificialHandler(t, universeWorld, admin)
	session := &http.Cookie{Name: "uaw_session", Value: "session"}
	csrfCookie := &http.Cookie{Name: "uaw_csrf", Value: "csrf-token"}

	for _, name := range []string{"Kepler", "Galilee"} {
		if _, err := universeWorld.AI.Create(ctx, admin, appai.Request{
			Name: name, Archetype: domainai.CautiousMiner,
			Window: domainai.Window{Start: 0, End: 0}, Interval: 5 * time.Minute,
		}); err != nil {
			t.Fatalf("Create(%s) error = %v", name, err)
		}
	}
	page := getPage(t, handler, "/admin/ai", session, csrfCookie)
	if !strings.Contains(page, "Affecter") {
		t.Fatalf("the page offers no alliance: %q", page)
	}
	for _, expected := range []string{`class="admin-table-scroll"`, `class="admin-ai-actions"`,
		`class="admin-ai-alliance-form"`} {
		if !strings.Contains(page, expected) {
			t.Fatalf("the artificial-player actions miss the aligned layout %q: %q", expected, page)
		}
	}
	styles := fetch(handler, "/static/css/pages.css")
	if styles.Code != http.StatusOK || !strings.Contains(styles.Body.String(), ".admin-ai-actions {") ||
		!strings.Contains(styles.Body.String(), "align-items: flex-end;") ||
		!strings.Contains(styles.Body.String(), "grid-template-columns: minmax(7rem, 1fr) minmax(5rem, .7fr) auto;") ||
		!strings.Contains(styles.Body.String(), ".admin-ai-alliance-form input { width: 100%; min-width: 0; }") {
		t.Fatalf("the artificial-player action row is not aligned: %q", styles.Body.String())
	}
	// The first founds the alliance, the second is invited into it.
	postForm(t, handler, "/admin/ai/3/alliance", url.Values{
		"csrf_token": {"csrf-token"}, "alliance": {"Les Machines"}, "tag": {"mch"},
	}, http.StatusSeeOther, session, csrfCookie)
	postForm(t, handler, "/admin/ai/4/alliance", url.Values{
		"csrf_token": {"csrf-token"}, "alliance": {"Les Machines"}, "tag": {"mch"},
	}, http.StatusSeeOther, session, csrfCookie)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM alliance_members", 2)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM alliances", 1)

	listed := getPage(t, handler, "/admin/ai", session, csrfCookie)
	if !strings.Contains(listed, "MCH") {
		t.Fatalf("the page does not show the team: %q", listed)
	}
	// A machine already in a team is not offered a second one.
	if strings.Contains(listed, `name="alliance"`) {
		t.Fatal("a member was offered another alliance")
	}
	body := postForm(t, handler, "/admin/ai/3/alliance", url.Values{
		"csrf_token": {"csrf-token"}, "alliance": {"Les Autres"}, "tag": {"aut"},
	}, http.StatusBadRequest, session, csrfCookie)
	if !strings.Contains(body, "appartient déjà") {
		t.Fatalf("a second alliance was accepted: %q", body)
	}
}
