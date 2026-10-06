package tests

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appeconomy "universeatwar/internal/app/economy"
	appclock "universeatwar/internal/clock"
	"universeatwar/internal/domain/building"
	domaineconomy "universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/rules"
	storagesqlite "universeatwar/internal/storage/sqlite"
)

func TestEconomyProgressionFromEmpireToCompletedBuilding(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC)
	clock := appclock.NewFake(now)
	database := economyDatabase(t, ctx, 1)
	universe := newWorld(t, database, clock)
	service := universe.Economy
	principal := appauth.Principal{AccountID: 1, Username: "captain"}

	planet, err := service.CreateEmpire(ctx, principal, "Captain")
	if err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	if planet.Coordinate.String() != "1:1:8" || planet.Stock != (domaineconomy.Resources{Metal: 500, Crystal: 500}) {
		t.Fatalf("created planet = %#v", planet)
	}
	if _, err := service.CreateEmpire(ctx, principal, "Another"); !errors.Is(err, appeconomy.ErrEmpireExists) {
		t.Fatalf("second CreateEmpire() error = %v", err)
	}

	clock.Advance(2 * time.Hour)
	planet, err = service.Planet(ctx, principal, 0)
	if err != nil {
		t.Fatalf("Planet() error = %v", err)
	}
	if planet.Stock != (domaineconomy.Resources{Metal: 560, Crystal: 530}) {
		t.Fatalf("stock after two hours = %#v", planet.Stock)
	}

	queue, err := service.EnqueueBuilding(ctx, principal, planet.ID, building.MetalMine, "build-1")
	if err != nil {
		t.Fatalf("EnqueueBuilding() error = %v", err)
	}
	if queue.Cost != (domaineconomy.Resources{Metal: 60, Crystal: 15}) || queue.CompletesAt.Sub(queue.StartedAt) != 108*time.Second {
		t.Fatalf("queue = %#v", queue)
	}
	replayed, err := service.EnqueueBuilding(ctx, principal, planet.ID, building.MetalMine, "build-1")
	if err != nil || replayed.ID != queue.ID {
		t.Fatalf("idempotent replay = %#v, %v", replayed, err)
	}
	// A second order no longer collides with the first: it waits behind it, and
	// pays on the spot all the same.
	behind, err := service.EnqueueBuilding(ctx, principal, planet.ID, building.SolarPlant, "build-2")
	if err != nil || behind.Position != 1 || behind.State != "queued" {
		t.Fatalf("second order = %#v, %v", behind, err)
	}
	if !behind.StartedAt.IsZero() || !behind.CompletesAt.IsZero() {
		t.Fatalf("a waiting order was given a schedule: %#v", behind)
	}
	planet, err = service.Planet(ctx, principal, 0)
	if err != nil {
		t.Fatal(err)
	}
	if planet.Stock != (domaineconomy.Resources{Metal: 425, Crystal: 485}) {
		t.Fatalf("stock after two debits = %#v", planet.Stock)
	}

	clock.Advance(108 * time.Second)
	completed, err := universe.Events.CompleteDue(ctx, 10)
	if err != nil || completed != 1 {
		t.Fatalf("CompleteDue() = %d, %v", completed, err)
	}
	completed, err = universe.Events.CompleteDue(ctx, 10)
	if err != nil || completed != 0 {
		t.Fatalf("second CompleteDue() = %d, %v", completed, err)
	}
	// Simulate a duplicate delivery after an external crash/recovery decision.
	if _, err := database.Write().ExecContext(ctx, "UPDATE scheduled_events SET state = 'pending', processed_at = NULL WHERE entity_id = ?", fmt.Sprint(queue.ID)); err != nil {
		t.Fatal(err)
	}
	completed, err = universe.Events.CompleteDue(ctx, 10)
	if err != nil || completed != 1 {
		t.Fatalf("redelivered CompleteDue() = %d, %v", completed, err)
	}
	planet, err = service.Planet(ctx, principal, 0)
	if err != nil {
		t.Fatal(err)
	}
	if planet.Levels[building.MetalMine] != 1 || planet.UsedFields != 1 || len(planet.Queue) != 1 {
		t.Fatalf("completed planet = %#v", planet)
	}
	if head := planet.Queue[0]; head.Building != building.SolarPlant || head.State != "active" {
		t.Fatalf("the solar plant did not take over the queue: %#v", head)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM game_event_log WHERE event_type = 'building_completed'", 1)
}

