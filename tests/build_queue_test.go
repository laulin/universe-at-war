package tests

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appeconomy "universeatwar/internal/app/economy"
	appclock "universeatwar/internal/clock"
	"universeatwar/internal/domain/building"
	domaineconomy "universeatwar/internal/domain/economy"
	storagesqlite "universeatwar/internal/storage/sqlite"
)

// queuedEmpire founds an empire rich enough to fill a queue without ever
// running out of resources, so a test can watch the queue and nothing else.
func queuedEmpire(t *testing.T, ctx context.Context) (*storagesqlite.Database, *world, *appclock.Fake, appauth.Principal, appeconomy.Planet) {
	t.Helper()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC))
	database := economyDatabase(t, ctx, 1)
	universeWorld := newWorld(t, database, clock)
	principal := appauth.Principal{AccountID: 1, Username: "captain"}
	planet, err := universeWorld.Economy.CreateEmpire(ctx, principal, "Captain")
	if err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	setResources(t, ctx, database, planet.ID, 5_000_000, 5_000_000, 5_000_000)
	planet, err = universeWorld.Economy.Planet(ctx, principal, planet.ID)
	if err != nil {
		t.Fatalf("Planet() error = %v", err)
	}
	return database, universeWorld, clock, principal, planet
}

func TestQueueingSeveralMinesDebitsEachCostAtOrderTime(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, _, principal, planet := queuedEmpire(t, ctx)
	before := planet.Stock

	spent := domaineconomy.Resources{}
	for index := range 3 {
		entry, err := universeWorld.Economy.EnqueueBuilding(ctx, principal, planet.ID, building.MetalMine, fmt.Sprintf("mine-%d", index))
		if err != nil {
			t.Fatalf("EnqueueBuilding(%d) error = %v", index, err)
		}
		if entry.TargetLevel != index+1 || entry.Position != index {
			t.Fatalf("entry %d = %#v", index, entry)
		}
		if (index == 0) != (entry.State == "active") {
			t.Fatalf("entry %d state = %q", index, entry.State)
		}
		spent = domaineconomy.Resources{
			Metal:     spent.Metal + entry.Cost.Metal,
			Crystal:   spent.Crystal + entry.Cost.Crystal,
			Deuterium: spent.Deuterium + entry.Cost.Deuterium,
		}
	}

	planet, err := universeWorld.Economy.Planet(ctx, principal, planet.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := domaineconomy.Resources{
		Metal: before.Metal - spent.Metal, Crystal: before.Crystal - spent.Crystal,
		Deuterium: before.Deuterium - spent.Deuterium,
	}
	if planet.Stock != want {
		t.Fatalf("stock after three orders = %#v, want %#v", planet.Stock, want)
	}
	if len(planet.Queue) != 3 {
		t.Fatalf("queue = %#v", planet.Queue)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM building_queue WHERE planet_id = ? AND state = 'active'", 1, planet.ID)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM building_queue WHERE planet_id = ? AND state = 'queued'", 2, planet.ID)
	// Only the head is scheduled: a waiting entry has no deadline to honour.
	assertSingleValue(t, database, "SELECT COUNT(*) FROM scheduled_events WHERE event_type = 'building_completed' AND state = 'pending'", 1)
}

func TestQueuedConstructionsStartOneAfterTheOtherWithoutGaps(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, clock, principal, planet := queuedEmpire(t, ctx)

	for index := range 3 {
		if _, err := universeWorld.Economy.EnqueueBuilding(ctx, principal, planet.ID, building.MetalMine, fmt.Sprintf("mine-%d", index)); err != nil {
			t.Fatalf("EnqueueBuilding(%d) error = %v", index, err)
		}
	}
	planet, err := universeWorld.Economy.Planet(ctx, principal, planet.ID)
	if err != nil {
		t.Fatal(err)
	}
	head := planet.Queue[0]

	setClock(t, clock, head.CompletesAt)
	if _, err := universeWorld.Events.CompleteDue(ctx, 10); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}

	planet, err = universeWorld.Economy.Planet(ctx, principal, planet.ID)
	if err != nil {
		t.Fatal(err)
	}
	if planet.Levels[building.MetalMine] != 1 {
		t.Fatalf("mine level after the first completion = %d", planet.Levels[building.MetalMine])
	}
	if len(planet.Queue) != 2 {
		t.Fatalf("queue after the first completion = %#v", planet.Queue)
	}
	promoted := planet.Queue[0]
	if promoted.State != "active" || promoted.TargetLevel != 2 {
		t.Fatalf("promoted entry = %#v", promoted)
	}
	if !promoted.StartedAt.Equal(head.CompletesAt) {
		t.Fatalf("promoted started at %s, want %s", promoted.StartedAt, head.CompletesAt)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM scheduled_events WHERE event_type = 'building_completed' AND state = 'pending'", 1)

	advanceUntilIdle(t, ctx, universeWorld, clock)
	planet, err = universeWorld.Economy.Planet(ctx, principal, planet.ID)
	if err != nil {
		t.Fatal(err)
	}
	if planet.Levels[building.MetalMine] != 3 || len(planet.Queue) != 0 {
		t.Fatalf("mine level = %d, queue = %#v", planet.Levels[building.MetalMine], planet.Queue)
	}
}

func TestAnOrderBeyondTheQueueLengthIsRefusedWithoutSpending(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, _, principal, planet := queuedEmpire(t, ctx)
	capacity := planet.Rules.Progression.QueueLength

	for index := range capacity {
		if _, err := universeWorld.Economy.EnqueueBuilding(ctx, principal, planet.ID, building.MetalMine, fmt.Sprintf("mine-%d", index)); err != nil {
			t.Fatalf("EnqueueBuilding(%d) error = %v", index, err)
		}
	}
	planet, err := universeWorld.Economy.Planet(ctx, principal, planet.ID)
	if err != nil {
		t.Fatal(err)
	}
	full := planet.Stock

	if _, err := universeWorld.Economy.EnqueueBuilding(ctx, principal, planet.ID, building.MetalMine, "one-too-many"); !errors.Is(err, appeconomy.ErrQueueFull) {
		t.Fatalf("order beyond the queue length error = %v", err)
	}
	planet, err = universeWorld.Economy.Planet(ctx, principal, planet.ID)
	if err != nil {
		t.Fatal(err)
	}
	if planet.Stock != full {
		t.Fatalf("a refused order spent %#v", domaineconomy.Resources{
			Metal: full.Metal - planet.Stock.Metal, Crystal: full.Crystal - planet.Stock.Crystal,
			Deuterium: full.Deuterium - planet.Stock.Deuterium,
		})
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM building_queue WHERE planet_id = ? AND state IN ('active', 'queued')", capacity, planet.ID)
}

func TestQueuedBuildingsReserveThePlanetFields(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, _, principal, planet := queuedEmpire(t, ctx)
	// Leave room for exactly two more buildings on the body.
	if _, err := database.Write().ExecContext(ctx,
		"UPDATE planets SET used_fields = total_fields - 2 WHERE id = ?", planet.ID); err != nil {
		t.Fatal(err)
	}

	for index := range 2 {
		if _, err := universeWorld.Economy.EnqueueBuilding(ctx, principal, planet.ID, building.MetalMine, fmt.Sprintf("mine-%d", index)); err != nil {
			t.Fatalf("EnqueueBuilding(%d) error = %v", index, err)
		}
	}
	// A third order would consume a field the body does not have, even though
	// none of the queued levels has been built yet.
	if _, err := universeWorld.Economy.EnqueueBuilding(ctx, principal, planet.ID, building.MetalMine, "mine-2"); err == nil {
		t.Fatal("a queue was allowed to book more fields than the body owns")
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM building_queue WHERE planet_id = ? AND state IN ('active', 'queued')", 2, planet.ID)
}

func TestAPromotedConstructionUsesTheFactoryLevelsOfItsOwnStart(t *testing.T) {
	ctx := context.Background()
	_, universeWorld, clock, principal, planet := queuedEmpire(t, ctx)

	// A robotics factory in front of a mine must shorten that mine, because the
	// duration of a waiting entry is only decided when it starts.
	if _, err := universeWorld.Economy.EnqueueBuilding(ctx, principal, planet.ID, building.RoboticsFactory, "robotics"); err != nil {
		t.Fatalf("EnqueueBuilding(robotics) error = %v", err)
	}
	if _, err := universeWorld.Economy.EnqueueBuilding(ctx, principal, planet.ID, building.MetalMine, "mine"); err != nil {
		t.Fatalf("EnqueueBuilding(mine) error = %v", err)
	}

	planet, err := universeWorld.Economy.Planet(ctx, principal, planet.ID)
	if err != nil {
		t.Fatal(err)
	}
	mine := planet.Queue[1]
	unhelped, err := universeWorld.Economy.Catalogue.Duration(mine.Cost, 0, 0, planet.Rules.Time.BuildingSpeed)
	if err != nil {
		t.Fatal(err)
	}
	helped, err := universeWorld.Economy.Catalogue.Duration(mine.Cost, 1, 0, planet.Rules.Time.BuildingSpeed)
	if err != nil {
		t.Fatal(err)
	}
	if helped >= unhelped {
		t.Fatalf("the fixture proves nothing: %s is not shorter than %s", helped, unhelped)
	}

	setClock(t, clock, planet.Queue[0].CompletesAt)
	if _, err := universeWorld.Events.CompleteDue(ctx, 10); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}
	planet, err = universeWorld.Economy.Planet(ctx, principal, planet.ID)
	if err != nil {
		t.Fatal(err)
	}
	promoted := planet.Queue[0]
	if got := promoted.CompletesAt.Sub(promoted.StartedAt); got != helped {
		t.Fatalf("promoted mine takes %s, want %s", got, helped)
	}
}
