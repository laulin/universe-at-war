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

	appauth "universeatwar/internal/app/authentication"
	appregistration "universeatwar/internal/app/registration"
	appserverstate "universeatwar/internal/app/serverstate"
	appsetup "universeatwar/internal/app/setup"
	"universeatwar/internal/auth"
	appclock "universeatwar/internal/clock"
	storagesqlite "universeatwar/internal/storage/sqlite"
	webhandler "universeatwar/internal/web"
)

// TestAPlayerSigningInLandsInTheGameNotInTheSetup proves an ordinary player is
// taken to their empire after signing in. The configuration of the universe
// belongs to an administrator, and a player who is sent there is only shown a
// refusal they can do nothing about.
func TestAPlayerSigningInLandsInTheGameNotInTheSetup(t *testing.T) {
	handler, database, _ := playableUniverse(t)

	// A player signs up the ordinary way and signs in.
	postForm(t, handler, "/register", url.Values{
		"csrf_token": {"csrf-token"}, "username": {"newcomer"},
		"password": {"a-strong-password"}, "password_confirmation": {"a-strong-password"},
	}, http.StatusSeeOther, csrfOf())
	assertSingleValue(t, database, "SELECT COUNT(*) FROM accounts WHERE username = 'newcomer'", 1)

	login := postFormRequest("/login", url.Values{
		"csrf_token": {"csrf-token"}, "username": {"newcomer"}, "password": {"a-strong-password"},
	})
	login.AddCookie(csrfOf())
	signedIn := httptest.NewRecorder()
	handler.ServeHTTP(signedIn, login)
	if signedIn.Code != http.StatusSeeOther {
		t.Fatalf("POST /login = %d %q", signedIn.Code, signedIn.Body.String())
	}
	if location := signedIn.Header().Get("Location"); location != "/" {
		t.Fatalf("a player signing in lands on %q, want the game itself", location)
	}

	// And following that redirect really shows them the game.
	session := responseCookie(t, signedIn.Result(), "uaw_session")
	home := httptest.NewRequest(http.MethodGet, "/", nil)
	home.AddCookie(session)
	home.AddCookie(csrfOf())
	landing := httptest.NewRecorder()
	handler.ServeHTTP(landing, home)
	if landing.Code != http.StatusOK {
		t.Fatalf("GET / after signing in = %d %q", landing.Code, landing.Body.String())
	}
	if !strings.Contains(landing.Body.String(), `action="/empire"`) {
		t.Fatalf("the landing page does not offer to found an empire: %q", landing.Body.String())
	}

	// The configuration of the universe stays out of reach, and says so as a
	// missing page rather than as a refusal a player cannot act on.
	setup := httptest.NewRequest(http.MethodGet, "/setup/1", nil)
	setup.AddCookie(session)
	setup.AddCookie(csrfOf())
	refused := httptest.NewRecorder()
	handler.ServeHTTP(refused, setup)
	if refused.Code != http.StatusNotFound {
		t.Fatalf("GET /setup/1 as a player = %d %q", refused.Code, refused.Body.String())
	}
}

// TestAPlayerIsToldTheUniverseIsClosedRatherThanRefused covers the other half:
// while the universe is not open, a player is told so plainly instead of being
// bounced into an administrator's page.
func TestAPlayerIsToldTheUniverseIsClosedRatherThanRefused(t *testing.T) {
	ctx := context.Background()
	handler, database, _ := playableUniverse(t)
	postForm(t, handler, "/register", url.Values{
		"csrf_token": {"csrf-token"}, "username": {"newcomer"},
		"password": {"a-strong-password"}, "password_confirmation": {"a-strong-password"},
	}, http.StatusSeeOther, csrfOf())
	login := postFormRequest("/login", url.Values{
		"csrf_token": {"csrf-token"}, "username": {"newcomer"}, "password": {"a-strong-password"},
	})
	login.AddCookie(csrfOf())
	signedIn := httptest.NewRecorder()
	handler.ServeHTTP(signedIn, login)
	session := responseCookie(t, signedIn.Result(), "uaw_session")

	if _, err := database.Write().ExecContext(ctx,
		"UPDATE server_state SET state = 'PAUSED' WHERE id = 1"); err != nil {
		t.Fatal(err)
	}
	home := httptest.NewRequest(http.MethodGet, "/", nil)
	home.AddCookie(session)
	home.AddCookie(csrfOf())
	closed := httptest.NewRecorder()
	handler.ServeHTTP(closed, home)
	if closed.Code != http.StatusOK {
		t.Fatalf("GET / while paused = %d %q", closed.Code, closed.Body.String())
	}
	if !strings.Contains(closed.Body.String(), "L'univers n'est pas ouvert") {
		t.Fatalf("a paused universe does not tell the player why: %q", closed.Body.String())
	}
	if strings.Contains(closed.Body.String(), "setup") {
		t.Fatalf("a player is still pointed at the setup: %q", closed.Body.String())
	}
}

// playableUniverse builds a running universe that accepts sign-ups, wired the
// way the serve command wires it.
func playableUniverse(t *testing.T) (http.Handler, *storagesqlite.Database, *world) {
	t.Helper()
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC))
	database := economyDatabase(t, ctx, 1)
	universeWorld := newWorld(t, database, clock)
	if _, err := database.Write().ExecContext(ctx,
		"INSERT INTO server_state(id, state, updated_at) VALUES (1, 'RUNNING', '2042-09-10T12:00:00Z')"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Write().ExecContext(ctx,
		`UPDATE ruleset_versions SET document = json_set(document, '$.identity.registration_policy', 'open')`); err != nil {
		t.Fatal(err)
	}
	parameters := auth.Parameters{MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	passwords := auth.NewPasswordHasher(parameters, rand.Reader)
	authentication := appauth.Service{
		Clock:       clock,
		Passwords:   passwords,
		Tokens:      auth.NewSecretGenerator(rand.Reader, 32),
		Repository:  storagesqlite.NewAuthenticationRepository(database.Read(), database.Write()),
		SessionLife: 12 * time.Hour,
	}
	handler, err := webhandler.New(webhandler.Dependencies{
		Authentication: authentication,
		ServerState:    appserverstate.Service{Repository: storagesqlite.NewServerStateRepository(database.Read(), database.Write())},
		CSRFSecrets:    sequenceSecret{value: "csrf-token"},
		Setup: appsetup.Service{
			Clock: clock, Repository: storagesqlite.NewSetupRepository(database.Write()),
		},
		Registration: appregistration.Service{
			Clock: clock, Passwords: passwords,
			Repository: storagesqlite.NewRegistrationRepository(database.Read(), database.Write()),
		},
		Economy: universeWorld.Economy,
	})
	if err != nil {
		t.Fatal(err)
	}
	return handler, database, universeWorld
}

func csrfOf() *http.Cookie {
	return &http.Cookie{Name: "uaw_csrf", Value: "csrf-token"}
}
