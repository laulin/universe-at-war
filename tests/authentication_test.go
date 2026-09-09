package tests

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appbootstrap "universeatwar/internal/app/bootstrap"
	"universeatwar/internal/auth"
	appclock "universeatwar/internal/clock"
	storagesqlite "universeatwar/internal/storage/sqlite"
)

func TestAuthenticationSessionLifecycle(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2042, time.May, 6, 7, 8, 9, 0, time.UTC)
	clock := appclock.NewFake(now)
	database := freshDatabase(t, ctx, "universe.db")

	parameters := auth.Parameters{
		MemoryKiB:   8 * 1024,
		Iterations:  1,
		Parallelism: 1,
		SaltLength:  16,
		KeyLength:   32,
	}
	passwords := auth.NewPasswordHasher(parameters, bytes.NewReader(bytes.Repeat([]byte{0x21}, 256)))
	bootstrap := appbootstrap.Service{
		Clock:      clock,
		Passwords:  passwords,
		Secrets:    auth.NewSecretGenerator(bytes.NewReader(bytes.Repeat([]byte{0x42}, 64)), 32),
		Repository: storagesqlite.NewBootstrapRepository(database.Write()),
		Version:    "test",
	}
	initial, err := bootstrap.Initialize(ctx)
	if err != nil {
		t.Fatalf("bootstrap Initialize() error = %v", err)
	}

	repository := storagesqlite.NewAuthenticationRepository(database.Read(), database.Write())
	sessions := appauth.Service{
		Clock:     clock,
		Passwords: passwords,
		Tokens: auth.NewSecretGenerator(bytes.NewReader(bytes.Join([][]byte{
			bytes.Repeat([]byte{0x73}, 32),
			bytes.Repeat([]byte{0x74}, 32),
			bytes.Repeat([]byte{0x75}, 32),
		}, nil)), 32),
		Repository:  repository,
		SessionLife: 12 * time.Hour,
	}

	if _, err := sessions.Login(ctx, "admin", "wrong password"); !errors.Is(err, appauth.ErrInvalidCredentials) {
		t.Fatalf("Login(wrong) error = %v, want ErrInvalidCredentials", err)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM sessions", 0)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM audit_log WHERE action = 'login_failed'", 1)

	login, err := sessions.Login(ctx, " ADMIN ", initial.Password)
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if login.Token == "" || !login.MustChangePassword {
		t.Fatalf("Login() result = %+v, want token and mandatory password change", login)
	}
	digest := sha256.Sum256([]byte(login.Token))
	var storedDigest []byte
	if err := database.Read().QueryRow("SELECT token_digest FROM sessions").Scan(&storedDigest); err != nil {
		t.Fatalf("read token digest: %v", err)
	}
	if bytes.Equal(storedDigest, []byte(login.Token)) || !bytes.Equal(storedDigest, digest[:]) {
		t.Fatal("session storage does not contain exactly the SHA-256 token digest")
	}

	principal, err := sessions.Resolve(ctx, login.Token)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if principal.Username != "admin" || !principal.HasRole(appauth.RoleAdmin) || !principal.MustChangePassword {
		t.Fatalf("Resolve() principal = %+v", principal)
	}

	changed, err := sessions.ChangePassword(ctx, login.Token, initial.Password, "a-new-strong-password")
	if err != nil {
		t.Fatalf("ChangePassword() error = %v", err)
	}
	if changed.Token == "" || changed.Token == login.Token {
		t.Fatalf("ChangePassword() token = %q, want rotated token", changed.Token)
	}
	if _, err := sessions.Resolve(ctx, login.Token); !errors.Is(err, appauth.ErrInvalidSession) {
		t.Fatalf("Resolve(old token) error = %v, want ErrInvalidSession", err)
	}
	newPrincipal, err := sessions.Resolve(ctx, changed.Token)
	if err != nil {
		t.Fatalf("Resolve(new token) error = %v", err)
	}
	if newPrincipal.MustChangePassword {
		t.Fatal("new principal still requires a password change")
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM audit_log WHERE action = 'password_changed'", 1)

	if _, err := sessions.Login(ctx, "admin", initial.Password); !errors.Is(err, appauth.ErrInvalidCredentials) {
		t.Fatalf("Login(old password) error = %v, want ErrInvalidCredentials", err)
	}
	if _, err := sessions.Login(ctx, "admin", "a-new-strong-password"); err != nil {
		t.Fatalf("Login(new password) error = %v", err)
	}
}

func TestExpiredSessionIsRejected(t *testing.T) {
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.June, 1, 0, 0, 0, 0, time.UTC))
	database, service, password := authenticatedService(t, ctx, clock, time.Minute)

	login, err := service.Login(ctx, "admin", password)
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	clock.Advance(2 * time.Minute)
	if _, err := service.Resolve(ctx, login.Token); !errors.Is(err, appauth.ErrInvalidSession) {
		t.Fatalf("Resolve(expired) error = %v, want ErrInvalidSession", err)
	}
	_ = database
}

func authenticatedService(t *testing.T, ctx context.Context, clock *appclock.Fake, lifetime time.Duration) (*storagesqlite.Database, appauth.Service, string) {
	t.Helper()
	database := freshDatabase(t, ctx, "universe.db")
	passwords := auth.NewPasswordHasher(auth.Parameters{
		MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	}, bytes.NewReader(bytes.Repeat([]byte{0x17}, 128)))
	bootstrap := appbootstrap.Service{
		Clock:      clock,
		Passwords:  passwords,
		Secrets:    auth.NewSecretGenerator(bytes.NewReader(bytes.Repeat([]byte{0x55}, 64)), 32),
		Repository: storagesqlite.NewBootstrapRepository(database.Write()),
		Version:    "test",
	}
	result, err := bootstrap.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	service := appauth.Service{
		Clock:     clock,
		Passwords: passwords,
		Tokens: auth.NewSecretGenerator(bytes.NewReader(bytes.Join([][]byte{
			bytes.Repeat([]byte{0x63}, 32),
			bytes.Repeat([]byte{0x64}, 32),
			bytes.Repeat([]byte{0x65}, 32),
			bytes.Repeat([]byte{0x66}, 32),
		}, nil)), 32),
		Repository:  storagesqlite.NewAuthenticationRepository(database.Read(), database.Write()),
		SessionLife: lifetime,
	}
	return database, service, result.Password
}
