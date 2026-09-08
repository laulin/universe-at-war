package tests

import (
	"context"
	"database/sql"
	"testing"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appclock "universeatwar/internal/clock"
	"universeatwar/internal/domain/building"
	"universeatwar/internal/domain/research"
	"universeatwar/internal/domain/unit"
	storagesqlite "universeatwar/internal/storage/sqlite"
)

func TestMilestoneThreeProgressionFromEmptyEmpireToFleetAndDefense(t *testing.T) {
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC))
	database := economyDatabase(t, ctx, 1)
	universe := newWorld(t, database, clock)
	principal := appauth.Principal{AccountID: 1}

	planet, err := universe.Economy.CreateEmpire(ctx, principal, "Captain")
	if err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	// Storages first: a settlement caps the stock at the storage capacity.
	for _, storage := range []string{"metal_storage", "crystal_storage", "deuterium_tank"} {
		setBuilding(t, ctx, database, planet.ID, storage, 5)
	}
	setResources(t, ctx, database, planet.ID, 100000, 100000, 100000)

	// The laboratory is built through the ordinary construction use case.
	if _, err := universe.Economy.EnqueueBuilding(ctx, principal, planet.ID, building.ResearchLab, "lab"); err != nil {
		t.Fatalf("EnqueueBuilding(research_lab) error = %v", err)
	}
	advanceUntilIdle(t, ctx, universe, clock)
	assertSingleValue(t, database, "SELECT level FROM planet_buildings WHERE planet_id = 1 AND building_id = 'research_lab'", 1)

	// A research completes and unlocks the drive the light fighter needs.
	if _, err := universe.Research.EnqueueResearch(ctx, principal, planet.ID, research.EnergyTechnology, "energy"); err != nil {
		t.Fatalf("Start(energy) error = %v", err)
	}
	advanceUntilIdle(t, ctx, universe, clock)
	if _, err := universe.Research.EnqueueResearch(ctx, principal, planet.ID, research.CombustionDrive, "combustion"); err != nil {
		t.Fatalf("Start(combustion) error = %v", err)
	}
	advanceUntilIdle(t, ctx, universe, clock)
	assertSingleValue(t, database, "SELECT level FROM player_research WHERE player_id = 1 AND research_id = 'combustion_drive'", 1)

	// The shipyard requires the robotics factory, which the same queue builds.
	for _, id := range []building.ID{building.RoboticsFactory, building.RoboticsFactory, building.Shipyard} {
		if _, err := universe.Economy.EnqueueBuilding(ctx, principal, planet.ID, id, string(id)+time.Now().String()); err != nil {
			t.Fatalf("EnqueueBuilding(%s) error = %v", id, err)
		}
		advanceUntilIdle(t, ctx, universe, clock)
	}
	assertSingleValue(t, database, "SELECT level FROM planet_buildings WHERE planet_id = 1 AND building_id = 'shipyard'", 1)

	// Ships and defenses are produced from the same shipyard, one order at a time.
	if _, err := universe.Shipyard.Order(ctx, principal, planet.ID, unit.LightFighter, 2, "fighters"); err != nil {
		t.Fatalf("Order(light_fighter) error = %v", err)
	}
	advanceUntilIdle(t, ctx, universe, clock)
	if _, err := universe.Shipyard.Order(ctx, principal, planet.ID, unit.RocketLauncher, 3, "launchers"); err != nil {
		t.Fatalf("Order(rocket_launcher) error = %v", err)
	}
	advanceUntilIdle(t, ctx, universe, clock)

	assertSingleValue(t, database, "SELECT quantity FROM planet_units WHERE planet_id = 1 AND unit_id = 'light_fighter'", 2)
	assertSingleValue(t, database, "SELECT quantity FROM planet_units WHERE planet_id = 1 AND unit_id = 'rocket_launcher'", 3)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM scheduled_events WHERE state <> 'completed'", 0)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM production_orders WHERE state <> 'completed'", 0)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM research_queue WHERE state <> 'completed'", 0)

	var metal, crystal, deuterium int64
	if err := database.Read().QueryRowContext(ctx,
		"SELECT metal, crystal, deuterium FROM planet_resources WHERE planet_id = 1").Scan(&metal, &crystal, &deuterium); err != nil {
		t.Fatal(err)
	}
	if metal < 0 || crystal < 0 || deuterium < 0 {
		t.Fatalf("negative stock after the whole progression: %d/%d/%d", metal, crystal, deuterium)
	}
}

