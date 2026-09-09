package tests

import (
	"bytes"
	"context"
	"encoding/base64"
	"testing"
	"time"

	appbootstrap "universeatwar/internal/app/bootstrap"
	"universeatwar/internal/auth"
	appclock "universeatwar/internal/clock"
	storagesqlite "universeatwar/internal/storage/sqlite"
)

func TestBootstrapCreatesAdminExactlyOnce(t *testing.T) {
	ctx := context.Background()
	database := freshDatabase(t, ctx, "universe.db")

	now := time.Date(2042, time.April, 5, 6, 7, 8, 0, time.UTC)
	passwords := auth.NewPasswordHasher(auth.Parameters{
		MemoryKiB:   8 * 1024,
		Iterations:  1,
		Parallelism: 1,
		SaltLength:  16,
		KeyLength:   32,
	}, bytes.NewReader(bytes.Repeat([]byte{0x31}, 64)))
	service := appbootstrap.Service{
		Clock:      appclock.NewFake(now),
		Passwords:  passwords,
		Secrets:    auth.NewSecretGenerator(bytes.NewReader(bytes.Repeat([]byte{0xA7}, 64)), 32),
		Repository: storagesqlite.NewBootstrapRepository(database.Write()),
		Version:    "test-version",
	}

	result, err := service.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if !result.Created || result.Username != "admin" || result.Password == "" {
		t.Fatalf("Initialize() result = %+v, want newly created admin secret", result)
	}
	rawSecret, err := base64.RawURLEncoding.DecodeString(result.Password)
	if err != nil || len(rawSecret) != 32 {
		t.Fatalf("bootstrap password has %d decoded bytes, error %v; want 32", len(rawSecret), err)
	}

	var accountID int64
	var encodedHash string
	var mustChange int
	if err := database.Read().QueryRowContext(ctx, `
		SELECT a.id, c.encoded_hash, c.must_change_password
		FROM accounts a
		JOIN password_credentials c ON c.account_id = a.id
		WHERE a.username_normalized = 'admin'
	`).Scan(&accountID, &encodedHash, &mustChange); err != nil {
		t.Fatalf("read bootstrap account: %v", err)
	}
	if encodedHash == result.Password || bytes.Contains([]byte(encodedHash), []byte(result.Password)) {
		t.Fatal("database contains the bootstrap password in clear text")
	}
	match, _, err := passwords.Verify(encodedHash, result.Password)
	if err != nil || !match {
		t.Fatalf("stored credential does not verify: match %v, error %v", match, err)
	}
	if mustChange != 1 {
		t.Fatalf("must_change_password = %d, want 1", mustChange)
	}

	assertSingleValue(t, database, "SELECT COUNT(*) FROM accounts", 1)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM account_roles WHERE role = 'ADMIN'", 1)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM audit_log WHERE action = 'bootstrap_admin_created'", 1)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM server_state WHERE state = 'BOOTSTRAP_PENDING'", 1)

	second, err := service.Initialize(ctx)
	if err != nil {
		t.Fatalf("second Initialize() error = %v", err)
	}
	if second.Created || second.Password != "" {
		t.Fatalf("second Initialize() result = %+v, want no secret", second)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM accounts", 1)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM audit_log WHERE action = 'bootstrap_admin_created'", 1)
	_ = accountID
}

func assertSingleValue(t *testing.T, database *storagesqlite.Database, query string, want int, arguments ...any) {
	t.Helper()
	var got int
	if err := database.Read().QueryRow(query, arguments...).Scan(&got); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	if got != want {
		t.Fatalf("query %q = %d, want %d", query, got, want)
	}
}
