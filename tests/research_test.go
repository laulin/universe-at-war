package tests

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appeconomy "universeatwar/internal/app/economy"
	appresearch "universeatwar/internal/app/research"
	appclock "universeatwar/internal/clock"
	domaineconomy "universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/research"
	storagesqlite "universeatwar/internal/storage/sqlite"
)

func TestResearchStartsCompletesOnceAndRefusesConcurrentQueues(t *testing.T) {
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
	setResources(t, ctx, database, planet.ID, 5000, 5000, 5000)

	queue, err := universe.Research.EnqueueResearch(ctx, principal, planet.ID, research.EnergyTechnology, "key-1")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if queue.TargetLevel != 1 || queue.CompletesAt.Sub(queue.StartedAt) != 1440*time.Second {
		t.Fatalf("queue = %+v", queue)
	}
	replay, err := universe.Research.EnqueueResearch(ctx, principal, planet.ID, research.EnergyTechnology, "key-1")
	if err != nil || replay.ID != queue.ID {
		t.Fatalf("idempotent replay = %+v %v", replay, err)
	}
	assertSingleValue(t, database, "SELECT crystal FROM planet_resources WHERE planet_id = 1", 4200)
	assertSingleValue(t, database, "SELECT deuterium FROM planet_resources WHERE planet_id = 1", 4600)
	// A second research no longer collides with the first: it waits behind it,
	// and pays on the spot all the same.
	behind, err := universe.Research.EnqueueResearch(ctx, principal, planet.ID, research.ComputerTechnology, "key-2")
	if err != nil || behind.Position != 1 || behind.State != "queued" {
		t.Fatalf("second research = %+v, %v", behind, err)
	}
	assertSingleValue(t, database, "SELECT crystal FROM planet_resources WHERE planet_id = 1", int(4200-behind.Cost.Crystal))
	assertSingleValue(t, database, "SELECT deuterium FROM planet_resources WHERE planet_id = 1", int(4600-behind.Cost.Deuterium))
	assertSingleValue(t, database, "SELECT COUNT(*) FROM scheduled_events WHERE event_type = 'research_completed' AND state = 'pending'", 1)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM game_event_log WHERE event_type = 'research_started'", 1)

	clock.Advance(1440 * time.Second)
	if _, err := universe.Events.CompleteDue(ctx, 10); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}
	assertSingleValue(t, database, "SELECT level FROM player_research WHERE player_id = 1 AND research_id = 'energy_technology'", 1)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM game_event_log WHERE event_type = 'research_completed'", 1)

	replayEvent(t, ctx, database, "research_completed")
	if _, err := universe.Events.CompleteDue(ctx, 10); err != nil {
		t.Fatalf("CompleteDue() after redelivery error = %v", err)
	}
	assertSingleValue(t, database, "SELECT level FROM player_research WHERE player_id = 1 AND research_id = 'energy_technology'", 1)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM game_event_log WHERE event_type = 'research_completed'", 1)

	overview, err := universe.Research.Overview(ctx, principal, planet.ID)
	if err != nil {
		t.Fatalf("Overview() error = %v", err)
	}
	if overview.Levels[research.EnergyTechnology] != 1 || len(overview.Queue) != 1 {
		t.Fatalf("overview = %+v", overview)
	}
	if head := overview.Queue[0]; head.Research != research.ComputerTechnology || head.State != "active" {
		t.Fatalf("the computer technology did not take over the laboratory: %+v", head)
	}
	if len(overview.Choices) == 0 {
		t.Fatal("overview lists no research")
	}
	for _, choice := range overview.Choices {
		if choice.Definition.ID == research.EnergyTechnology && choice.Plan.TargetLevel != 2 {
			t.Fatalf("energy choice = %+v", choice)
		}
		if choice.Definition.ID == research.IonTechnology && choice.Available {
			t.Fatalf("ion technology is available without its prerequisites: %+v", choice)
		}
	}
}

