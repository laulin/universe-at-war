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
