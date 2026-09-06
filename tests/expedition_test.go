package tests

import (
	"context"
	"errors"
	"testing"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appeconomy "universeatwar/internal/app/economy"
	appfleet "universeatwar/internal/app/fleet"
	appclock "universeatwar/internal/clock"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/unit"
	"universeatwar/internal/domain/universe"
	storagesqlite "universeatwar/internal/storage/sqlite"
)

// TestExpeditionFliesWaitsAndComesBack walks the whole mission: it needs a slot,
// it waits out there, its result is drawn once and written down, and the fleet
// comes home.
func TestExpeditionFliesWaitsAndComesBack(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, home := expeditionUniverse(t)
	pilot := appauth.Principal{AccountID: 1}

	// Without astrophysics there is no slot, so there is no expedition.
	if _, err := universeWorld.Fleet.Launch(ctx, pilot, home.ID, expeditionRequest(home.Coordinate, 4),
		"too-early"); !errors.Is(err, domainfleet.ErrNoExpeditionSlot) {
		t.Fatalf("an expedition without a slot error = %v", err)
	}
	setResearch(t, ctx, database, 1, "astrophysics", 4)

	launched, err := universeWorld.Fleet.Launch(ctx, pilot, home.ID, expeditionRequest(home.Coordinate, 4), "go")
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if launched.HoldsUntil == nil || launched.ReturnsAt == nil {
		t.Fatalf("an expedition without a wait: %+v", launched)
	}
	if !launched.HoldsUntil.After(launched.ArrivesAt) || !launched.ReturnsAt.After(*launched.HoldsUntil) {
		t.Fatalf("the schedule is out of order: %+v", launched)
	}
	assertSingleText(t, database, "SELECT target_kind FROM fleets WHERE id = 1", "space")
	assertSingleValue(t, database, "SELECT target_position FROM fleets WHERE id = 1", 16)

	// It lands on nothing at all and waits there.
	setClock(t, universeWorld.Clock, launched.ArrivesAt)
	if _, err := universeWorld.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}
	assertSingleText(t, database, "SELECT state FROM fleets WHERE id = 1", "holding")
	assertSingleValue(t, database, "SELECT COUNT(*) FROM reports", 0)

	// The draw happens at the end of the wait, and once.
	setClock(t, universeWorld.Clock, *launched.HoldsUntil)
	if _, err := universeWorld.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM reports WHERE kind = 'expedition'", 1)
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM game_event_log WHERE event_type = 'expedition_resolved'", 1)

	// Replaying the end of the wait changes nothing at all.
	before := cargoOf(t, ctx, database, 1)
	if _, err := database.Write().ExecContext(ctx,
		"UPDATE scheduled_events SET state = 'pending', processed_at = NULL WHERE event_type = 'holding_ended'"); err != nil {
		t.Fatal(err)
	}
	if _, err := universeWorld.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatalf("replayed CompleteDue() error = %v", err)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM reports WHERE kind = 'expedition'", 1)
	if after := cargoOf(t, ctx, database, 1); after != before {
		t.Fatalf("a redelivered expedition brought %d instead of %d", after, before)
	}
}

// TestTheSameSeedGivesTheSameExpeditionOutcome proves an expedition is
// replayable from what the database keeps.
func TestTheSameSeedGivesTheSameExpeditionOutcome(t *testing.T) {
	ctx := context.Background()
	outcomes := map[int]string{}
	for run := 0; run < 2; run++ {
		database, universeWorld, home := expeditionUniverse(t)
		pilot := appauth.Principal{AccountID: 1}
		setResearch(t, ctx, database, 1, "astrophysics", 4)
		launched, err := universeWorld.Fleet.Launch(ctx, pilot, home.ID, expeditionRequest(home.Coordinate, 4), "go")
		if err != nil {
			t.Fatalf("Launch() error = %v", err)
		}
		// The seed of a mission is drawn from the system; pinning it is what
		// makes "the same expedition" a statement one can test.
		if _, err := database.Write().ExecContext(ctx, "UPDATE fleets SET seed = 20421 WHERE id = 1"); err != nil {
			t.Fatal(err)
		}
		setClock(t, universeWorld.Clock, *launched.HoldsUntil)
		if _, err := universeWorld.Events.CompleteDue(ctx, 20); err != nil {
			t.Fatalf("CompleteDue() error = %v", err)
		}
		var outcome string
		if err := database.Read().QueryRowContext(ctx,
			`SELECT json_extract(payload, '$.outcome') FROM reports WHERE kind = 'expedition'`).Scan(&outcome); err != nil {
			t.Fatal(err)
		}
		outcomes[run] = outcome
	}
	if outcomes[0] != outcomes[1] {
		t.Fatalf("the same seed gave %q then %q", outcomes[0], outcomes[1])
	}
}

