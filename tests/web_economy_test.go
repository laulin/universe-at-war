package tests

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appclock "universeatwar/internal/clock"
	"universeatwar/internal/domain/server"
	webhandler "universeatwar/internal/web"
)

type webAuthenticationStub struct {
	principal appauth.Principal
}

func (s webAuthenticationStub) Login(context.Context, string, string) (appauth.LoginResult, error) {
	return appauth.LoginResult{}, errors.New("not implemented")
}
func (s webAuthenticationStub) Resolve(_ context.Context, token string) (appauth.Principal, error) {
	if token != "session" {
		return appauth.Principal{}, appauth.ErrInvalidSession
	}
	return s.principal, nil
}
func (s webAuthenticationStub) ChangePassword(context.Context, string, string, string) (appauth.LoginResult, error) {
	return appauth.LoginResult{}, errors.New("not implemented")
}
func (s webAuthenticationStub) Logout(context.Context, string) error { return nil }

type runningStateStub struct{}

func (runningStateStub) Current(context.Context) (server.State, error) { return server.Running, nil }

type sequenceSecret struct{ value string }

func (s sequenceSecret) Generate() (string, error) { return s.value, nil }

func TestWebEconomyFlowIsPlayableAndCSRFProtected(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC)
	database := economyDatabase(t, ctx, 1)
	economy := newWorld(t, database, appclock.NewFake(now)).Economy
	handler, err := webhandler.New(webhandler.Dependencies{
		Authentication: webAuthenticationStub{principal: appauth.Principal{AccountID: 1, Username: "player1"}},
		ServerState:    runningStateStub{}, CSRFSecrets: sequenceSecret{value: "csrf-token"}, Economy: economy,
	})
	if err != nil {
		t.Fatal(err)
	}
	session := &http.Cookie{Name: "uaw_session", Value: "session"}

	landing := httptest.NewRecorder()
	landingRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	landingRequest.AddCookie(session)
	handler.ServeHTTP(landing, landingRequest)
	if landing.Code != http.StatusOK || !strings.Contains(landing.Body.String(), "Fonder votre empire") {
		t.Fatalf("initial GET / = %d %q", landing.Code, landing.Body.String())
	}
	csrfCookie := responseCookie(t, landing.Result(), "uaw_csrf")

	withoutCSRF := postFormRequest("/empire", url.Values{"name": {"Orion"}})
	withoutCSRF.AddCookie(session)
	denied := httptest.NewRecorder()
	handler.ServeHTTP(denied, withoutCSRF)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("POST /empire without CSRF = %d", denied.Code)
	}

	create := postFormRequest("/empire", url.Values{"csrf_token": {"csrf-token"}, "name": {"Orion"}})
	create.AddCookie(session)
	create.AddCookie(csrfCookie)
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, create)
	if created.Code != http.StatusSeeOther || created.Header().Get("Location") != "/" {
		t.Fatalf("POST /empire = %d %q", created.Code, created.Body.String())
	}

	overview := getPage(t, handler, "/", session, csrfCookie)
	if !strings.Contains(overview, "Planète mère") || !strings.Contains(overview, `href="/planets/1"`) {
		t.Fatalf("empire overview = %q", overview)
	}

	planetPage := getPage(t, handler, "/planets/1", session, csrfCookie)
	if !strings.Contains(planetPage, "Mine de métal") || !strings.Contains(planetPage, "500") {
		t.Fatalf("planet page = %q", planetPage)
	}
	idempotencyKey := hiddenValue(t, planetPage, "idempotency_key")
	build := postFormRequest("/planets/1/buildings/metal_mine", url.Values{"csrf_token": {"csrf-token"}, "idempotency_key": {idempotencyKey}})
	build.AddCookie(session)
	build.AddCookie(csrfCookie)
	started := httptest.NewRecorder()
	handler.ServeHTTP(started, build)
	if started.Code != http.StatusSeeOther || started.Header().Get("Location") != "/planets/1" {
		t.Fatalf("POST building = %d %q", started.Code, started.Body.String())
	}

	queued := getPage(t, handler, "/planets/1", session, csrfCookie)
	if !strings.Contains(queued, "Construction en cours") || !strings.Contains(queued, "440") {
		t.Fatalf("queued planet page = %q", queued)
	}
}

