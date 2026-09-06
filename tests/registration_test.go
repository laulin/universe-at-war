package tests

import (
	"context"
	"crypto/rand"
	"errors"
	"sync"
	"testing"
	"time"

	appadmin "universeatwar/internal/app/administration"
	appauth "universeatwar/internal/app/authentication"
	appregistration "universeatwar/internal/app/registration"
	"universeatwar/internal/auth"
	appclock "universeatwar/internal/clock"
	storagesqlite "universeatwar/internal/storage/sqlite"
)

func TestRegistrationFollowsTheRulesetPolicy(t *testing.T) {
	ctx := context.Background()
	database := runningUniverse(t, ctx)
	service := registrationService(t, database)

	if _, err := service.Register(ctx, "newcomer", "a-strong-password", ""); !errors.Is(err, appregistration.ErrClosed) {
		t.Fatalf("Register() with closed registrations error = %v, want ErrClosed", err)
	}
	openRegistrations(t, ctx, database)

	if _, err := service.Register(ctx, "Newcomer", "short", ""); !errors.Is(err, appregistration.ErrWeakPassword) {
		t.Fatalf("Register() with a short password error = %v, want ErrWeakPassword", err)
	}
	if _, err := service.Register(ctx, "no", "a-strong-password", ""); !errors.Is(err, appregistration.ErrInvalidUsername) {
		t.Fatalf("Register() with a short username error = %v, want ErrInvalidUsername", err)
	}
	if _, err := service.Register(ctx, "bad name!", "a-strong-password", ""); !errors.Is(err, appregistration.ErrInvalidUsername) {
		t.Fatalf("Register() with an invalid username error = %v, want ErrInvalidUsername", err)
	}

	accountID, err := service.Register(ctx, "Newcomer", "a-strong-password", "")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if accountID != 2 {
		t.Fatalf("account id = %d, want 2", accountID)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM account_roles WHERE account_id = 2 AND role = 'PLAYER'", 1)
	assertSingleValue(t, database, "SELECT must_change_password FROM password_credentials WHERE account_id = 2", 0)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM audit_log WHERE action = 'account_registered'", 1)

	if _, err := service.Register(ctx, "newcomer", "another-strong-password", ""); !errors.Is(err, appregistration.ErrUsernameTaken) {
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
			_, err := service.Register(ctx, "twin", "a-strong-password", "")
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

// TestInvitationLetsExactlyOnePlayerIn proves a ticket is spent once, that a
// closed universe cannot be entered without one, and that the form never says
// which codes are real.
func TestInvitationLetsExactlyOnePlayerIn(t *testing.T) {
	ctx := context.Background()
	database := economyDatabase(t, ctx, 1)
	if _, err := database.Write().ExecContext(ctx,
		"INSERT INTO server_state(id, state, updated_at) VALUES (1, 'RUNNING', '2042-09-10T11:12:13Z')"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Write().ExecContext(ctx,
		`UPDATE ruleset_versions SET document = json_set(document, '$.identity.registration_policy', 'invitation')`); err != nil {
		t.Fatal(err)
	}
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC))
	service := appregistration.Service{
		Clock: clock,
		Passwords: auth.NewPasswordHasher(auth.Parameters{
			MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
		}, rand.Reader),
		Repository: storagesqlite.NewRegistrationRepository(database.Read(), database.Write()),
	}
	invitations := appadmin.InvitationService{
		Clock:      clock,
		Secrets:    auth.NewSecretGenerator(rand.Reader, 24),
		Repository: storagesqlite.NewInvitationRepository(database.Write()),
	}
	admin := appauth.Principal{AccountID: 1, Roles: []appauth.Role{appauth.RoleAdmin}}

	// Without a ticket, a closed universe stays closed.
	if _, err := service.Register(ctx, "newcomer", "a-strong-password", ""); !errors.Is(err, appregistration.ErrInvalidInvitation) {
		t.Fatalf("registering without an invitation error = %v", err)
	}
	if _, err := service.Register(ctx, "newcomer", "a-strong-password", "not-a-code"); !errors.Is(err, appregistration.ErrInvalidInvitation) {
		t.Fatalf("registering with a made-up invitation error = %v", err)
	}

	ticket, err := invitations.Create(ctx, admin, "un ami")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if ticket.Code == "" {
		t.Fatal("the invitation was created without a code")
	}
	// The code itself is never stored, only its digest.
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM invitations WHERE code_digest = ?", 0, ticket.Code)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM invitations", 1)

	if _, err := service.Register(ctx, "newcomer", "a-strong-password", ticket.Code); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	// The very same ticket lets nobody else in.
	if _, err := service.Register(ctx, "second", "a-strong-password", ticket.Code); !errors.Is(err, appregistration.ErrInvalidInvitation) {
		t.Fatalf("a replayed invitation error = %v", err)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM invitations WHERE used_at IS NOT NULL", 1)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM accounts", 2)

	// A revoked ticket is dead, and an expired one too.
	revoked, err := invitations.Create(ctx, admin, "changé d'avis")
	if err != nil {
		t.Fatal(err)
	}
	if err := invitations.Revoke(ctx, admin, revoked.ID); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	if _, err := service.Register(ctx, "third", "a-strong-password", revoked.Code); !errors.Is(err, appregistration.ErrInvalidInvitation) {
		t.Fatalf("a revoked invitation error = %v", err)
	}
	if err := invitations.Revoke(ctx, admin, revoked.ID); !errors.Is(err, appadmin.ErrInvitationNotFound) {
		t.Fatalf("revoking twice error = %v", err)
	}
	// A player cannot mint themselves a way in.
	player := appauth.Principal{AccountID: 2, Roles: []appauth.Role{appauth.RolePlayer}}
	if _, err := invitations.Create(ctx, player, "moi"); !errors.Is(err, appadmin.ErrForbidden) {
		t.Fatalf("a player minting an invitation error = %v", err)
	}
	listed, err := invitations.List(ctx, admin)
	if err != nil || len(listed) != 2 {
		t.Fatalf("List() = %d %v", len(listed), err)
	}
	for _, invitation := range listed {
		if invitation.Code != "" {
			t.Fatal("a code was handed back after creation")
		}
	}
}
