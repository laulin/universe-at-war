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

	overviewRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	overviewRequest.AddCookie(session)
	overviewRequest.AddCookie(csrfCookie)
	overview := httptest.NewRecorder()
	handler.ServeHTTP(overview, overviewRequest)
	if overview.Code != http.StatusOK || !strings.Contains(overview.Body.String(), "Mine de métal") || !strings.Contains(overview.Body.String(), "500") {
		t.Fatalf("economic GET / = %d %q", overview.Code, overview.Body.String())
	}
	idempotencyKey := hiddenValue(t, overview.Body.String(), "idempotency_key")
	build := postFormRequest("/planets/1/buildings/metal_mine", url.Values{"csrf_token": {"csrf-token"}, "idempotency_key": {idempotencyKey}})
	build.AddCookie(session)
	build.AddCookie(csrfCookie)
	started := httptest.NewRecorder()
	handler.ServeHTTP(started, build)
	if started.Code != http.StatusSeeOther || started.Header().Get("Location") != "/" {
		t.Fatalf("POST building = %d %q", started.Code, started.Body.String())
	}

	queuedRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	queuedRequest.AddCookie(session)
	queuedRequest.AddCookie(csrfCookie)
	queued := httptest.NewRecorder()
	handler.ServeHTTP(queued, queuedRequest)
	if queued.Code != http.StatusOK || !strings.Contains(queued.Body.String(), "Construction en cours") || !strings.Contains(queued.Body.String(), "440") {
		t.Fatalf("queued GET / = %d %q", queued.Code, queued.Body.String())
	}
}
