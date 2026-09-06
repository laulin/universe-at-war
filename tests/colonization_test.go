package tests

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appfleet "universeatwar/internal/app/fleet"
	appclock "universeatwar/internal/clock"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/unit"
	"universeatwar/internal/domain/universe"
	storagesqlite "universeatwar/internal/storage/sqlite"
)

func TestColonizationFoundsAPlanetAndSendsTheEscortHome(t *testing.T) {
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC))
	database := economyDatabase(t, ctx, 1)
	universeWorld := newWorld(t, database, clock)
	alice := appauth.Principal{AccountID: 1}

	home, err := universeWorld.Economy.CreateEmpire(ctx, alice, "Alice")
	if err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	setUnits(t, ctx, database, home.ID, "colony_ship", 1)
	setUnits(t, ctx, database, home.ID, "small_cargo", 2)
	setResearch(t, ctx, database, 1, "astrophysics", 1)
	setResources(t, ctx, database, home.ID, 5000, 5000, 5000)

	target := universe.Coordinate{Galaxy: 1, System: 1, Position: 4}
	launched, err := universeWorld.Fleet.Launch(ctx, alice, home.ID, appfleet.LaunchRequest{
		Target: target, TargetKind: domainfleet.TargetEmpty, Mission: domainfleet.MissionColonize,
		Composition: domainfleet.Composition{unit.ColonyShip: 1, unit.SmallCargo: 2},
		Percent:     100,
	}, "colonize")
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}

	clock.Set(launched.ArrivesAt)
	if _, err := universeWorld.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM planets WHERE galaxy = 1 AND system = 1 AND position = 4", 1)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM planet_resources WHERE planet_id = 2", 1)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM game_event_log WHERE event_type = 'colony_founded'", 1)
	assertSingleText(t, database, "SELECT kind FROM planets WHERE id = 2", "planet")
	// The colony ship stays behind, the cargos fly home.
	assertSingleValue(t, database, "SELECT COUNT(*) FROM fleet_ships WHERE fleet_id = 1 AND unit_id = 'colony_ship'", 0)
	assertSingleValue(t, database, "SELECT quantity FROM fleet_ships WHERE fleet_id = 1 AND unit_id = 'small_cargo'", 2)
	assertSingleText(t, database, "SELECT state FROM fleets WHERE id = 1", "returning")

	var fields, temperature int
	if err := database.Read().QueryRowContext(ctx,
		"SELECT total_fields, maximum_temperature FROM planets WHERE id = 2").Scan(&fields, &temperature); err != nil {
		t.Fatal(err)
	}
	if fields < 120 || fields > 260 {
		t.Fatalf("the colony has %d fields, outside the configured range", fields)
	}
	if temperature != 80 {
		t.Fatalf("a planet on position four is at %d degrees, want 80", temperature)
	}

	clock.Set(*launched.ReturnsAt)
	if _, err := universeWorld.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}
	assertSingleValue(t, database, "SELECT quantity FROM planet_units WHERE planet_id = 1 AND unit_id = 'small_cargo'", 2)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM planet_units WHERE planet_id = 1 AND unit_id = 'colony_ship' AND quantity > 0", 0)

	planets, err := universeWorld.Economy.Planets(ctx, alice)
	if err != nil || len(planets) != 2 {
		t.Fatalf("Planets() = %d, %v", len(planets), err)
	}
}

func TestColonizationNeedsSlotsShipsAndAnEmptyPosition(t *testing.T) {
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC))
	database := economyDatabase(t, ctx, 1)
	universeWorld := newWorld(t, database, clock)
	alice := appauth.Principal{AccountID: 1}

	home, err := universeWorld.Economy.CreateEmpire(ctx, alice, "Alice")
	if err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	setUnits(t, ctx, database, home.ID, "colony_ship", 2)
	setUnits(t, ctx, database, home.ID, "small_cargo", 2)
	setResources(t, ctx, database, home.ID, 5000, 5000, 5000)

	request := appfleet.LaunchRequest{
		Target: universe.Coordinate{Galaxy: 1, System: 1, Position: 4}, TargetKind: domainfleet.TargetEmpty,
		Mission: domainfleet.MissionColonize, Composition: domainfleet.Composition{unit.ColonyShip: 1}, Percent: 100,
	}
	if _, err := universeWorld.Fleet.Launch(ctx, alice, home.ID, request, "no-astro"); !errors.Is(err, domainfleet.ErrNoColonySlot) {
		t.Fatalf("colonising without astrophysics error = %v, want ErrNoColonySlot", err)
	}
	setResearch(t, ctx, database, 1, "astrophysics", 1)

	withoutShip := request
	withoutShip.Composition = domainfleet.Composition{unit.SmallCargo: 1}
	if _, err := universeWorld.Fleet.Launch(ctx, alice, home.ID, withoutShip, "no-ship"); !errors.Is(err, domainfleet.ErrCompositionMismatch) {
		t.Fatalf("colonising without a colony ship error = %v", err)
	}
	onHome := request
	onHome.Target = home.Coordinate
	if _, err := universeWorld.Fleet.Launch(ctx, alice, home.ID, onHome, "occupied"); !errors.Is(err, domainfleet.ErrInvalidTarget) {
		t.Fatalf("colonising an occupied position error = %v", err)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM fleets", 0)
}

