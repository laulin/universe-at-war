package tests

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appclock "universeatwar/internal/clock"
	webhandler "universeatwar/internal/web"
)

// TestPlayerRoutesNeverLeakAnotherEmpire sweeps every page a player can reach
// and checks that no value belonging to somebody else appears in the response.
func TestPlayerRoutesNeverLeakAnotherEmpire(t *testing.T) {
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC))
	database := economyDatabase(t, ctx, 2)
	universeWorld := newWorld(t, database, clock)
	if _, err := universeWorld.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: 1}, "Alice"); err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	if _, err := universeWorld.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: 2}, "Bob"); err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}

	// Sentinels only Bob owns. None of them may ever reach Alice's browser.
	const (
		secretStock    = 987654321
		secretFighters = 876543210
		secretDefenses = 765432100
		secretResearch = 654321
	)
	setResources(t, ctx, database, 2, secretStock, secretStock, secretStock)
	setUnits(t, ctx, database, 2, "light_fighter", secretFighters)
	setUnits(t, ctx, database, 2, "rocket_launcher", secretDefenses)
	setResearch(t, ctx, database, 2, "energy_technology", secretResearch)
	setBuilding(t, ctx, database, 2, "metal_mine", 42)

	assertRoutesHideSecrets(t, universeWorld, appauth.Principal{AccountID: 1, Username: "player1"},
		[]string{
			"/", "/planets/1", "/planets/1/research", "/planets/1/shipyard", "/planets/1/defense",
			"/planets/1/fleet", "/planets/1/fleet/send", "/galaxy/1/1", "/reports",
			"/planets/2", "/planets/2/research", "/planets/2/shipyard", "/planets/2/defense",
			"/planets/2/fleet", "/planets/2/phalanx", "/planets/2/jump",
		},
		[]string{
			fmt.Sprint(secretStock), fmt.Sprint(secretFighters),
			fmt.Sprint(secretDefenses), fmt.Sprint(secretResearch),
		})
}

// pathFor builds a route for one body.
func pathFor(pattern string, bodyID int64) string {
	return fmt.Sprintf(pattern, bodyID)
}

// assertRoutesHideSecrets sweeps player routes and fails as soon as a value
// belonging to somebody else appears in a response.
func assertRoutesHideSecrets(t *testing.T, universeWorld *world, principal appauth.Principal, routes, secrets []string) {
	t.Helper()
	handler, err := webhandler.New(webhandler.Dependencies{
		Authentication: webAuthenticationStub{principal: principal},
		ServerState:    runningStateStub{},
		CSRFSecrets:    sequenceSecret{value: "csrf-token"},
		Economy:        universeWorld.Economy,
		Research:       universeWorld.Research,
		Shipyard:       universeWorld.Shipyard,
		Fleet:          universeWorld.Fleet,
		Galaxy:         universeWorld.Galaxy,
		Reports:        universeWorld.Reports,
		Phalanx:        universeWorld.Phalanx,
		JumpGate:       universeWorld.JumpGate,
	})
	if err != nil {
		t.Fatal(err)
	}
	session := &http.Cookie{Name: "uaw_session", Value: "session"}
	csrfCookie := &http.Cookie{Name: "uaw_csrf", Value: "csrf-token"}
	for _, route := range routes {
		t.Run(route, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, route, nil)
			request.AddCookie(session)
			request.AddCookie(csrfCookie)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusOK && recorder.Code != http.StatusNotFound {
				t.Fatalf("GET %s = %d", route, recorder.Code)
			}
			body := recorder.Body.String()
			for _, secret := range secrets {
				if strings.Contains(body, secret) {
					t.Fatalf("GET %s leaked %q", route, secret)
				}
			}
		})
	}
}
