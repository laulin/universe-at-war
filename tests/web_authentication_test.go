package tests

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appbootstrap "universeatwar/internal/app/bootstrap"
	appserverstate "universeatwar/internal/app/serverstate"
	"universeatwar/internal/auth"
	appclock "universeatwar/internal/clock"
	storagesqlite "universeatwar/internal/storage/sqlite"
	webhandler "universeatwar/internal/web"
)

func TestWebBootstrapAuthenticationFlow(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2042, time.July, 8, 9, 10, 11, 0, time.UTC)
	clock := appclock.NewFake(now)
	database, err := storagesqlite.Open(ctx, filepath.Join(t.TempDir(), "universe.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	passwords := auth.NewPasswordHasher(auth.Parameters{
		MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	}, bytes.NewReader(bytes.Repeat([]byte{0x29}, 256)))
	bootstrap := appbootstrap.Service{
		Clock:      clock,
		Passwords:  passwords,
		Secrets:    auth.NewSecretGenerator(bytes.NewReader(bytes.Repeat([]byte{0x51}, 64)), 32),
		Repository: storagesqlite.NewBootstrapRepository(database.Write()),
		Version:    "test",
	}
	initial, err := bootstrap.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	authentication := appauth.Service{
		Clock:       clock,
		Passwords:   passwords,
		Tokens:      auth.NewSecretGenerator(rand.Reader, 32),
		Repository:  storagesqlite.NewAuthenticationRepository(database.Read(), database.Write()),
		SessionLife: 12 * time.Hour,
	}
	states := appserverstate.Service{Repository: storagesqlite.NewServerStateRepository(database.Read(), database.Write())}
	handler, err := webhandler.New(webhandler.Dependencies{
		Authentication: authentication,
		ServerState:    states,
		CSRFSecrets:    auth.NewSecretGenerator(rand.Reader, 32),
	})
	if err != nil {
		t.Fatalf("web New() error = %v", err)
	}

	loginPage := httptest.NewRecorder()
	handler.ServeHTTP(loginPage, httptest.NewRequest(http.MethodGet, "/login", nil))
	if loginPage.Code != http.StatusOK {
		t.Fatalf("GET /login status = %d, body = %q", loginPage.Code, loginPage.Body.String())
	}
	assertSecurityHeaders(t, loginPage.Header())
	csrfCookie := responseCookie(t, loginPage.Result(), "uaw_csrf")
	csrfToken := hiddenValue(t, loginPage.Body.String(), "csrf_token")
	if !csrfCookie.HttpOnly || csrfCookie.SameSite != http.SameSiteStrictMode || csrfCookie.Value != csrfToken {
		t.Fatalf("CSRF cookie = %+v, token = %q", csrfCookie, csrfToken)
	}

	withoutCSRF := postFormRequest("/login", url.Values{
		"username": {"admin"}, "password": {initial.Password},
	})
	denied := httptest.NewRecorder()
	handler.ServeHTTP(denied, withoutCSRF)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("POST /login without CSRF status = %d, want 403", denied.Code)
	}

	loginRequest := postFormRequest("/login", url.Values{
		"csrf_token": {csrfToken}, "username": {"admin"}, "password": {initial.Password},
	})
	loginRequest.AddCookie(csrfCookie)
	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, loginRequest)
	if loginResponse.Code != http.StatusSeeOther || loginResponse.Header().Get("Location") != "/password/change" {
		t.Fatalf("POST /login status/location = %d %q", loginResponse.Code, loginResponse.Header().Get("Location"))
	}
	sessionCookie := responseCookie(t, loginResponse.Result(), "uaw_session")
	if !sessionCookie.HttpOnly || sessionCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie = %+v", sessionCookie)
	}

	setupBeforeChange := httptest.NewRecorder()
	setupRequest := httptest.NewRequest(http.MethodGet, "/setup/1", nil)
	setupRequest.AddCookie(sessionCookie)
	handler.ServeHTTP(setupBeforeChange, setupRequest)
	if setupBeforeChange.Code != http.StatusSeeOther || setupBeforeChange.Header().Get("Location") != "/password/change" {
		t.Fatalf("GET /setup/1 before password change = %d %q", setupBeforeChange.Code, setupBeforeChange.Header().Get("Location"))
	}

	changePage := httptest.NewRecorder()
	changeRequest := httptest.NewRequest(http.MethodGet, "/password/change", nil)
	changeRequest.AddCookie(sessionCookie)
	changeRequest.AddCookie(csrfCookie)
	handler.ServeHTTP(changePage, changeRequest)
	if changePage.Code != http.StatusOK {
		t.Fatalf("GET /password/change status = %d", changePage.Code)
	}
	changeToken := hiddenValue(t, changePage.Body.String(), "csrf_token")

	changePost := postFormRequest("/password/change", url.Values{
		"csrf_token":       {changeToken},
		"current_password": {initial.Password},
		"new_password":     {"a-new-strong-password"},
	})
	changePost.AddCookie(sessionCookie)
	changePost.AddCookie(csrfCookie)
	changeResponse := httptest.NewRecorder()
	handler.ServeHTTP(changeResponse, changePost)
	if changeResponse.Code != http.StatusSeeOther || changeResponse.Header().Get("Location") != "/" {
		t.Fatalf("POST /password/change = %d %q", changeResponse.Code, changeResponse.Header().Get("Location"))
	}
	rotatedCookie := responseCookie(t, changeResponse.Result(), "uaw_session")
	if rotatedCookie.Value == sessionCookie.Value {
		t.Fatal("password change did not rotate the session cookie")
	}

	// The home page is what routes each account: an administrator of a
	// universe still being configured is sent into the setup wizard.
	afterChange := httptest.NewRecorder()
	homeRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	homeRequest.AddCookie(rotatedCookie)
	handler.ServeHTTP(afterChange, homeRequest)
	if afterChange.Code != http.StatusSeeOther || afterChange.Header().Get("Location") != "/setup/1" {
		t.Fatalf("GET / as administrator = %d %q, want the setup", afterChange.Code, afterChange.Header().Get("Location"))
	}

	logoutRequest := postFormRequest("/logout", url.Values{"csrf_token": {changeToken}})
	logoutRequest.AddCookie(rotatedCookie)
	logoutRequest.AddCookie(csrfCookie)
	logoutResponse := httptest.NewRecorder()
	handler.ServeHTTP(logoutResponse, logoutRequest)
	if logoutResponse.Code != http.StatusSeeOther || logoutResponse.Header().Get("Location") != "/login" {
		t.Fatalf("POST /logout = %d %q", logoutResponse.Code, logoutResponse.Header().Get("Location"))
	}
	cleared := responseCookie(t, logoutResponse.Result(), "uaw_session")
	if cleared.MaxAge >= 0 {
		t.Fatalf("logout cookie MaxAge = %d, want deletion", cleared.MaxAge)
	}
	if _, err := authentication.Resolve(ctx, rotatedCookie.Value); !errors.Is(err, appauth.ErrInvalidSession) {
		t.Fatalf("Resolve(logged out session) error = %v, want ErrInvalidSession", err)
	}
}

