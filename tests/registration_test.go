package tests

import (
	"context"
	"crypto/rand"
	"errors"
	"sync"
	"testing"
	"time"

	appregistration "universeatwar/internal/app/registration"
	"universeatwar/internal/auth"
	appclock "universeatwar/internal/clock"
	storagesqlite "universeatwar/internal/storage/sqlite"
)

func TestRegistrationFollowsTheRulesetPolicy(t *testing.T) {
	ctx := context.Background()
	database := runningUniverse(t, ctx)
	service := registrationService(t, database)

	if _, err := service.Register(ctx, "newcomer", "a-strong-password"); !errors.Is(err, appregistration.ErrClosed) {
		t.Fatalf("Register() with closed registrations error = %v, want ErrClosed", err)
	}
	openRegistrations(t, ctx, database)

	if _, err := service.Register(ctx, "Newcomer", "short"); !errors.Is(err, appregistration.ErrWeakPassword) {
		t.Fatalf("Register() with a short password error = %v, want ErrWeakPassword", err)
	}
	if _, err := service.Register(ctx, "no", "a-strong-password"); !errors.Is(err, appregistration.ErrInvalidUsername) {
		t.Fatalf("Register() with a short username error = %v, want ErrInvalidUsername", err)
	}
	if _, err := service.Register(ctx, "bad name!", "a-strong-password"); !errors.Is(err, appregistration.ErrInvalidUsername) {
		t.Fatalf("Register() with an invalid username error = %v, want ErrInvalidUsername", err)
	}

	accountID, err := service.Register(ctx, "Newcomer", "a-strong-password")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if accountID != 2 {
		t.Fatalf("account id = %d, want 2", accountID)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM account_roles WHERE account_id = 2 AND role = 'PLAYER'", 1)
	assertSingleValue(t, database, "SELECT must_change_password FROM password_credentials WHERE account_id = 2", 0)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM audit_log WHERE action = 'account_registered'", 1)

	if _, err := service.Register(ctx, "newcomer", "another-strong-password"); !errors.Is(err, appregistration.ErrUsernameTaken) {
		t.Fatalf("Register() with a taken username error = %v, want ErrUsernameTaken", err)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM accounts", 2)
}

func TestConcurrentRegistrationsOfTheSameNameCreateOneAccount(t *testing.T) {
	ctx := context.Background()
	database := runningUniverse(t, ctx)
	openRegistrations(t, ctx, database)
	service := registrationService(t, database)

	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, err := service.Register(ctx, "twin", "a-strong-password")
			results <- err
		}()
	}
	close(start)
	wait.Wait()
	close(results)

	successes := 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, appregistration.ErrUsernameTaken):
		default:
			t.Fatalf("unexpected concurrent registration error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful registrations = %d, want 1", successes)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM accounts WHERE username_normalized = 'twin'", 1)
}

func registrationService(t *testing.T, database *storagesqlite.Database) appregistration.Service {
	t.Helper()
	return appregistration.Service{
		Clock: appclock.NewFake(time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC)),
		// crypto/rand is safe for concurrent use, which the concurrency test needs.
		Passwords: auth.NewPasswordHasher(auth.Parameters{
			MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
		}, rand.Reader),
		Repository: storagesqlite.NewRegistrationRepository(database.Read(), database.Write()),
	}
}

// runningUniverse prepares a database with one account, an active ruleset and a
// running server, which is the state players can register into.
func runningUniverse(t *testing.T, ctx context.Context) *storagesqlite.Database {
	t.Helper()
	database := economyDatabase(t, ctx, 1)
	if _, err := database.Write().ExecContext(ctx,
		"INSERT INTO server_state(id, state, updated_at) VALUES (1, 'RUNNING', '2042-09-10T11:12:13Z')"); err != nil {
		t.Fatalf("insert server state: %v", err)
	}
	return database
}

func openRegistrations(t *testing.T, ctx context.Context, database *storagesqlite.Database) {
	t.Helper()
	if _, err := database.Write().ExecContext(ctx,
		`UPDATE ruleset_versions SET document = json_set(document, '$.identity.registration_policy', 'open') WHERE status = 'active'`); err != nil {
		t.Fatalf("open registrations: %v", err)
	}
}
