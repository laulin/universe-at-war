package tests

import (
	"context"
	"errors"
	"fmt"
	"testing"

	appeconomy "universeatwar/internal/app/economy"
	"universeatwar/internal/domain/building"
	domaineconomy "universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/research"
	"universeatwar/internal/domain/unit"
)

// Cancelling gives everything back. The player already lost the time; taking
// the resources too would only make queues a trap.
func TestCancellingAWaitingConstructionRefundsItInFull(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, _, principal, planet := queuedEmpire(t, ctx)

	if _, err := universeWorld.Economy.EnqueueBuilding(ctx, principal, planet.ID, building.MetalMine, "mine-0"); err != nil {
		t.Fatal(err)
	}
	waiting, err := universeWorld.Economy.EnqueueBuilding(ctx, principal, planet.ID, building.SolarPlant, "plant")
	if err != nil {
		t.Fatal(err)
	}
	before, err := universeWorld.Economy.Planet(ctx, principal, planet.ID)
	if err != nil {
		t.Fatal(err)
	}

	cancellation, err := universeWorld.Economy.CancelBuilding(ctx, principal, planet.ID, waiting.ID)
	if err != nil {
		t.Fatalf("CancelBuilding() error = %v", err)
	}
	if cancellation.Cancelled != 1 || cancellation.Refunded != waiting.Cost || cancellation.Lost != (domaineconomy.Resources{}) {
		t.Fatalf("cancellation = %#v, order cost %#v", cancellation, waiting.Cost)
	}

	after, err := universeWorld.Economy.Planet(ctx, principal, planet.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := domaineconomy.Resources{
		Metal: before.Stock.Metal + waiting.Cost.Metal, Crystal: before.Stock.Crystal + waiting.Cost.Crystal,
		Deuterium: before.Stock.Deuterium + waiting.Cost.Deuterium,
	}
	if after.Stock != want {
		t.Fatalf("stock after the refund = %#v, want %#v", after.Stock, want)
	}
	if len(after.Queue) != 1 || after.Queue[0].Building != building.MetalMine {
		t.Fatalf("queue after the cancellation = %#v", after.Queue)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM building_queue WHERE state = 'cancelled'", 1)

	// The same order cannot be cancelled twice, so no refund is ever doubled.
	if _, err := universeWorld.Economy.CancelBuilding(ctx, principal, planet.ID, waiting.ID); !errors.Is(err, appeconomy.ErrQueueEntryNotFound) {
		t.Fatalf("second cancellation = %v", err)
	}
}

func TestCancellingTheRunningConstructionCancelsItsEventAndStartsTheNext(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, _, principal, planet := queuedEmpire(t, ctx)

	running, err := universeWorld.Economy.EnqueueBuilding(ctx, principal, planet.ID, building.MetalMine, "mine")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := universeWorld.Economy.EnqueueBuilding(ctx, principal, planet.ID, building.SolarPlant, "plant"); err != nil {
		t.Fatal(err)
	}

	if _, err := universeWorld.Economy.CancelBuilding(ctx, principal, planet.ID, running.ID); err != nil {
		t.Fatalf("CancelBuilding() error = %v", err)
	}
	after, err := universeWorld.Economy.Planet(ctx, principal, planet.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Queue) != 1 || after.Queue[0].Building != building.SolarPlant || after.Queue[0].State != "active" {
		t.Fatalf("queue after cancelling the head = %#v", after.Queue)
	}
	// The event of the cancelled order must never fire again.
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM scheduled_events WHERE idempotency_key = ? AND state = 'cancelled'",
		1, fmt.Sprintf("building-complete:%d", running.ID))
	assertSingleValue(t, database, "SELECT COUNT(*) FROM scheduled_events WHERE event_type = 'building_completed' AND state = 'pending'", 1)
}

// Levels of one building form a chain, so dropping a level drops the levels
// queued above it too, all of them refunded.
func TestCancellingAMineDropsTheLevelsQueuedAboveIt(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, _, principal, planet := queuedEmpire(t, ctx)

	var entries []appeconomy.Queue
	for index := range 3 {
		entry, err := universeWorld.Economy.EnqueueBuilding(ctx, principal, planet.ID, building.MetalMine, fmt.Sprintf("mine-%d", index))
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, entry)
	}
	before, err := universeWorld.Economy.Planet(ctx, principal, planet.ID)
	if err != nil {
		t.Fatal(err)
	}

	cancellation, err := universeWorld.Economy.CancelBuilding(ctx, principal, planet.ID, entries[1].ID)
	if err != nil {
		t.Fatalf("CancelBuilding() error = %v", err)
	}
	if cancellation.Cancelled != 2 {
		t.Fatalf("cancelling level two dropped %d orders, want 2", cancellation.Cancelled)
	}
	refunded := domaineconomy.Resources{
		Metal:     entries[1].Cost.Metal + entries[2].Cost.Metal,
		Crystal:   entries[1].Cost.Crystal + entries[2].Cost.Crystal,
		Deuterium: entries[1].Cost.Deuterium + entries[2].Cost.Deuterium,
	}
	if cancellation.Refunded != refunded {
		t.Fatalf("refund = %#v, want %#v", cancellation.Refunded, refunded)
	}
	after, err := universeWorld.Economy.Planet(ctx, principal, planet.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Stock.Metal != before.Stock.Metal+refunded.Metal {
		t.Fatalf("stock after the refund = %#v", after.Stock)
	}
	if len(after.Queue) != 1 || after.Queue[0].TargetLevel != 1 {
		t.Fatalf("queue after the cascade = %#v", after.Queue)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM building_queue WHERE state = 'cancelled'", 2)
}

// A refund cannot overflow a full store. What does not fit is lost, and the
// cancellation says so rather than letting it vanish quietly.
func TestARefundAboveTheStorageCapacityIsClippedAndReported(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, _, principal, planet := queuedEmpire(t, ctx)

	entry, err := universeWorld.Economy.EnqueueBuilding(ctx, principal, planet.ID, building.MetalMine, "mine")
	if err != nil {
		t.Fatal(err)
	}
	full, err := universeWorld.Economy.Planet(ctx, principal, planet.ID)
	if err != nil {
		t.Fatal(err)
	}
	setResources(t, ctx, database, planet.ID, full.Capacity.Metal, full.Capacity.Crystal, full.Capacity.Deuterium)

	cancellation, err := universeWorld.Economy.CancelBuilding(ctx, principal, planet.ID, entry.ID)
	if err != nil {
		t.Fatalf("CancelBuilding() error = %v", err)
	}
	if cancellation.Refunded != (domaineconomy.Resources{}) || cancellation.Lost != entry.Cost {
		t.Fatalf("cancellation into full stores = %#v, order cost %#v", cancellation, entry.Cost)
	}
	after, err := universeWorld.Economy.Planet(ctx, principal, planet.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Stock != full.Capacity {
		t.Fatalf("full stores grew past their capacity: %#v", after.Stock)
	}
}

func TestCancellingAResearchRefundsItAndStartsTheNext(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, _, principal, planet := researchingEmpire(t, ctx)

	running, err := universeWorld.Research.EnqueueResearch(ctx, principal, planet.ID, research.EnergyTechnology, "energy")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := universeWorld.Research.EnqueueResearch(ctx, principal, planet.ID, research.ComputerTechnology, "computer"); err != nil {
		t.Fatal(err)
	}
	before, err := universeWorld.Research.Overview(ctx, principal, planet.ID)
	if err != nil {
		t.Fatal(err)
	}

	cancellation, err := universeWorld.Research.CancelResearch(ctx, principal, planet.ID, running.ID)
	if err != nil {
		t.Fatalf("CancelResearch() error = %v", err)
	}
	if cancellation.Refunded != running.Cost {
		t.Fatalf("refund = %#v, want %#v", cancellation.Refunded, running.Cost)
	}
	after, err := universeWorld.Research.Overview(ctx, principal, planet.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Planet.Stock.Crystal != before.Planet.Stock.Crystal+running.Cost.Crystal {
		t.Fatalf("stock after the refund = %#v", after.Planet.Stock)
	}
	if len(after.Queue) != 1 || after.Queue[0].Research != research.ComputerTechnology || after.Queue[0].State != "active" {
		t.Fatalf("queue after cancelling the head = %#v", after.Queue)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM research_queue WHERE state = 'cancelled'", 1)
}

// The units a batch already delivered are the player's to keep; only what the
// yard still owes comes back.
func TestCancellingABatchRefundsOnlyTheUnitsStillOwed(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, clock, principal, planet := producingEmpire(t, ctx)

	order, err := universeWorld.Shipyard.OrderFamily(ctx, principal, planet.ID, unit.LightFighter, unit.Ship, 4, "fighters")
	if err != nil {
		t.Fatal(err)
	}
	// Let two of the four leave the yard.
	setClock(t, clock, order.StartedAt.Add(2*order.UnitDuration))
	if _, err := universeWorld.Shipyard.Ships(ctx, principal, planet.ID); err != nil {
		t.Fatal(err)
	}

	cancellation, err := universeWorld.Shipyard.CancelOrder(ctx, principal, planet.ID, order.ID)
	if err != nil {
		t.Fatalf("CancelOrder() error = %v", err)
	}
	owed := domaineconomy.Resources{
		Metal: order.UnitCost.Metal * 2, Crystal: order.UnitCost.Crystal * 2,
		Deuterium: order.UnitCost.Deuterium * 2,
	}
	if cancellation.Refunded != owed {
		t.Fatalf("refund = %#v, want the two undelivered fighters %#v", cancellation.Refunded, owed)
	}
	assertSingleValue(t, database, "SELECT quantity FROM planet_units WHERE planet_id = 1 AND unit_id = 'light_fighter'", 2)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM production_orders WHERE state = 'cancelled'", 1)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM scheduled_events WHERE event_type = 'production_completed' AND state = 'pending'", 0)
}