func TestFusionReactorBurnsFuelAcrossAnExistingPlanet(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2042, time.September, 10, 11, 0, 0, 0, time.UTC)
	clock := appclock.NewFake(now)
	database := economyDatabase(t, ctx, 1)
	universe := newWorld(t, database, clock)
	principal := appauth.Principal{AccountID: 1, Username: "captain"}
	planet, err := universe.Economy.CreateEmpire(ctx, principal, "Captain")
	if err != nil {
		t.Fatal(err)
	}
	setBuilding(t, ctx, database, planet.ID, string(building.FusionReactor), 1)
	setResearch(t, ctx, database, 1, "energy_technology", 3)
	setResources(t, ctx, database, planet.ID, 500, 500, 100)

	clock.Advance(time.Hour)
	planet, err = universe.Economy.Planet(ctx, principal, planet.ID)
	if err != nil {
		t.Fatal(err)
	}
	if planet.Stock.Deuterium != 89 || planet.Rates.Deuterium != -11 || planet.Energy.Produced != 32 {
		t.Fatalf("fuelled reactor planet = stock %#v, rates %#v, energy %#v", planet.Stock, planet.Rates, planet.Energy)
	}

	setResources(t, ctx, database, planet.ID, 500, 500, 0)
	planet, err = universe.Economy.Planet(ctx, principal, planet.ID)
	if err != nil {
		t.Fatal(err)
	}
	if planet.Rates.Deuterium != 0 || planet.Energy.Produced != 0 {
		t.Fatalf("empty reactor planet = rates %#v, energy %#v", planet.Rates, planet.Energy)
	}
}

func TestDueBuildingsUseStableEventOrder(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC)
	clock := appclock.NewFake(now)
	database := economyDatabase(t, ctx, 2)
	universe := newWorld(t, database, clock)
	service := universe.Economy
	one, err := service.CreateEmpire(ctx, appauth.Principal{AccountID: 1}, "First Player")
	if err != nil {
		t.Fatal(err)
	}
	two, err := service.CreateEmpire(ctx, appauth.Principal{AccountID: 2}, "Second Player")
	if err != nil {
		t.Fatal(err)
	}
	firstQueue, err := service.EnqueueBuilding(ctx, appauth.Principal{AccountID: 1}, one.ID, building.MetalMine, "first")
	if err != nil {
		t.Fatal(err)
	}
	secondQueue, err := service.EnqueueBuilding(ctx, appauth.Principal{AccountID: 2}, two.ID, building.MetalMine, "second")
	if err != nil {
		t.Fatal(err)
	}
	if firstQueue.CompletesAt != secondQueue.CompletesAt || firstQueue.ID >= secondQueue.ID {
		t.Fatalf("queues do not establish the fixture order: %#v %#v", firstQueue, secondQueue)
	}
	clock.Advance(firstQueue.CompletesAt.Sub(now))
	completed, err := universe.Events.CompleteDue(ctx, 1)
	if err != nil || completed != 1 {
		t.Fatalf("CompleteDue(limit 1) = %d, %v", completed, err)
	}
	var completedID int64
	if err := database.Read().QueryRowContext(ctx, "SELECT id FROM building_queue WHERE state = 'completed'").Scan(&completedID); err != nil {
		t.Fatal(err)
	}
	if completedID != firstQueue.ID {
		t.Fatalf("completed queue = %d, want oldest %d", completedID, firstQueue.ID)
	}
}