func TestEventsDueAtTheSameInstantFollowTheDocumentedPriority(t *testing.T) {
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC))
	database := economyDatabase(t, ctx, 1)
	universe := newWorld(t, database, clock)
	principal := appauth.Principal{AccountID: 1}

	planet, err := universe.Economy.CreateEmpire(ctx, principal, "Captain")
	if err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	setBuilding(t, ctx, database, planet.ID, "research_lab", 1)
	setBuilding(t, ctx, database, planet.ID, "shipyard", 1)
	setResources(t, ctx, database, planet.ID, 100000, 100000, 100000)

	if _, err := universe.Shipyard.Order(ctx, principal, planet.ID, unit.RocketLauncher, 1, "launcher"); err != nil {
		t.Fatalf("Order() error = %v", err)
	}
	if _, err := universe.Research.EnqueueResearch(ctx, principal, planet.ID, research.EnergyTechnology, "energy"); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, err := universe.Economy.EnqueueBuilding(ctx, principal, planet.ID, building.MetalMine, "mine"); err != nil {
		t.Fatalf("EnqueueBuilding() error = %v", err)
	}
	if _, err := database.Write().ExecContext(ctx,
		"UPDATE scheduled_events SET due_at = '2042-09-10T12:00:00Z' WHERE state = 'pending'"); err != nil {
		t.Fatal(err)
	}

	clock.Set(time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC))
	if _, err := universe.Events.CompleteDue(ctx, 10); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}

	rows, err := database.Read().QueryContext(ctx, `
		SELECT event_type FROM game_event_log
		WHERE event_type IN ('building_completed', 'research_completed', 'production_completed')
		ORDER BY id
	`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var order []string
	for rows.Next() {
		var eventType string
		if err := rows.Scan(&eventType); err != nil {
			t.Fatal(err)
		}
		order = append(order, eventType)
	}
	want := []string{"building_completed", "research_completed", "production_completed"}
	if len(order) != len(want) {
		t.Fatalf("completion order = %v, want %v", order, want)
	}
	for index := range want {
		if order[index] != want[index] {
			t.Fatalf("completion order = %v, want %v", order, want)
		}
	}
}

func TestEmpireWithoutAnyProgressionStaysPlayable(t *testing.T) {
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC))
	database := economyDatabase(t, ctx, 1)
	universe := newWorld(t, database, clock)
	principal := appauth.Principal{AccountID: 1}

	planet, err := universe.Economy.CreateEmpire(ctx, principal, "Captain")
	if err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	// Nothing is ever written to the research or production tables here: a game
	// started before this milestone must keep working.
	if _, _, err := universe.Economy.Buildings(ctx, principal, planet.ID); err != nil {
		t.Fatalf("Buildings() error = %v", err)
	}
	overview, err := universe.Research.Overview(ctx, principal, planet.ID)
	if err != nil {
		t.Fatalf("Research overview error = %v", err)
	}
	if len(overview.Levels) != 0 || len(overview.Queue) != 0 {
		t.Fatalf("fresh empire has research state: %+v", overview)
	}
	ships, err := universe.Shipyard.Ships(ctx, principal, planet.ID)
	if err != nil {
		t.Fatalf("Ships() error = %v", err)
	}
	if len(ships.Inventory) != 0 || len(ships.Queue) != 0 {
		t.Fatalf("fresh empire has production state: %+v", ships)
	}
	defenses, err := universe.Shipyard.Defenses(ctx, principal, planet.ID)
	if err != nil || len(defenses.Choices) == 0 {
		t.Fatalf("Defenses() = %d choices, %v", len(defenses.Choices), err)
	}
}

// advanceUntilIdle moves the clock to the next deadline and settles it, until
// nothing is due any more. The bound is generous because a queue turns one
// order into a chain of deadlines.
func advanceUntilIdle(t *testing.T, ctx context.Context, universe *world, clock *appclock.Fake) {
	t.Helper()
	for range 128 {
		dueAt, ok, err := universe.Events.NextDue(ctx)
		if err != nil {
			t.Fatalf("NextDue() error = %v", err)
		}
		if !ok {
			return
		}
		if dueAt.After(clock.Now()) {
			if err := clock.Set(dueAt); err != nil {
				t.Fatalf("Set() error = %v", err)
			}
		}
		if _, err := universe.Events.CompleteDue(ctx, 100); err != nil {
			t.Fatalf("CompleteDue() error = %v", err)
		}
	}
	t.Fatal("the simulation never became idle")
}

func BenchmarkEventProcessorBacklog(b *testing.B) {
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC))
	for b.Loop() {
		b.StopTimer()
		database := benchmarkDatabase(b, ctx)
		processor := storagesqlite.NewEventProcessor(database.Write(), clock)
		processor.Register("benchmark", func(context.Context, *sql.Tx, storagesqlite.ScheduledEvent, time.Time) error { return nil })
		for index := range 1000 {
			if _, err := database.Write().ExecContext(ctx, `
				INSERT INTO scheduled_events(event_type, due_at, priority, entity_type, entity_id, idempotency_key, created_at)
				VALUES ('benchmark', '2042-09-10T11:00:00Z', 50, 'test', ?, ?, '2042-09-10T10:00:00Z')
			`, index, index); err != nil {
				b.Fatal(err)
			}
		}
		b.StartTimer()
		if _, err := processor.CompleteDue(ctx, 1000); err != nil {
			b.Fatal(err)
		}
	}
}
