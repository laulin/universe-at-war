package tests

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	appauth "universeatwar/internal/app/authentication"
	webhandler "universeatwar/internal/web"
)

func TestWebRegistrationRespectsPolicyAndCSRF(t *testing.T) {
	ctx := context.Background()
	database := runningUniverse(t, ctx)
	handler, err := webhandler.New(webhandler.Dependencies{
		Authentication: webAuthenticationStub{principal: appauth.Principal{AccountID: 1}},
		ServerState:    runningStateStub{},
		CSRFSecrets:    sequenceSecret{value: "csrf-token"},
		Registration:   registrationService(t, database),
	})
	if err != nil {
		t.Fatal(err)
	}

	closed := httptest.NewRecorder()
	handler.ServeHTTP(closed, httptest.NewRequest(http.MethodGet, "/register", nil))
	if closed.Code != http.StatusNotFound {
		t.Fatalf("GET /register with closed registrations = %d, want 404", closed.Code)
	}
	login := getPage(t, handler, "/login")
	if strings.Contains(login, `href="/register"`) {
		t.Fatalf("login page advertises registration while it is closed: %q", login)
	}

	openRegistrations(t, ctx, database)
	form := getPage(t, handler, "/register")
	if !strings.Contains(form, `name="username"`) || !strings.Contains(form, `name="password"`) {
		t.Fatalf("registration form = %q", form)
	}
	if openLogin := getPage(t, handler, "/login"); !strings.Contains(openLogin, `href="/register"`) {
		t.Fatalf("login page hides the registration link while it is open: %q", openLogin)
	}

	denied := httptest.NewRecorder()
	handler.ServeHTTP(denied, postFormRequest("/register", url.Values{"username": {"newcomer"}, "password": {"a-strong-password"}}))
	if denied.Code != http.StatusForbidden {
		t.Fatalf("POST /register without CSRF = %d, want 403", denied.Code)
	}

	csrfCookie := &http.Cookie{Name: "uaw_csrf", Value: "csrf-token"}
	weak := postFormRequest("/register", url.Values{"csrf_token": {"csrf-token"}, "username": {"newcomer"}, "password": {"short"}, "password_confirmation": {"short"}})
	weak.AddCookie(csrfCookie)
	refused := httptest.NewRecorder()
	handler.ServeHTTP(refused, weak)
	if refused.Code != http.StatusBadRequest {
		t.Fatalf("POST /register with a weak password = %d, want 400", refused.Code)
	}
	if body := refused.Body.String(); strings.Contains(body, "registration:") {
		t.Fatalf("registration error leaked a technical message: %q", body)
	}

	created := postFormRequest("/register", url.Values{"csrf_token": {"csrf-token"}, "username": {"newcomer"}, "password": {"a-strong-password"}, "password_confirmation": {"a-strong-password"}})
	created.AddCookie(csrfCookie)
	accepted := httptest.NewRecorder()
	handler.ServeHTTP(accepted, created)
	if accepted.Code != http.StatusSeeOther || accepted.Header().Get("Location") != "/login?registered=1" {
		t.Fatalf("POST /register = %d %q %q", accepted.Code, accepted.Header().Get("Location"), accepted.Body.String())
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM accounts WHERE username_normalized = 'newcomer'", 1)

	duplicate := postFormRequest("/register", url.Values{"csrf_token": {"csrf-token"}, "username": {"newcomer"}, "password": {"a-strong-password"}, "password_confirmation": {"a-strong-password"}})
	duplicate.AddCookie(csrfCookie)
	conflict := httptest.NewRecorder()
	handler.ServeHTTP(conflict, duplicate)
	if conflict.Code != http.StatusBadRequest {
		t.Fatalf("POST /register with a taken username = %d, want 400", conflict.Code)
	}
	if body := conflict.Body.String(); strings.Contains(body, "déjà") || strings.Contains(body, "existe") {
		t.Fatalf("registration response reveals that the account exists: %q", body)
	}
}