func TestEmpirePositionsAndResourceConstraints(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC)
	database := economyDatabase(t, ctx, 2)
	universe := newWorld(t, database, appclock.NewFake(now))
	service := universe.Economy
	one, err := service.CreateEmpire(ctx, appauth.Principal{AccountID: 1}, "Player One")
	if err != nil {
		t.Fatal(err)
	}
	two, err := service.CreateEmpire(ctx, appauth.Principal{AccountID: 2}, "Player Two")
	if err != nil {
		t.Fatal(err)
	}
	if one.Coordinate == two.Coordinate {
		t.Fatalf("duplicate coordinates: %s", one.Coordinate)
	}
	if _, err := database.Write().ExecContext(ctx, "UPDATE planet_resources SET metal = -1 WHERE planet_id = ?", one.ID); err == nil {
		t.Fatal("database accepted negative resources")
	}
	if _, err := database.Write().ExecContext(ctx, `INSERT INTO planets(owner_player_id, name, galaxy, system, position, total_fields, minimum_temperature, maximum_temperature, created_at) SELECT owner_player_id, 'Duplicate', galaxy, system, position, total_fields, minimum_temperature, maximum_temperature, created_at FROM planets WHERE id = ?`, one.ID); err == nil {
		t.Fatal("database accepted a duplicate position")
	}
}

func TestConcurrentBuildingSpendOnlySucceedsOnce(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC)
	database := economyDatabase(t, ctx, 1)
	universe := newWorld(t, database, appclock.NewFake(now))
	service := universe.Economy
	principal := appauth.Principal{AccountID: 1}
	planet, err := service.CreateEmpire(ctx, principal, "Concurrent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Write().ExecContext(ctx, "UPDATE planet_resources SET metal = 75, crystal = 30, deuterium = 0 WHERE planet_id = ?", planet.ID); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	errorsFound := make(chan error, 2)
	var wait sync.WaitGroup
	for index, id := range []building.ID{building.MetalMine, building.SolarPlant} {
		wait.Add(1)
		go func(index int, id building.ID) {
			defer wait.Done()
			<-start
			_, startErr := service.EnqueueBuilding(ctx, principal, planet.ID, id, fmt.Sprintf("concurrent-%d", index))
			errorsFound <- startErr
		}(index, id)
	}
	close(start)
	wait.Wait()
	close(errorsFound)
	successes := 0
	for startErr := range errorsFound {
		if startErr == nil {
			successes++
		} else if !errors.Is(startErr, appeconomy.ErrQueueBusy) && !errors.Is(startErr, domaineconomy.ErrInsufficientResources) {
			t.Fatalf("unexpected concurrent error: %v", startErr)
		}
	}
	if successes != 1 {
		t.Fatalf("successful starts = %d, want 1", successes)
	}
	var metal, crystal int64
	if err := database.Read().QueryRowContext(ctx, "SELECT metal, crystal FROM planet_resources WHERE planet_id = ?", planet.ID).Scan(&metal, &crystal); err != nil {
		t.Fatal(err)
	}
	if metal < 0 || crystal < 0 {
		t.Fatalf("negative stock after concurrency: %d/%d", metal, crystal)
	}
}

func economyDatabase(t *testing.T, ctx context.Context, accountCount int) *storagesqlite.Database {
	t.Helper()
	database := freshDatabase(t, ctx, "economy.db")
	now := "2042-09-10T11:12:13Z"
	for id := 1; id <= accountCount; id++ {
		if _, err := database.Write().ExecContext(ctx, "INSERT INTO accounts(id, username, username_normalized, created_at, updated_at) VALUES (?, ?, ?, ?, ?)", id, fmt.Sprintf("player%d", id), fmt.Sprintf("player%d", id), now, now); err != nil {
			t.Fatal(err)
		}
	}
	document, err := rules.Encode(rules.Default())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Write().ExecContext(ctx, "INSERT INTO ruleset_versions(version, status, document, checksum, author_account_id, effective_at, created_at) VALUES (1, 'active', ?, 'test', 1, ?, ?)", string(document), now, now); err != nil {
		t.Fatal(err)
	}
	return database
}