func TestTwoColonizationsOnTheSamePositionCreateOnePlanet(t *testing.T) {
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC))
	database := economyDatabase(t, ctx, 2)
	universeWorld := newWorld(t, database, clock)
	alice := appauth.Principal{AccountID: 1}
	bob := appauth.Principal{AccountID: 2}

	first, err := universeWorld.Economy.CreateEmpire(ctx, alice, "Alice")
	if err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	second, err := universeWorld.Economy.CreateEmpire(ctx, bob, "Bob")
	if err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	for _, planet := range []int64{first.ID, second.ID} {
		setUnits(t, ctx, database, planet, "colony_ship", 1)
		setResources(t, ctx, database, planet, 5000, 5000, 5000)
	}
	setResearch(t, ctx, database, 1, "astrophysics", 1)
	setResearch(t, ctx, database, 2, "astrophysics", 1)

	target := universe.Coordinate{Galaxy: 1, System: 1, Position: 4}
	request := appfleet.LaunchRequest{
		Target: target, TargetKind: domainfleet.TargetEmpty, Mission: domainfleet.MissionColonize,
		Composition: domainfleet.Composition{unit.ColonyShip: 1}, Percent: 100,
	}
	alicesFleet, err := universeWorld.Fleet.Launch(ctx, alice, first.ID, request, "alice")
	if err != nil {
		t.Fatalf("Launch(alice) error = %v", err)
	}
	if _, err := universeWorld.Fleet.Launch(ctx, bob, second.ID, request, "bob"); err != nil {
		t.Fatalf("Launch(bob) error = %v", err)
	}

	clock.Set(alicesFleet.ArrivesAt)
	if _, err := universeWorld.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM planets WHERE galaxy = 1 AND system = 1 AND position = 4", 1)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM game_event_log WHERE event_type = 'colony_founded'", 1)
	// The loser flies home with its colony ship untouched.
	assertSingleValue(t, database, "SELECT COUNT(*) FROM game_event_log WHERE event_type = 'mission_aborted'", 1)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM fleets WHERE state = 'returning'", 1)
}

func TestConcurrentColonizationRequestsSpendOneColonyShip(t *testing.T) {
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC))
	database := economyDatabase(t, ctx, 1)
	universeWorld := newWorld(t, database, clock)
	alice := appauth.Principal{AccountID: 1}

	home, err := universeWorld.Economy.CreateEmpire(ctx, alice, "Alice")
	if err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	setUnits(t, ctx, database, home.ID, "colony_ship", 1)
	setResearch(t, ctx, database, 1, "astrophysics", 9)
	setResearch(t, ctx, database, 1, "computer_technology", 5)
	setResources(t, ctx, database, home.ID, 50000, 50000, 50000)

	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for index, position := range []int{4, 5} {
		wait.Add(1)
		go func(index, position int) {
			defer wait.Done()
			<-start
			_, err := universeWorld.Fleet.Launch(ctx, alice, home.ID, appfleet.LaunchRequest{
				Target:     universe.Coordinate{Galaxy: 1, System: 1, Position: position},
				TargetKind: domainfleet.TargetEmpty, Mission: domainfleet.MissionColonize,
				Composition: domainfleet.Composition{unit.ColonyShip: 1}, Percent: 100,
			}, colonyKey(index))
			results <- err
		}(index, position)
	}
	close(start)
	wait.Wait()
	close(results)

	successes := 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, domainfleet.ErrInsufficientUnits):
		default:
			t.Fatalf("unexpected concurrent error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful launches = %d, want 1", successes)
	}
	assertNoNegativeInventory(t, ctx, database)
}

func colonyKey(index int) string {
	if index == 0 {
		return "colony-first"
	}
	return "colony-second"
}

func assertNoNegativeInventory(t *testing.T, ctx context.Context, database *storagesqlite.Database) {
	t.Helper()
	var negatives int
	if err := database.Read().QueryRowContext(ctx, "SELECT COUNT(*) FROM planet_units WHERE quantity < 0").Scan(&negatives); err != nil {
		t.Fatal(err)
	}
	if negatives != 0 {
		t.Fatal("an inventory went negative")
	}
}
