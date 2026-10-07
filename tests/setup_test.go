package tests

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appbootstrap "universeatwar/internal/app/bootstrap"
	appserverstate "universeatwar/internal/app/serverstate"
	appsetup "universeatwar/internal/app/setup"
	"universeatwar/internal/auth"
	appclock "universeatwar/internal/clock"
	"universeatwar/internal/domain/rules"
	"universeatwar/internal/domain/server"
	storagesqlite "universeatwar/internal/storage/sqlite"
)

func TestSetupDraftAndActivationAreTransactional(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC)
	clock := appclock.NewFake(now)
	database := freshDatabase(t, ctx, "universe.db")
	passwords := auth.NewPasswordHasher(auth.Parameters{
		MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	}, bytes.NewReader(bytes.Repeat([]byte{0x37}, 256)))
	bootstrap := appbootstrap.Service{
		Clock: clock, Passwords: passwords,
		Secrets:    auth.NewSecretGenerator(bytes.NewReader(bytes.Repeat([]byte{0x47}, 64)), 32),
		Repository: storagesqlite.NewBootstrapRepository(database.Write()), Version: "test",
	}
	initial, err := bootstrap.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	authentication := appauth.Service{
		Clock: clock, Passwords: passwords, Tokens: auth.NewSecretGenerator(rand.Reader, 32),
		Repository: storagesqlite.NewAuthenticationRepository(database.Read(), database.Write()), SessionLife: time.Hour,
	}
	login, err := authentication.Login(ctx, "admin", initial.Password)
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	changed, err := authentication.ChangePassword(ctx, login.Token, initial.Password, "a-new-strong-password")
	if err != nil {
		t.Fatalf("ChangePassword() error = %v", err)
	}
	principal, err := authentication.Resolve(ctx, changed.Token)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	setup := appsetup.Service{
		Clock:      clock,
		Repository: storagesqlite.NewSetupRepository(database.Write()),
	}
	draft, err := setup.Load(ctx, principal)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if draft.Version != 1 || draft.Rules.Identity.Name == "" {
		t.Fatalf("initial draft = %+v", draft)
	}
	assertServerState(t, database, server.SetupInProgress)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM audit_log WHERE action = 'setup_started'", 1)

	draft.Rules.Identity.Name = "Campagne locale"
	draft.Rules.Identity.Description = "Univers de test"
	saved, err := setup.Save(ctx, principal, 1, draft.Version, draft.Rules)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if saved.Version != 2 || saved.CurrentStep != 2 {
		t.Fatalf("saved draft = %+v, want version 2 step 2", saved)
	}
	if _, err := setup.Save(ctx, principal, 1, draft.Version, draft.Rules); !errors.Is(err, appsetup.ErrConflict) {
		t.Fatalf("Save(stale) error = %v, want ErrConflict", err)
	}

	for step := 2; step <= 9; step++ {
		saved, err = setup.Save(ctx, principal, step, saved.Version, saved.Rules)
		if err != nil {
			t.Fatalf("Save(step %d) error = %v", step, err)
		}
	}
	if err := setup.Activate(ctx, principal, saved.Version, saved.Rules); err != nil {
		t.Fatalf("Activate() error = %v", err)
	}
	assertServerState(t, database, server.Running)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM ruleset_versions WHERE version = 1 AND status = 'active'", 1)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM audit_log WHERE action = 'setup_completed'", 1)

	var document string
	if err := database.Read().QueryRow("SELECT document FROM ruleset_versions WHERE version = 1").Scan(&document); err != nil {
		t.Fatalf("read active ruleset: %v", err)
	}
	decoded, err := rules.Decode([]byte(document))
	if err != nil {
		t.Fatalf("Decode(active ruleset) error = %v", err)
	}
	if decoded.Identity.Name != "Campagne locale" {
		t.Fatalf("active universe name = %q", decoded.Identity.Name)
	}
	states := appserverstate.Service{Repository: storagesqlite.NewServerStateRepository(database.Read(), database.Write())}
	if timezone, err := states.Timezone(ctx); err != nil || timezone != "Europe/Paris" {
		t.Fatalf("active timezone = %q, %v, want Europe/Paris", timezone, err)
	}
}

func assertServerState(t *testing.T, database *storagesqlite.Database, want server.State) {
	t.Helper()
	var got server.State
	if err := database.Read().QueryRow("SELECT state FROM server_state WHERE id = 1").Scan(&got); err != nil {
		t.Fatalf("read server state: %v", err)
	}
	if got != want {
		t.Fatalf("server state = %q, want %q", got, want)
	}
}