func TestPlanetsListEveryOwnedBodyAndRejectForeignOnes(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC)
	clock := appclock.NewFake(now)
	database := economyDatabase(t, ctx, 2)
	universe := newWorld(t, database, clock)
	owner := appauth.Principal{AccountID: 1}
	stranger := appauth.Principal{AccountID: 2}

	home, err := universe.Economy.CreateEmpire(ctx, owner, "Alpha")
	if err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	if _, err := universe.Economy.CreateEmpire(ctx, stranger, "Beta"); err != nil {
		t.Fatalf("second CreateEmpire() error = %v", err)
	}
	colony := insertColony(t, ctx, database, 1, "Colonie", 1, 2, 4)

	clock.Advance(time.Hour)
	planets, err := universe.Economy.Planets(ctx, owner)
	if err != nil {
		t.Fatalf("Planets() error = %v", err)
	}
	if len(planets) != 2 {
		t.Fatalf("planets = %d, want 2", len(planets))
	}
	if planets[0].ID != home.ID || planets[1].ID != colony {
		t.Fatalf("planets = %d and %d, want %d then %d", planets[0].ID, planets[1].ID, home.ID, colony)
	}
	if planets[1].Name != "Colonie" || planets[1].Stock.Metal != 530 {
		t.Fatalf("colony projection = %#v", planets[1])
	}

	if _, err := universe.Economy.Planet(ctx, owner, colony); err != nil {
		t.Fatalf("Planet(own colony) error = %v", err)
	}
	if _, err := universe.Economy.Planet(ctx, stranger, colony); !errors.Is(err, appeconomy.ErrPlanetNotFound) {
		t.Fatalf("Planet(foreign planet) error = %v, want ErrPlanetNotFound", err)
	}
	if _, _, err := universe.Economy.Buildings(ctx, stranger, colony); !errors.Is(err, appeconomy.ErrPlanetNotFound) {
		t.Fatalf("Buildings(foreign planet) error = %v, want ErrPlanetNotFound", err)
	}
}

func TestPlanetNamesAreValidatedAndOwned(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC)
	database := economyDatabase(t, ctx, 2)
	universe := newWorld(t, database, appclock.NewFake(now))
	owner := appauth.Principal{AccountID: 1}
	stranger := appauth.Principal{AccountID: 2}
	home, err := universe.Economy.CreateEmpire(ctx, owner, "Alpha")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := universe.Economy.CreateEmpire(ctx, stranger, "Beta"); err != nil {
		t.Fatal(err)
	}

	if err := universe.Economy.RenamePlanet(ctx, owner, home.ID, "  Nouvelle Terre  "); err != nil {
		t.Fatalf("RenamePlanet() error = %v", err)
	}
	renamed, err := universe.Economy.Planet(ctx, owner, home.ID)
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Name != "Nouvelle Terre" {
		t.Fatalf("renamed planet = %q", renamed.Name)
	}
	for _, invalid := range []string{"   ", strings.Repeat("é", 33)} {
		if err := universe.Economy.RenamePlanet(ctx, owner, home.ID, invalid); !errors.Is(err, appeconomy.ErrInvalidPlanetName) {
			t.Fatalf("RenamePlanet(%q) error = %v, want ErrInvalidPlanetName", invalid, err)
		}
	}
	if err := universe.Economy.RenamePlanet(ctx, stranger, home.ID, "Volée"); !errors.Is(err, appeconomy.ErrPlanetNotFound) {
		t.Fatalf("RenamePlanet(foreign) error = %v, want ErrPlanetNotFound", err)
	}
	assertSingleText(t, database, "SELECT name FROM planets WHERE id = ?", "Nouvelle Terre", home.ID)
}

func insertColony(t *testing.T, ctx context.Context, database *storagesqlite.Database, playerID int64, name string, galaxy, system, position int) int64 {
	t.Helper()
	result, err := database.Write().ExecContext(ctx, `
		INSERT INTO planets(owner_player_id, name, galaxy, system, position, total_fields, minimum_temperature, maximum_temperature, created_at)
		VALUES (?, ?, ?, ?, ?, 190, 10, 50, '2042-09-10T11:12:13Z')
	`, playerID, name, galaxy, system, position)
	if err != nil {
		t.Fatalf("insert colony: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("colony id: %v", err)
	}
	if _, err := database.Write().ExecContext(ctx, "INSERT INTO planet_resources(planet_id, produced_at) VALUES (?, '2042-09-10T11:12:13Z')", id); err != nil {
		t.Fatalf("insert colony resources: %v", err)
	}
	return id
}
