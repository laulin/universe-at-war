package tests

import (
	"context"
	"fmt"
	"testing"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appfleet "universeatwar/internal/app/fleet"
	appclock "universeatwar/internal/clock"
	domaineconomy "universeatwar/internal/domain/economy"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/unit"
	"universeatwar/internal/domain/universe"
	storagesqlite "universeatwar/internal/storage/sqlite"
)

func TestFleetMissionsNeverCreateOrDestroyShips(t *testing.T) {
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC))
	database := economyDatabase(t, ctx, 2)
	universe := newWorld(t, database, clock)
	alice := appauth.Principal{AccountID: 1}

	home, err := universe.Economy.CreateEmpire(ctx, alice, "Alice")
	if err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	target, err := universe.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: 2}, "Bob")
	if err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	const fleetSize = 12
	setUnits(t, ctx, database, home.ID, "small_cargo", fleetSize)
	setBuilding(t, ctx, database, home.ID, "metal_storage", 4)
	setResearch(t, ctx, database, 1, "computer_technology", 3)
	setResources(t, ctx, database, home.ID, 100000, 5000, 5000)

	for round := range 6 {
		launched, err := universe.Fleet.Launch(ctx, alice, home.ID, appfleet.LaunchRequest{
			Target: target.Coordinate, TargetKind: domainfleet.TargetPlanet, Mission: domainfleet.MissionTransport,
			Composition: domainfleet.Composition{unit.SmallCargo: 2},
			Cargo:       domaineconomy.Resources{Metal: 100}, Percent: 10 * (round%10 + 1),
		}, fmt.Sprintf("round-%d", round))
		if err != nil {
			t.Fatalf("Launch(%d) error = %v", round, err)
		}
		assertShipsAreConserved(t, ctx, database, fleetSize)
		clock.Set(launched.ArrivesAt)
		if _, err := universe.Events.CompleteDue(ctx, 100); err != nil {
			t.Fatalf("CompleteDue() error = %v", err)
		}
		assertShipsAreConserved(t, ctx, database, fleetSize)
		clock.Set(*launched.ReturnsAt)
		if _, err := universe.Events.CompleteDue(ctx, 100); err != nil {
			t.Fatalf("CompleteDue() error = %v", err)
		}
		assertShipsAreConserved(t, ctx, database, fleetSize)
	}

	assertSingleValue(t, database, "SELECT COUNT(*) FROM fleets WHERE state <> 'completed'", 0)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM scheduled_events WHERE state <> 'completed'", 0)
	assertSingleValue(t, database, "SELECT quantity FROM planet_units WHERE planet_id = 1 AND unit_id = 'small_cargo'", fleetSize)

	// Every drop of deuterium spent is accounted for by the fuel of a mission.
	var spentFuel, stock int64
	if err := database.Read().QueryRowContext(ctx, "SELECT COALESCE(SUM(fuel), 0) FROM fleets").Scan(&spentFuel); err != nil {
		t.Fatal(err)
	}
	if err := database.Read().QueryRowContext(ctx, "SELECT deuterium FROM planet_resources WHERE planet_id = 1").Scan(&stock); err != nil {
		t.Fatal(err)
	}
	if stock > 5000-spentFuel {
		t.Fatalf("deuterium stock %d exceeds the initial 5000 minus %d of fuel", stock, spentFuel)
	}
}

func assertShipsAreConserved(t *testing.T, ctx context.Context, database *storagesqlite.Database, want int64) {
	t.Helper()
	var stationed, flying int64
	if err := database.Read().QueryRowContext(ctx,
		"SELECT COALESCE(SUM(quantity), 0) FROM planet_units WHERE unit_id = 'small_cargo'").Scan(&stationed); err != nil {
		t.Fatal(err)
	}
	if err := database.Read().QueryRowContext(ctx,
		"SELECT COALESCE(SUM(quantity), 0) FROM fleet_ships WHERE unit_id = 'small_cargo'").Scan(&flying); err != nil {
		t.Fatal(err)
	}
	if stationed+flying != want {
		t.Fatalf("ships stationed %d plus flying %d, want %d", stationed, flying, want)
	}
}

func BenchmarkFleetLaunch(b *testing.B) {
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC))
	database := benchmarkUniverse(b, ctx)
	game := newBenchmarkWorld(b, database, clock)
	alice := appauth.Principal{AccountID: 1}
	home, err := game.Economy.CreateEmpire(ctx, alice, "Alice")
	if err != nil {
		b.Fatal(err)
	}
	if _, err := game.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: 2}, "Bob"); err != nil {
		b.Fatal(err)
	}
	if _, err := database.Write().ExecContext(ctx,
		"INSERT INTO planet_units(planet_id, unit_id, quantity) VALUES (?, 'small_cargo', 1000000)", home.ID); err != nil {
		b.Fatal(err)
	}
	if _, err := database.Write().ExecContext(ctx,
		"INSERT INTO player_research(player_id, research_id, level) VALUES (1, 'computer_technology', 100000)"); err != nil {
		b.Fatal(err)
	}
	if _, err := database.Write().ExecContext(ctx,
		"UPDATE planet_resources SET deuterium = 1000000000 WHERE planet_id = ?", home.ID); err != nil {
		b.Fatal(err)
	}
	request := appfleet.LaunchRequest{
		Target: universe.Coordinate{Galaxy: 1, System: 1, Position: 1}, TargetKind: domainfleet.TargetPlanet, Mission: domainfleet.MissionTransport,
		Composition: domainfleet.Composition{unit.SmallCargo: 1}, Percent: 100,
	}
	index := 0
	for b.Loop() {
		if _, err := game.Fleet.Launch(ctx, alice, home.ID, request, fmt.Sprintf("bench-%d", index)); err != nil {
			b.Fatal(err)
		}
		index++
	}
}