func TestResearchNetworkUsesTheBestRemoteLaboratories(t *testing.T) {
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC))
	database := economyDatabase(t, ctx, 1)
	universe := newWorld(t, database, clock)
	principal := appauth.Principal{AccountID: 1}

	planet, err := universe.Economy.CreateEmpire(ctx, principal, "Captain")
	if err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	colony := insertColony(t, ctx, database, 1, "Colonie", 1, 2, 4)
	setBuilding(t, ctx, database, planet.ID, "research_lab", 1)
	setBuilding(t, ctx, database, colony, "research_lab", 3)
	setResearch(t, ctx, database, 1, "intergalactic_research_network", 1)
	setResources(t, ctx, database, planet.ID, 5000, 5000, 5000)

	queue, err := universe.Research.EnqueueResearch(ctx, principal, planet.ID, research.EnergyTechnology, "network")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if got := queue.CompletesAt.Sub(queue.StartedAt); got != 576*time.Second {
		t.Fatalf("duration with the research network = %v, want 576s", got)
	}
}

func TestGravitonNeedsAvailableEnergyAndCostsNoResources(t *testing.T) {
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC))
	database := economyDatabase(t, ctx, 1)
	universe := newWorld(t, database, clock)
	principal := appauth.Principal{AccountID: 1}

	planet, err := universe.Economy.CreateEmpire(ctx, principal, "Captain")
	if err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	setBuilding(t, ctx, database, planet.ID, "research_lab", 12)
	setResources(t, ctx, database, planet.ID, 1000, 1000, 1000)

	if _, err := universe.Research.EnqueueResearch(ctx, principal, planet.ID, research.GravitonTechnology, "graviton-1"); !errors.Is(err, appresearch.ErrInsufficientEnergy) {
		t.Fatalf("Start() without energy error = %v, want ErrInsufficientEnergy", err)
	}
	setUnits(t, ctx, database, planet.ID, "solar_satellite", 10000)

	queue, err := universe.Research.EnqueueResearch(ctx, principal, planet.ID, research.GravitonTechnology, "graviton-2")
	if err != nil {
		t.Fatalf("Start() with satellites error = %v", err)
	}
	if queue.CompletesAt.Sub(queue.StartedAt) != time.Second || queue.Cost != (domaineconomy.Resources{}) || queue.Energy != 300_000 {
		t.Fatalf("graviton queue = %+v", queue)
	}
	assertSingleValue(t, database, "SELECT metal FROM planet_resources WHERE planet_id = 1", 1000)
}

func TestConcurrentResearchStartsSpendOnlyOnce(t *testing.T) {
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
	setResources(t, ctx, database, planet.ID, 0, 800, 600)

	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for index, id := range []research.ID{research.EnergyTechnology, research.ComputerTechnology} {
		wait.Add(1)
		go func(index int, id research.ID) {
			defer wait.Done()
			<-start
			_, err := universe.Research.EnqueueResearch(ctx, principal, planet.ID, id, fmt.Sprintf("concurrent-%d", index))
			results <- err
		}(index, id)
	}
	close(start)
	wait.Wait()
	close(results)

	successes := 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, appresearch.ErrQueueBusy), errors.Is(err, domaineconomy.ErrInsufficientResources):
		default:
			t.Fatalf("unexpected concurrent error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful starts = %d, want 1", successes)
	}
	var crystal, deuterium int64
	if err := database.Read().QueryRowContext(ctx, "SELECT crystal, deuterium FROM planet_resources WHERE planet_id = 1").Scan(&crystal, &deuterium); err != nil {
		t.Fatal(err)
	}
	if crystal < 0 || deuterium < 0 {
		t.Fatalf("negative stock after concurrency: %d/%d", crystal, deuterium)
	}
}

func TestResearchAndLaboratoryUpgradeExcludeEachOther(t *testing.T) {
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
	setResources(t, ctx, database, planet.ID, 50000, 50000, 50000)

	if _, err := universe.Research.EnqueueResearch(ctx, principal, planet.ID, research.EnergyTechnology, "exclusive-1"); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, err := universe.Economy.EnqueueBuilding(ctx, principal, planet.ID, "research_lab", "build-lab"); !errors.Is(err, appeconomy.ErrFacilityBusy) {
		t.Fatalf("laboratory upgrade during research error = %v, want ErrFacilityBusy", err)
	}
	if _, err := universe.Economy.EnqueueBuilding(ctx, principal, planet.ID, "metal_mine", "build-mine"); err != nil {
		t.Fatalf("other construction during research error = %v", err)
	}
}

func replayEvent(t *testing.T, ctx context.Context, database *storagesqlite.Database, eventType string) {
	t.Helper()
	if _, err := database.Write().ExecContext(ctx,
		"UPDATE scheduled_events SET state = 'pending', processed_at = NULL WHERE event_type = ?", eventType); err != nil {
		t.Fatalf("replay %s: %v", eventType, err)
	}
}
