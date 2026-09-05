package tests

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
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
	service := appeconomy.Service{Clock: clock, Repository: storagesqlite.NewEconomyRepository(database.Write()), Catalogue: building.DefaultCatalogue()}
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
	planet, err = service.Planet(ctx, principal)
	if err != nil {
		t.Fatalf("Planet() error = %v", err)
	}
	if planet.Stock != (domaineconomy.Resources{Metal: 560, Crystal: 530}) {
		t.Fatalf("stock after two hours = %#v", planet.Stock)
	}

	queue, err := service.StartConstruction(ctx, principal, planet.ID, building.MetalMine, "build-1")
	if err != nil {
		t.Fatalf("StartConstruction() error = %v", err)
	}
	if queue.Cost != (domaineconomy.Resources{Metal: 60, Crystal: 15}) || queue.CompletesAt.Sub(queue.StartedAt) != 108*time.Second {
		t.Fatalf("queue = %#v", queue)
	}
	replayed, err := service.StartConstruction(ctx, principal, planet.ID, building.MetalMine, "build-1")
	if err != nil || replayed.ID != queue.ID {
		t.Fatalf("idempotent replay = %#v, %v", replayed, err)
	}
	if _, err := service.StartConstruction(ctx, principal, planet.ID, building.SolarPlant, "build-2"); !errors.Is(err, appeconomy.ErrQueueBusy) {
		t.Fatalf("parallel queue error = %v", err)
	}
	planet, err = service.Planet(ctx, principal)
	if err != nil {
		t.Fatal(err)
	}
	if planet.Stock != (domaineconomy.Resources{Metal: 500, Crystal: 515}) {
		t.Fatalf("stock after one debit = %#v", planet.Stock)
	}

	clock.Advance(108 * time.Second)
	completed, err := service.CompleteDue(ctx, 10)
	if err != nil || completed != 1 {
		t.Fatalf("CompleteDue() = %d, %v", completed, err)
	}
	completed, err = service.CompleteDue(ctx, 10)
	if err != nil || completed != 0 {
		t.Fatalf("second CompleteDue() = %d, %v", completed, err)
	}
	// Simulate a duplicate delivery after an external crash/recovery decision.
	if _, err := database.Write().ExecContext(ctx, "UPDATE scheduled_events SET state = 'pending', processed_at = NULL WHERE entity_id = ?", fmt.Sprint(queue.ID)); err != nil {
		t.Fatal(err)
	}
	completed, err = service.CompleteDue(ctx, 10)
	if err != nil || completed != 1 {
		t.Fatalf("redelivered CompleteDue() = %d, %v", completed, err)
	}
	planet, err = service.Planet(ctx, principal)
	if err != nil {
		t.Fatal(err)
	}
	if planet.Levels[building.MetalMine] != 1 || planet.UsedFields != 1 || planet.ActiveQueue != nil {
		t.Fatalf("completed planet = %#v", planet)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM game_event_log WHERE event_type = 'building_completed'", 1)
}

func TestDueBuildingsUseStableEventOrder(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC)
	clock := appclock.NewFake(now)
	database := economyDatabase(t, ctx, 2)
	service := appeconomy.Service{Clock: clock, Repository: storagesqlite.NewEconomyRepository(database.Write()), Catalogue: building.DefaultCatalogue()}
	one, err := service.CreateEmpire(ctx, appauth.Principal{AccountID: 1}, "First Player")
	if err != nil {
		t.Fatal(err)
	}
	two, err := service.CreateEmpire(ctx, appauth.Principal{AccountID: 2}, "Second Player")
	if err != nil {
		t.Fatal(err)
	}
	firstQueue, err := service.StartConstruction(ctx, appauth.Principal{AccountID: 1}, one.ID, building.MetalMine, "first")
	if err != nil {
		t.Fatal(err)
	}
	secondQueue, err := service.StartConstruction(ctx, appauth.Principal{AccountID: 2}, two.ID, building.MetalMine, "second")
	if err != nil {
		t.Fatal(err)
	}
	if firstQueue.CompletesAt != secondQueue.CompletesAt || firstQueue.ID >= secondQueue.ID {
		t.Fatalf("queues do not establish the fixture order: %#v %#v", firstQueue, secondQueue)
	}
	clock.Advance(firstQueue.CompletesAt.Sub(now))
	completed, err := service.CompleteDue(ctx, 1)
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
	service := appeconomy.Service{Clock: appclock.NewFake(now), Repository: storagesqlite.NewEconomyRepository(database.Write()), Catalogue: building.DefaultCatalogue()}
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
	repository := storagesqlite.NewEconomyRepository(database.Write())
	service := appeconomy.Service{Clock: appclock.NewFake(now), Repository: repository, Catalogue: building.DefaultCatalogue()}
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
			_, startErr := service.StartConstruction(ctx, principal, planet.ID, id, fmt.Sprintf("concurrent-%d", index))
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
	database, err := storagesqlite.Open(ctx, filepath.Join(t.TempDir(), "economy.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
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
