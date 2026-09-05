package tests

import (
	"context"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	appserverstate "universeatwar/internal/app/serverstate"
	appsetup "universeatwar/internal/app/setup"
	"universeatwar/internal/auth"
	appclock "universeatwar/internal/clock"
	"universeatwar/internal/domain/server"
	storagesqlite "universeatwar/internal/storage/sqlite"
	webhandler "universeatwar/internal/web"
)

func TestWebSetupStartsAndActivatesUniverse(t *testing.T) {
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.October, 11, 12, 13, 14, 0, time.UTC))
	database, authentication, initialPassword := authenticatedService(t, ctx, clock, time.Hour)
	login, err := authentication.Login(ctx, "admin", initialPassword)
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	changed, err := authentication.ChangePassword(ctx, login.Token, initialPassword, "a-new-strong-password")
	if err != nil {
		t.Fatalf("ChangePassword() error = %v", err)
	}
	principal, err := authentication.Resolve(ctx, changed.Token)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	setup := appsetup.Service{Clock: clock, Repository: storagesqlite.NewSetupRepository(database.Write())}
	states := appserverstate.Service{Repository: storagesqlite.NewServerStateRepository(database.Read(), database.Write())}
	handler, err := webhandler.New(webhandler.Dependencies{
		Authentication: authentication,
		ServerState:    states,
		CSRFSecrets:    auth.NewSecretGenerator(rand.Reader, 32),
		Setup:          setup,
	})
	if err != nil {
		t.Fatalf("web New() error = %v", err)
	}
	sessionCookie := &http.Cookie{Name: "uaw_session", Value: changed.Token}

	stepOne := httptest.NewRecorder()
	stepOneRequest := httptest.NewRequest(http.MethodGet, "/setup/1", nil)
	stepOneRequest.AddCookie(sessionCookie)
	handler.ServeHTTP(stepOne, stepOneRequest)
	if stepOne.Code != http.StatusOK || !strings.Contains(stepOne.Body.String(), "Étape 1 sur 10") {
		t.Fatalf("GET /setup/1 = %d %q", stepOne.Code, stepOne.Body.String())
	}
	csrfCookie := responseCookie(t, stepOne.Result(), "uaw_csrf")
	csrfToken := hiddenValue(t, stepOne.Body.String(), "csrf_token")
	version := hiddenValue(t, stepOne.Body.String(), "version")

	saveIdentity := postFormRequest("/setup/1", url.Values{
		"csrf_token": {csrfToken}, "version": {version},
		"name": {"Campagne HTTP"}, "language": {"fr"}, "timezone": {"Europe/Paris"},
		"description": {"Test du wizard"}, "network_visibility": {"local"},
		"registration_policy": {"closed"},
	})
	saveIdentity.AddCookie(sessionCookie)
	saveIdentity.AddCookie(csrfCookie)
	saved := httptest.NewRecorder()
	handler.ServeHTTP(saved, saveIdentity)
	if saved.Code != http.StatusSeeOther || saved.Header().Get("Location") != "/setup/2" {
		t.Fatalf("POST /setup/1 = %d %q", saved.Code, saved.Header().Get("Location"))
	}

	draft, err := setup.Load(ctx, principal)
	if err != nil {
		t.Fatalf("Load() after step 1 error = %v", err)
	}
	for step := 2; step <= 9; step++ {
		draft, err = setup.Save(ctx, principal, step, draft.Version, draft.Rules)
		if err != nil {
			t.Fatalf("Save(step %d) error = %v", step, err)
		}
	}

	confirmation := httptest.NewRecorder()
	confirmationRequest := httptest.NewRequest(http.MethodGet, "/setup/10", nil)
	confirmationRequest.AddCookie(sessionCookie)
	confirmationRequest.AddCookie(csrfCookie)
	handler.ServeHTTP(confirmation, confirmationRequest)
	if confirmation.Code != http.StatusOK || !strings.Contains(confirmation.Body.String(), "Campagne HTTP") {
		t.Fatalf("GET /setup/10 = %d %q", confirmation.Code, confirmation.Body.String())
	}
	confirmVersion := hiddenValue(t, confirmation.Body.String(), "version")
	confirmToken := hiddenValue(t, confirmation.Body.String(), "csrf_token")
	activate := postFormRequest("/setup/10", url.Values{
		"csrf_token": {confirmToken}, "version": {confirmVersion}, "confirm": {"yes"},
	})
	activate.AddCookie(sessionCookie)
	activate.AddCookie(csrfCookie)
	activated := httptest.NewRecorder()
	handler.ServeHTTP(activated, activate)
	if activated.Code != http.StatusSeeOther || activated.Header().Get("Location") != "/" {
		t.Fatalf("POST /setup/10 = %d %q body %q", activated.Code, activated.Header().Get("Location"), activated.Body.String())
	}
	assertServerState(t, database, server.Running)
}
