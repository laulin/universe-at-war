package tests

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appclock "universeatwar/internal/clock"
	webhandler "universeatwar/internal/web"
)

func TestGamePagesShareLayoutWithoutInlineScripts(t *testing.T) {
	handler, session, csrfCookie := playableHandler(t)

	for _, target := range []string{"/", "/planets/1"} {
		body := getPage(t, handler, target, session, csrfCookie)
		if !strings.Contains(body, `<nav aria-label="Navigation principale">`) {
			t.Fatalf("%s has no shared navigation: %q", target, body)
		}
		if !strings.Contains(body, `href="/planets/1"`) {
			t.Fatalf("%s has no link to the current planet: %q", target, body)
		}
		if !strings.Contains(body, `aria-current="page"`) {
			t.Fatalf("%s does not mark the current section: %q", target, body)
		}
		if strings.Contains(body, "<script>") {
			t.Fatalf("%s embeds an inline script: %q", target, body)
		}
		if !strings.Contains(body, `<script src="/static/app.js" defer></script>`) {
			t.Fatalf("%s does not load the shared script: %q", target, body)
		}
		if !strings.Contains(body, "Déconnexion") {
			t.Fatalf("%s has no logout control: %q", target, body)
		}
	}

	assets := httptest.NewRecorder()
	handler.ServeHTTP(assets, httptest.NewRequest(http.MethodGet, "/static/app.js", nil))
	if assets.Code != http.StatusOK {
		t.Fatalf("GET /static/app.js = %d", assets.Code)
	}
	if contentType := assets.Header().Get("Content-Type"); !strings.Contains(contentType, "javascript") {
		t.Fatalf("app.js content type = %q", contentType)
	}
}

func TestAuthenticationPagesKeepTheirOwnShellWithoutNavigation(t *testing.T) {
	handler, err := webhandler.New(webhandler.Dependencies{
		Authentication: webAuthenticationStub{},
		ServerState:    runningStateStub{},
		CSRFSecrets:    sequenceSecret{value: "csrf-token"},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := getPage(t, handler, "/login")
	if strings.Contains(body, `<nav aria-label="Navigation principale">`) {
		t.Fatalf("login page shows the game navigation: %q", body)
	}
	if !strings.Contains(body, "Connexion") {
		t.Fatalf("login page = %q", body)
	}
}

func playableHandler(t *testing.T) (http.Handler, *http.Cookie, *http.Cookie) {
	t.Helper()
	ctx := context.Background()
	database := economyDatabase(t, ctx, 1)
	universe := newWorld(t, database, appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC)))
	if _, err := universe.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: 1}, "Captain"); err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	handler, err := webhandler.New(webhandler.Dependencies{
		Authentication: webAuthenticationStub{principal: appauth.Principal{AccountID: 1, Username: "player1"}},
		ServerState:    runningStateStub{},
		CSRFSecrets:    sequenceSecret{value: "csrf-token"},
		Economy:        universe.Economy,
	})
	if err != nil {
		t.Fatal(err)
	}
	return handler, &http.Cookie{Name: "uaw_session", Value: "session"}, &http.Cookie{Name: "uaw_csrf", Value: "csrf-token"}
}

// The shell is three columns the player never loses: the navigation, the
// resources of the body being looked at, and the bodies of the account.
func TestGameShellKeepsItsThreeRegions(t *testing.T) {
	handler, session, csrfCookie := playableHandler(t)

	for _, target := range []string{"/", "/planets/1"} {
		body := getPage(t, handler, target, session, csrfCookie)
		for _, region := range []string{
			`class="resource-bar"`, `class="body-column"`, `class="game-shell"`,
			`<meter`, `/art/resource/metal`, `/art/body/planet-`,
		} {
			if !strings.Contains(body, region) {
				t.Fatalf("%s has no %s: %q", target, region, body)
			}
		}
		// Filling is an attribute because the policy refuses inline styles.
		if strings.Contains(body, "style=") {
			t.Fatalf("%s carries an inline style the policy would block: %q", target, body)
		}
	}
}

// A screen with no planet of its own follows the body the player last visited,
// rather than falling back on the first one of the account.
func TestShellFollowsTheBodyThePlayerLastVisited(t *testing.T) {
	handler, _, _, bodies, session, csrfCookie := expansionHandler(t)

	first := getPage(t, handler, "/", session, csrfCookie)
	if !strings.Contains(first, fmt.Sprintf(`href="/planets/%d" aria-current="true"`, bodies.AliceHome)) {
		t.Fatalf("without a remembered body the shell does not fall back on the first one: %q", first)
	}
	remembered := getPage(t, handler, "/", session, csrfCookie, &http.Cookie{Name: "uaw_body", Value: strconv.FormatInt(bodies.Moon, 10)})
	if !strings.Contains(remembered, fmt.Sprintf(`href="/planets/%d" aria-current="true"`, bodies.Moon)) {
		t.Fatalf("the shell ignored the remembered body: %q", remembered)
	}
	if strings.Contains(remembered, fmt.Sprintf(`href="/planets/%d" aria-current="true"`, bodies.AliceHome)) {
		t.Fatalf("the shell marks two bodies as current: %q", remembered)
	}
}

// Visiting a body is what remembers it.
func TestVisitingABodyRemembersIt(t *testing.T) {
	handler, _, _, bodies, session, csrfCookie := expansionHandler(t)
	request := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/planets/%d", bodies.Moon), nil)
	request.AddCookie(session)
	request.AddCookie(csrfCookie)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == "uaw_body" && cookie.Value == strconv.FormatInt(bodies.Moon, 10) {
			return
		}
	}
	t.Fatalf("visiting a body did not remember it: %v", recorder.Result().Cookies())
}
