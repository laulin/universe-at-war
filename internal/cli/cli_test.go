package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"universeatwar/internal/auth"
	storagesqlite "universeatwar/internal/storage/sqlite"
)

func TestVersionCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	runner := Runner{Stdout: &stdout, Stderr: &stderr, Version: "1.2.3"}

	if code := runner.Run(context.Background(), []string{"version"}); code != 0 {
		t.Fatalf("Run(version) code = %d, stderr = %q", code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != "Universe At War 1.2.3" {
		t.Fatalf("version output = %q", got)
	}
}

func TestMigrateThenDoctor(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "campaign.db")
	var stdout, stderr bytes.Buffer
	runner := Runner{Stdout: &stdout, Stderr: &stderr, Version: "test"}

	if code := runner.Run(context.Background(), []string{"migrate", "--database", databasePath}); code != 0 {
		t.Fatalf("Run(migrate) code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "schema version") {
		t.Fatalf("migrate output = %q, want schema version", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"doctor", "--database", databasePath}); code != 0 {
		t.Fatalf("Run(doctor) code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "integrity: ok") {
		t.Fatalf("doctor output = %q, want integrity status", stdout.String())
	}
}

func TestUnknownCommandIsUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	runner := Runner{Stdout: &stdout, Stderr: &stderr, Version: "test"}

	if code := runner.Run(context.Background(), []string{"unknown"}); code != 2 {
		t.Fatalf("Run(unknown) code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("stderr = %q, want unknown command error", stderr.String())
	}
}

func TestMissingCommandPrintsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	runner := Runner{Stdout: &stdout, Stderr: &stderr, Version: "test"}

	if code := runner.Run(context.Background(), nil); code != 2 {
		t.Fatalf("Run(nil) code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("stderr = %q, want usage", stderr.String())
	}
}

func TestServeBootstrapsAndPrintsSecretOnlyOnce(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "campaign.db")
	var stdout, stderr bytes.Buffer
	served := 0
	runner := Runner{
		Stdout:  &stdout,
		Stderr:  &stderr,
		Version: "test",
		Random:  rand.Reader,
		PasswordParameters: auth.Parameters{
			MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
		},
		ServeHTTP: func(_ context.Context, server *http.Server) error {
			served++
			response := httptest.NewRecorder()
			server.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
			if response.Code != http.StatusOK {
				t.Fatalf("health status = %d, body = %q", response.Code, response.Body.String())
			}
			return http.ErrServerClosed
		},
	}

	arguments := []string{"serve", "--database", databasePath, "--listen", "127.0.0.1:0"}
	if code := runner.Run(context.Background(), arguments); code != 0 {
		t.Fatalf("first Run(serve) code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Bootstrap password:") {
		t.Fatalf("first serve output = %q, want bootstrap password", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), arguments); code != 0 {
		t.Fatalf("second Run(serve) code = %d, stderr = %q", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "Bootstrap password:") {
		t.Fatalf("second serve output leaked another bootstrap password: %q", stdout.String())
	}
	if served != 2 {
		t.Fatalf("ServeHTTP calls = %d, want 2", served)
	}
}

func TestServeReturnsListenerErrors(t *testing.T) {
	var stdout, stderr bytes.Buffer
	runner := Runner{
		Stdout:  &stdout,
		Stderr:  &stderr,
		Version: "test",
		Random:  rand.Reader,
		PasswordParameters: auth.Parameters{
			MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
		},
		ServeHTTP: func(context.Context, *http.Server) error {
			return errors.New("listener failed")
		},
	}
	code := runner.Run(context.Background(), []string{
		"serve", "--database", filepath.Join(t.TempDir(), "campaign.db"), "--listen", "127.0.0.1:0",
	})
	if code != 1 || !strings.Contains(stderr.String(), "listener failed") {
		t.Fatalf("Run(serve) = %d, stderr %q", code, stderr.String())
	}
}

func TestAdminResetPasswordReplacesCredential(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "campaign.db")
	var stdout, stderr bytes.Buffer
	runner := Runner{
		Stdout:  &stdout,
		Stderr:  &stderr,
		Version: "test",
		Random:  rand.Reader,
		PasswordParameters: auth.Parameters{
			MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
		},
		ServeHTTP: func(context.Context, *http.Server) error { return http.ErrServerClosed },
	}
	if code := runner.Run(context.Background(), []string{"serve", "--database", databasePath, "--listen", "127.0.0.1:0"}); code != 0 {
		t.Fatalf("Run(serve) code = %d, stderr = %q", code, stderr.String())
	}
	initialOutput := stdout.String()

	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"admin", "reset-password", "--database", databasePath, "--username", "admin"}); code != 0 {
		t.Fatalf("Run(admin reset-password) code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "New password:") || stdout.String() == initialOutput {
		t.Fatalf("reset output = %q, want a new one-time password", stdout.String())
	}

	ctx := context.Background()
	database, err := storagesqlite.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	assertSingleValueCLI(t, database, "SELECT must_change_password FROM password_credentials", 1)
	assertSingleValueCLI(t, database, "SELECT COUNT(*) FROM audit_log WHERE action = 'admin_password_reset'", 1)
}

func assertSingleValueCLI(t *testing.T, database *storagesqlite.Database, query string, want int) {
	t.Helper()
	var got int
	if err := database.Read().QueryRow(query).Scan(&got); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	if got != want {
		t.Fatalf("query %q = %d, want %d", query, got, want)
	}
}
