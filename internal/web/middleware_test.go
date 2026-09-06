package web

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"universeatwar/internal/observability"
)

func TestRequestLoggerCorrelatesWithoutLeakingSecrets(t *testing.T) {
	var recorded bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&recorded, nil))
	metrics := observability.NewMetrics()
	handler := requestID(requestLogger(logger, metrics, http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if RequestIDFrom(request.Context()) == "" {
			t.Error("handler saw no request identifier in its context")
		}
		response.WriteHeader(http.StatusTeapot)
	})))

	request := httptest.NewRequest(http.MethodGet, "/planets/1?token=secret-query", nil)
	request.AddCookie(&http.Cookie{Name: "uaw_session", Value: "super-secret-session"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	identifier := response.Header().Get("X-Request-Id")
	if len(identifier) != 32 {
		t.Fatalf("X-Request-Id = %q, want 32 hexadecimal characters", identifier)
	}
	line := recorded.String()
	for _, want := range []string{`"status":418`, `"method":"GET"`, `"path":"/planets/1"`, `"request_id":"` + identifier + `"`, `"duration_ms"`} {
		if !strings.Contains(line, want) {
			t.Fatalf("log line %s does not contain %s", line, want)
		}
	}
	for _, secret := range []string{"super-secret-session", "secret-query", "uaw_session"} {
		if strings.Contains(line, secret) {
			t.Fatalf("log line leaks %q: %s", secret, line)
		}
	}
}

func TestRequestIDIsUniquePerRequest(t *testing.T) {
	handler := requestID(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}))
	seen := map[string]bool{}
	for range 16 {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
		identifier := response.Header().Get("X-Request-Id")
		if seen[identifier] {
			t.Fatalf("request identifier %q was reused", identifier)
		}
		seen[identifier] = true
	}
}