func TestWebPublicAndProtectedRoutes(t *testing.T) {
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.August, 1, 0, 0, 0, 0, time.UTC))
	database, service, _ := authenticatedService(t, ctx, clock, time.Hour)
	states := appserverstate.Service{Repository: storagesqlite.NewServerStateRepository(database.Read(), database.Write())}
	handler, err := webhandler.New(webhandler.Dependencies{
		Authentication: service,
		ServerState:    states,
		CSRFSecrets:    auth.NewSecretGenerator(rand.Reader, 32),
	})
	if err != nil {
		t.Fatalf("web New() error = %v", err)
	}

	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK || !strings.Contains(health.Body.String(), "BOOTSTRAP_PENDING") {
		t.Fatalf("GET /healthz = %d %q", health.Code, health.Body.String())
	}

	root := httptest.NewRecorder()
	handler.ServeHTTP(root, httptest.NewRequest(http.MethodGet, "/", nil))
	if root.Code != http.StatusSeeOther || root.Header().Get("Location") != "/login" {
		t.Fatalf("GET / = %d %q", root.Code, root.Header().Get("Location"))
	}

	method := httptest.NewRecorder()
	handler.ServeHTTP(method, httptest.NewRequest(http.MethodPut, "/login", nil))
	if method.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PUT /login status = %d, want 405", method.Code)
	}
}

func postFormRequest(target string, values url.Values) *http.Request {
	request := httptest.NewRequest(http.MethodPost, target, strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return request
}

func responseCookie(t *testing.T, response *http.Response, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range response.Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	body, _ := io.ReadAll(response.Body)
	t.Fatalf("response has no %s cookie; body = %q", name, body)
	return nil
}

func hiddenValue(t *testing.T, body, name string) string {
	t.Helper()
	pattern := regexp.MustCompile(`name="` + regexp.QuoteMeta(name) + `" value="([^"]+)"`)
	match := pattern.FindStringSubmatch(body)
	if len(match) != 2 {
		t.Fatalf("body has no hidden %s: %s", name, body)
	}
	return match[1]
}

func assertSecurityHeaders(t *testing.T, header http.Header) {
	t.Helper()
	for _, name := range []string{"Content-Security-Policy", "X-Content-Type-Options", "Referrer-Policy", "X-Frame-Options"} {
		if header.Get(name) == "" {
			t.Errorf("missing security header %s", name)
		}
	}
}