func TestWebPlanetOfAnotherAccountIsNotFound(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC)
	database := economyDatabase(t, ctx, 2)
	universe := newWorld(t, database, appclock.NewFake(now))
	if _, err := universe.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: 1}, "Alpha"); err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	if _, err := universe.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: 2}, "Beta"); err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	handler, err := webhandler.New(webhandler.Dependencies{
		Authentication: webAuthenticationStub{principal: appauth.Principal{AccountID: 2, Username: "player2"}},
		ServerState:    runningStateStub{}, CSRFSecrets: sequenceSecret{value: "csrf-token"}, Economy: universe.Economy,
	})
	if err != nil {
		t.Fatal(err)
	}
	session := &http.Cookie{Name: "uaw_session", Value: "session"}
	csrfCookie := &http.Cookie{Name: "uaw_csrf", Value: "csrf-token"}

	request := httptest.NewRequest(http.MethodGet, "/planets/1", nil)
	request.AddCookie(session)
	request.AddCookie(csrfCookie)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("GET foreign planet = %d, want 404", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "Alpha") {
		t.Fatalf("foreign planet response leaked its owner: %q", recorder.Body.String())
	}

	build := postFormRequest("/planets/1/buildings/metal_mine", url.Values{"csrf_token": {"csrf-token"}, "idempotency_key": {"key"}})
	build.AddCookie(session)
	build.AddCookie(csrfCookie)
	denied := httptest.NewRecorder()
	handler.ServeHTTP(denied, build)
	if denied.Code != http.StatusNotFound {
		t.Fatalf("POST on foreign planet = %d, want 404", denied.Code)
	}
}

func TestWebPlanetTitleCanRenameOnlyItsOwnPlanet(t *testing.T) {
	handler, database, session, csrfCookie := fleetHandler(t)

	page := getPage(t, handler, "/planets/1", session, csrfCookie)
	for _, fragment := range []string{
		`data-body-rename`,
		`action="/planets/1/name"`,
		`data-body-rename-trigger`,
		`value="Planète mère"`,
		`maxlength="32"`,
	} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("rename control misses %q: %q", fragment, page)
		}
	}

	withoutCSRF := postFormRequest("/planets/1/name", url.Values{"name": {"Nouvelle Terre"}})
	withoutCSRF.AddCookie(session)
	denied := httptest.NewRecorder()
	handler.ServeHTTP(denied, withoutCSRF)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("rename without CSRF = %d, want 403", denied.Code)
	}

	rename := postFormRequest("/planets/1/name", url.Values{
		"csrf_token": {"csrf-token"}, "name": {"  Nouvelle Terre  "},
	})
	rename.AddCookie(session)
	rename.AddCookie(csrfCookie)
	renamed := httptest.NewRecorder()
	handler.ServeHTTP(renamed, rename)
	if renamed.Code != http.StatusSeeOther || renamed.Header().Get("Location") != "/planets/1" {
		t.Fatalf("rename = %d %q", renamed.Code, renamed.Body.String())
	}
	if refreshed := getPage(t, handler, "/planets/1", session, csrfCookie); !strings.Contains(refreshed, `value="Nouvelle Terre"`) {
		t.Fatalf("renamed title is absent: %q", refreshed)
	}

	invalid := postFormRequest("/planets/1/name", url.Values{
		"csrf_token": {"csrf-token"}, "name": {"   "},
	})
	invalid.AddCookie(session)
	invalid.AddCookie(csrfCookie)
	refused := httptest.NewRecorder()
	handler.ServeHTTP(refused, invalid)
	if refused.Code != http.StatusBadRequest || !strings.Contains(refused.Body.String(), "entre 1 et 32 caractères") {
		t.Fatalf("invalid rename = %d %q", refused.Code, refused.Body.String())
	}

	foreign := postFormRequest("/planets/2/name", url.Values{
		"csrf_token": {"csrf-token"}, "name": {"Volée"},
	})
	foreign.AddCookie(session)
	foreign.AddCookie(csrfCookie)
	foreignResponse := httptest.NewRecorder()
	handler.ServeHTTP(foreignResponse, foreign)
	if foreignResponse.Code != http.StatusNotFound {
		t.Fatalf("foreign rename = %d, want 404", foreignResponse.Code)
	}
	assertSingleText(t, database, "SELECT name FROM planets WHERE id = 2", "Planète mère")
}

func TestPlanetRenameButtonsDoNotLoseTheEditedNameOnFocusChange(t *testing.T) {
	handler, _, _, _ := fleetHandler(t)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/static/app.js", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET /static/app.js = %d", response.Code)
	}
	script := response.Body.String()
	for _, expected := range []string{
		`form.addEventListener("focusout", (event)`,
		`event.relatedTarget && !form.contains(event.relatedTarget)`,
		`document.addEventListener("pointerdown", (event)`,
		`!rename.contains(event.target)`,
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("rename focus handling misses %q", expected)
		}
	}
}

func getPage(t *testing.T, handler http.Handler, target string, cookies ...*http.Cookie) string {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET %s = %d %q", target, recorder.Code, recorder.Body.String())
	}
	return recorder.Body.String()
}
