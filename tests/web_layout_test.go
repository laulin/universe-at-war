package tests

import (
	"context"
	"net/http"
	"net/http/httptest"
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
