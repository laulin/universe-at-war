package tests

import (
	"context"
	"errors"
	"testing"
	"time"

	appai "universeatwar/internal/app/ai"
	appauth "universeatwar/internal/app/authentication"
	appclock "universeatwar/internal/clock"
	domainai "universeatwar/internal/domain/ai"
	storagesqlite "universeatwar/internal/storage/sqlite"
)

// TestArtificialPlayerIsAnOrdinaryAccountWithoutCredentials proves an
// artificial player owns an empire like anybody else and cannot be logged into.
func TestArtificialPlayerIsAnOrdinaryAccountWithoutCredentials(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, admin := aiUniverse(t)

	profile, err := universeWorld.AI.Create(ctx, admin, appai.Request{
		Name: "Kepler", Archetype: domainai.CautiousMiner,
		Window: domainai.Window{Start: 8, End: 23}, Interval: 5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if profile.Archetype != domainai.CautiousMiner || profile.Bodies != 1 || profile.Seed == 0 {
		t.Fatalf("profile = %+v", profile)
	}
	assertSingleText(t, database, "SELECT kind FROM accounts WHERE username = 'Kepler'", "ai")
	assertSingleValue(t, database, `
		SELECT COUNT(*) FROM password_credentials c
		JOIN accounts a ON a.id = c.account_id WHERE a.kind = 'ai'`, 0)
	assertSingleValue(t, database, `
		SELECT COUNT(*) FROM account_roles r
		JOIN accounts a ON a.id = r.account_id WHERE a.kind = 'ai' AND r.role = 'PLAYER'`, 1)
	assertSingleValue(t, database, `
		SELECT COUNT(*) FROM planets p JOIN players pl ON pl.id = p.owner_player_id
		JOIN accounts a ON a.id = pl.account_id WHERE a.kind = 'ai'`, 1)
	// The first reflection is planned, and nothing else has happened yet.
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM scheduled_events WHERE event_type = 'ai_think' AND state = 'pending'", 1)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM ai_decisions", 0)

	// Only an administrator may add or read an artificial player.
	player := appauth.Principal{AccountID: 1, Roles: []appauth.Role{appauth.RolePlayer}}
	if _, err := universeWorld.AI.Create(ctx, player, appai.Request{
		Name: "Pirate", Archetype: domainai.Raider,
		Window: domainai.Window{Start: 0, End: 0}, Interval: time.Minute,
	}); !errors.Is(err, appai.ErrForbidden) {
		t.Fatalf("a player creating an artificial rival error = %v", err)
	}
	if _, err := universeWorld.AI.List(ctx, player); !errors.Is(err, appai.ErrForbidden) {
		t.Fatalf("a player listing the artificial players error = %v", err)
	}
	if _, err := universeWorld.AI.Create(ctx, admin, appai.Request{
		Name: "Kepler", Archetype: domainai.Raider,
		Window: domainai.Window{Start: 0, End: 0}, Interval: time.Minute,
	}); !errors.Is(err, appai.ErrNameTaken) {
		t.Fatalf("a duplicate name error = %v", err)
	}
	if _, err := universeWorld.AI.Create(ctx, admin, appai.Request{
		Name: "Bulldozer", Archetype: "bulldozer",
		Window: domainai.Window{Start: 0, End: 0}, Interval: time.Minute,
	}); !errors.Is(err, domainai.ErrUnknownArchetype) {
		t.Fatalf("an unknown archetype error = %v", err)
	}
}

// TestReflectionSchedulesItselfAndSleepsOutsideTheHours proves the clock of an
// artificial player is durable and that no decision is taken while it sleeps.
func TestReflectionSchedulesItselfAndSleepsOutsideTheHours(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, admin := aiUniverse(t)

	// A player awake only in the morning, asked to think in the evening.
	profile, err := universeWorld.AI.Create(ctx, admin, appai.Request{
		Name: "Hypnos", Archetype: domainai.Turtle,
		Window: domainai.Window{Start: 6, End: 10}, Interval: 5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if profile.NextThinkAt == nil {
		t.Fatal("the first reflection was not planned")
	}
	setClock(t, universeWorld.Clock, *profile.NextThinkAt)
	if _, err := universeWorld.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}
	// It slept: it owes no reflection, and the next one falls after the opening.
	assertSingleValue(t, database, "SELECT COUNT(*) FROM ai_profiles WHERE due_think_at IS NOT NULL", 0)
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM ai_decisions WHERE action = 'sleep' AND outcome = 'skipped'", 1)
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM scheduled_events WHERE event_type = 'ai_think' AND state = 'pending'", 1)

	after, err := universeWorld.AI.Inspect(ctx, admin, profile.PlayerID)
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if after.NextThinkAt == nil || after.NextThinkAt.UTC().Hour() < 6 || after.NextThinkAt.UTC().Hour() > 10 {
		t.Fatalf("the next reflection falls outside the morning: %v", after.NextThinkAt)
	}
	if after.Awake {
		t.Fatal("the turtle is reported awake in the middle of the night")
	}

	// Once the morning comes, the reflection is owed and the chain goes on.
	setClock(t, universeWorld.Clock, *after.NextThinkAt)
	if _, err := universeWorld.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM ai_profiles WHERE due_think_at IS NOT NULL", 1)
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM scheduled_events WHERE event_type = 'ai_think' AND state = 'pending'", 1)
	due, err := universeWorld.Thinking.Due(ctx, 10)
	if err != nil || len(due) != 1 || due[0].Archetype != domainai.Turtle {
		t.Fatalf("Due() = %d %v", len(due), err)
	}
	if err := universeWorld.Thinking.Complete(ctx, due[0].PlayerID, []domainai.Decision{
		domainai.Skip(domainai.Strategic, "nothing", "the test decides nothing"),
	}); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM ai_profiles WHERE due_think_at IS NOT NULL", 0)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM ai_profiles WHERE last_think_at IS NOT NULL", 1)
}

// TestRetiringAnArtificialPlayerStopsItForGood proves a retired player thinks
// no more and leaves no pending event behind.
func TestRetiringAnArtificialPlayerStopsItForGood(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, admin := aiUniverse(t)
	profile, err := universeWorld.AI.Create(ctx, admin, appai.Request{
		Name: "Ephemere", Archetype: domainai.Scout,
		Window: domainai.Window{Start: 0, End: 0}, Interval: time.Minute,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := universeWorld.AI.Retire(ctx, admin, profile.PlayerID); err != nil {
		t.Fatalf("Retire() error = %v", err)
	}
	// Retiring twice is not an error, and nothing is planned any more.
	if err := universeWorld.AI.Retire(ctx, admin, profile.PlayerID); err != nil {
		t.Fatalf("Retire() replay error = %v", err)
	}
	assertSingleText(t, database, "SELECT state FROM ai_profiles WHERE player_id = ?", "retired", profile.PlayerID)
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM scheduled_events WHERE event_type = 'ai_think' AND state = 'pending'", 0)
	assertSingleText(t, database,
		"SELECT status FROM accounts WHERE id = ?", "disabled", profile.AccountID)
	// Its world stays on the map: retiring never rewrites the universe.
	assertSingleValue(t, database, "SELECT COUNT(*) FROM planets WHERE owner_player_id = ?", 1, profile.PlayerID)
	if err := universeWorld.AI.Retire(ctx, admin, 9999); !errors.Is(err, appai.ErrNotFound) {
		t.Fatalf("retiring nobody error = %v", err)
	}
}

// aiUniverse prepares a running universe with one human account and an
// administrator able to add artificial players.
func aiUniverse(t *testing.T) (*storagesqlite.Database, *world, appauth.Principal) {
	t.Helper()
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 2, 0, 0, 0, time.UTC))
	database := economyDatabase(t, ctx, 1)
	universeWorld := newWorld(t, database, clock)
	if _, err := universeWorld.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: 1}, "Alice"); err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	return database, universeWorld, appauth.Principal{AccountID: 1, Roles: []appauth.Role{appauth.RoleAdmin}}
}
