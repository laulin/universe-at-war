package tests

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appeconomy "universeatwar/internal/app/economy"
	appresearch "universeatwar/internal/app/research"
	appclock "universeatwar/internal/clock"
	"universeatwar/internal/domain/building"
	domaineconomy "universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/research"
	"universeatwar/internal/domain/unit"
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
	// Stores large enough that settling never clips the fortune below, so a
	// test watches the queue and nothing else.
	for _, store := range []string{"metal_storage", "crystal_storage", "deuterium_tank"} {
		setBuilding(t, ctx, database, planet.ID, store, 10)
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

// researchingEmpire founds an empire with a laboratory and enough of everything
// to fill the research queue.
func researchingEmpire(t *testing.T, ctx context.Context) (*storagesqlite.Database, *world, *appclock.Fake, appauth.Principal, appeconomy.Planet) {
	t.Helper()
	database, universeWorld, clock, principal, planet := queuedEmpire(t, ctx)
	setBuilding(t, ctx, database, planet.ID, "research_lab", 4)
	setBuilding(t, ctx, database, planet.ID, "solar_plant", 20)
	planet, err := universeWorld.Economy.Planet(ctx, principal, planet.ID)
	if err != nil {
		t.Fatal(err)
	}
	return database, universeWorld, clock, principal, planet
}

func TestQueueingSeveralResearchLevelsPaysForEachAtOrderTime(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, clock, principal, planet := researchingEmpire(t, ctx)
	before := planet.Stock

	spent := domaineconomy.Resources{}
	for index := range 3 {
		entry, err := universeWorld.Research.EnqueueResearch(ctx, principal, planet.ID, research.EnergyTechnology, fmt.Sprintf("energy-%d", index))
		if err != nil {
			t.Fatalf("EnqueueResearch(%d) error = %v", index, err)
		}
		if entry.TargetLevel != index+1 || entry.Position != index {
			t.Fatalf("entry %d = %#v", index, entry)
		}
		spent = domaineconomy.Resources{
			Metal:     spent.Metal + entry.Cost.Metal,
			Crystal:   spent.Crystal + entry.Cost.Crystal,
			Deuterium: spent.Deuterium + entry.Cost.Deuterium,
		}
	}

	overview, err := universeWorld.Research.Overview(ctx, principal, planet.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(overview.Queue) != 3 {
		t.Fatalf("research queue = %#v", overview.Queue)
	}
	want := domaineconomy.Resources{
		Metal: before.Metal - spent.Metal, Crystal: before.Crystal - spent.Crystal,
		Deuterium: before.Deuterium - spent.Deuterium,
	}
	if overview.Planet.Stock != want {
		t.Fatalf("stock after three orders = %#v, want %#v", overview.Planet.Stock, want)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM scheduled_events WHERE event_type = 'research_completed' AND state = 'pending'", 1)

	advanceUntilIdle(t, ctx, universeWorld, clock)
	assertSingleValue(t, database, "SELECT level FROM player_research WHERE player_id = 1 AND research_id = 'energy_technology'", 3)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM research_queue WHERE state = 'completed'", 3)
}

// The laboratory cannot be upgraded while a research is anywhere in the queue,
// and a research cannot be ordered while the laboratory is anywhere in the
// building queue. Looking at whole queues keeps that exclusion true at every
// instant instead of only while an entry runs.
func TestTheLaboratoryExclusionCoversWholeQueues(t *testing.T) {
	ctx := context.Background()
	_, universeWorld, _, principal, planet := researchingEmpire(t, ctx)

	// A research waiting behind another still blocks the laboratory.
	for index := range 2 {
		if _, err := universeWorld.Research.EnqueueResearch(ctx, principal, planet.ID, research.EnergyTechnology, fmt.Sprintf("energy-%d", index)); err != nil {
			t.Fatalf("EnqueueResearch(%d) error = %v", index, err)
		}
	}
	if _, err := universeWorld.Economy.EnqueueBuilding(ctx, principal, planet.ID, building.ResearchLab, "lab"); !errors.Is(err, appeconomy.ErrFacilityBusy) {
		t.Fatalf("laboratory upgrade during a research queue = %v", err)
	}

	_, otherWorld, _, otherPrincipal, otherPlanet := researchingEmpire(t, ctx)
	// A laboratory waiting behind another building still blocks research.
	if _, err := otherWorld.Economy.EnqueueBuilding(ctx, otherPrincipal, otherPlanet.ID, building.MetalMine, "mine"); err != nil {
		t.Fatalf("EnqueueBuilding(mine) error = %v", err)
	}
	if _, err := otherWorld.Economy.EnqueueBuilding(ctx, otherPrincipal, otherPlanet.ID, building.ResearchLab, "lab"); err != nil {
		t.Fatalf("EnqueueBuilding(lab) error = %v", err)
	}
	if _, err := otherWorld.Research.EnqueueResearch(ctx, otherPrincipal, otherPlanet.ID, research.EnergyTechnology, "energy"); !errors.Is(err, appresearch.ErrLaboratoryBusy) {
		t.Fatalf("research during a queued laboratory = %v", err)
	}
}

// producingEmpire founds an empire with a shipyard and the research a light
// fighter needs.
func producingEmpire(t *testing.T, ctx context.Context) (*storagesqlite.Database, *world, *appclock.Fake, appauth.Principal, appeconomy.Planet) {
	t.Helper()
	database, universeWorld, clock, principal, planet := queuedEmpire(t, ctx)
	setBuilding(t, ctx, database, planet.ID, "shipyard", 2)
	setResearch(t, ctx, database, 1, "combustion_drive", 1)
	planet, err := universeWorld.Economy.Planet(ctx, principal, planet.ID)
	if err != nil {
		t.Fatal(err)
	}
	return database, universeWorld, clock, principal, planet
}

// The yard and the defences hold one queue each and advance side by side, so a
// batch of fighters never holds a rocket launcher back.
func TestShipsAndDefencesProgressInTwoIndependentQueues(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, _, principal, planet := producingEmpire(t, ctx)

	if _, err := universeWorld.Shipyard.OrderFamily(ctx, principal, planet.ID, unit.LightFighter, unit.Ship, 3, "fighters"); err != nil {
		t.Fatalf("OrderFamily(ships) error = %v", err)
	}
	if _, err := universeWorld.Shipyard.OrderFamily(ctx, principal, planet.ID, unit.RocketLauncher, unit.Defense, 4, "launchers"); err != nil {
		t.Fatalf("OrderFamily(defences) error = %v", err)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM production_orders WHERE planet_id = ? AND state = 'active'", 2, planet.ID)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM scheduled_events WHERE event_type = 'production_completed' AND state = 'pending'", 2)

	ships, err := universeWorld.Shipyard.Ships(ctx, principal, planet.ID)
	if err != nil {
		t.Fatal(err)
	}
	defenses, err := universeWorld.Shipyard.Defenses(ctx, principal, planet.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ships.Queue) != 1 || ships.Queue[0].Unit != unit.LightFighter {
		t.Fatalf("ship queue = %+v", ships.Queue)
	}
	if len(defenses.Queue) != 1 || defenses.Queue[0].Unit != unit.RocketLauncher {
		t.Fatalf("defence queue = %+v", defenses.Queue)
	}
}

func TestQueueingTwoBatchesPaysForBothAndBuildsThemInOrder(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, clock, principal, planet := producingEmpire(t, ctx)
	before := planet.Stock

	first, err := universeWorld.Shipyard.OrderFamily(ctx, principal, planet.ID, unit.LightFighter, unit.Ship, 3, "batch-1")
	if err != nil {
		t.Fatalf("OrderFamily(first) error = %v", err)
	}
	second, err := universeWorld.Shipyard.OrderFamily(ctx, principal, planet.ID, unit.LightFighter, unit.Ship, 2, "batch-2")
	if err != nil {
		t.Fatalf("OrderFamily(second) error = %v", err)
	}
	if second.Position != 1 || second.State != "queued" {
		t.Fatalf("second batch = %+v", second)
	}

	ships, err := universeWorld.Shipyard.Ships(ctx, principal, planet.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := domaineconomy.Resources{
		Metal:     before.Metal - first.TotalCost.Metal - second.TotalCost.Metal,
		Crystal:   before.Crystal - first.TotalCost.Crystal - second.TotalCost.Crystal,
		Deuterium: before.Deuterium - first.TotalCost.Deuterium - second.TotalCost.Deuterium,
	}
	if ships.Planet.Stock != want {
		t.Fatalf("stock after two batches = %#v, want %#v", ships.Planet.Stock, want)
	}
	// A batch waiting its turn delivers nothing yet.
	assertSingleValue(t, database, "SELECT COUNT(*) FROM production_orders WHERE state = 'queued' AND delivered = 0", 1)

	advanceUntilIdle(t, ctx, universeWorld, clock)
	assertSingleValue(t, database, "SELECT quantity FROM planet_units WHERE planet_id = 1 AND unit_id = 'light_fighter'", 5)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM production_orders WHERE state = 'completed'", 2)
}

// A queued batch counts towards the limits a unit carries, so a second shield
// dome cannot slip in behind the first.
func TestAQueuedBatchCountsTowardsTheUnitLimits(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, _, principal, planet := producingEmpire(t, ctx)
	setResearch(t, ctx, database, 1, "shielding_technology", 2)
	setResearch(t, ctx, database, 1, "energy_technology", 3)

	if _, err := universeWorld.Shipyard.OrderFamily(ctx, principal, planet.ID, unit.SmallShieldDome, unit.Defense, 1, "dome-1"); err != nil {
		t.Fatalf("OrderFamily(dome) error = %v", err)
	}
	if _, err := universeWorld.Shipyard.OrderFamily(ctx, principal, planet.ID, unit.SmallShieldDome, unit.Defense, 1, "dome-2"); !errors.Is(err, unit.ErrQuantityLimit) {
		t.Fatalf("second shield dome error = %v, want ErrQuantityLimit", err)
	}
}