// TestExpeditionsAreBoundedBySlotsAndHold proves an expedition cannot replace
// the rest of the game: the slots limit how many fly, and the hold limits what
// they bring back.
func TestExpeditionsAreBoundedBySlotsAndHold(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, home := expeditionUniverse(t)
	pilot := appauth.Principal{AccountID: 1}
	setResearch(t, ctx, database, 1, "astrophysics", 1)
	setResearch(t, ctx, database, 1, "computer_technology", 5)

	if _, err := universeWorld.Fleet.Launch(ctx, pilot, home.ID, expeditionRequest(home.Coordinate, 2), "first"); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	// Astrophysics 1 allows exactly one expedition at a time.
	if _, err := universeWorld.Fleet.Launch(ctx, pilot, home.ID, expeditionRequest(home.Coordinate, 2),
		"second"); !errors.Is(err, domainfleet.ErrNoExpeditionSlot) {
		t.Fatalf("a second expedition error = %v", err)
	}
	// Whatever it finds fits in what it took with it.
	var launched appfleet.Fleet
	if err := database.Read().QueryRowContext(ctx,
		"SELECT id FROM fleets WHERE mission = 'expedition'").Scan(&launched.ID); err != nil {
		t.Fatal(err)
	}
	flyEverything(t, ctx, universeWorld)
	var carried, hold int64
	if err := database.Read().QueryRowContext(ctx,
		"SELECT metal + crystal + deuterium FROM fleet_cargo WHERE fleet_id = ?", launched.ID).Scan(&carried); err != nil {
		t.Fatal(err)
	}
	hold = 2 * 5000
	if carried > hold {
		t.Fatalf("the expedition carried %d in a hold of %d", carried, hold)
	}
}

func expeditionRequest(home universe.Coordinate, cargos int64) appfleet.LaunchRequest {
	return appfleet.LaunchRequest{
		Target:     expeditionSlot(home),
		TargetKind: domainfleet.TargetSpace, Mission: domainfleet.MissionExpedition,
		Composition: domainfleet.Composition{unit.SmallCargo: cargos}, Percent: 100,
	}
}

func expeditionSlot(home universe.Coordinate) universe.Coordinate {
	return universe.Coordinate{Galaxy: home.Galaxy, System: home.System, Position: 16}
}

func expeditionUniverse(t *testing.T) (*storagesqlite.Database, *world, appeconomy.Planet) {
	t.Helper()
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC))
	database := economyDatabase(t, ctx, 1)
	universeWorld := newWorld(t, database, clock)
	home, err := universeWorld.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: 1}, "Alice")
	if err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	setUnits(t, ctx, database, home.ID, "small_cargo", 10)
	setResearch(t, ctx, database, 1, "combustion_drive", 2)
	setResources(t, ctx, database, home.ID, 200000, 200000, 200000)
	return database, universeWorld, home
}

func cargoOf(t *testing.T, ctx context.Context, database *storagesqlite.Database, fleetID int64) int64 {
	t.Helper()
	var carried int64
	if err := database.Read().QueryRowContext(ctx,
		"SELECT metal + crystal + deuterium FROM fleet_cargo WHERE fleet_id = ?", fleetID).Scan(&carried); err != nil {
		t.Fatal(err)
	}
	return carried
}
