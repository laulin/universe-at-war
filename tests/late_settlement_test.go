package tests

import (
	"context"
	"fmt"
	"testing"
	"time"

	appclock "universeatwar/internal/clock"
	"universeatwar/internal/domain/building"
)

// A player looking at a planet settles it to the present. An event that fell
// due a moment before, and that the simulation has not reached yet, must still
// apply afterwards instead of failing for good on a production time that has
// already moved past it.
func TestABuildingDueBeforeAPlayerSettlementStillCompletes(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, clock, principal, planet := queuedEmpire(t, ctx)
	for index := range 2 {
		if _, err := universeWorld.Economy.EnqueueBuilding(ctx, principal, planet.ID, building.MetalMine, fmt.Sprintf("mine-%d", index)); err != nil {
			t.Fatalf("EnqueueBuilding(%d) error = %v", index, err)
		}
	}
	planet, err := universeWorld.Economy.Planet(ctx, principal, planet.ID)
	if err != nil {
		t.Fatal(err)
	}
	head := planet.Queue[0]

	setClock(t, clock, head.CompletesAt.Add(2*time.Second))
	if _, err := universeWorld.Economy.Repository.Planet(ctx, principal.AccountID, planet.ID, clock.Now(), universeWorld.Economy.Catalogue); err != nil {
		t.Fatalf("settle the planet before the event: %v", err)
	}
	if _, err := universeWorld.Events.CompleteDue(ctx, 10); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}

	assertSingleValue(t, database, "SELECT COUNT(*) FROM scheduled_events WHERE last_error IS NOT NULL", 0)
	assertSingleText(t, database, "SELECT state FROM building_queue WHERE id = ?", "completed", head.ID)
	assertSingleValue(t, database, "SELECT level FROM planet_buildings WHERE planet_id = ? AND building_id = 'metal_mine'", 1, planet.ID)
	// The next order still starts when the first one was due, not when the
	// simulation came round to it.
	assertSingleText(t, database, "SELECT started_at FROM building_queue WHERE planet_id = ? AND state = 'active'",
		head.CompletesAt.UTC().Format(time.RFC3339Nano), planet.ID)
}

func TestAFleetReturningToAPlanetSettledMeanwhileStillLands(t *testing.T) {
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC))
	database, universeWorld, alice, launched := launchedTransport(t, ctx, clock)

	setClock(t, clock, launched.ArrivesAt)
	if _, err := universeWorld.Events.CompleteDue(ctx, 10); err != nil {
		t.Fatalf("CompleteDue() at arrival error = %v", err)
	}
	assertSingleText(t, database, "SELECT state FROM fleets WHERE id = ?", "returning", launched.ID)

	setClock(t, clock, launched.ReturnsAt.Add(2*time.Second))
	if _, err := universeWorld.Economy.Repository.Planet(ctx, alice.AccountID, 0, clock.Now(), universeWorld.Economy.Catalogue); err != nil {
		t.Fatalf("settle the origin before the return: %v", err)
	}
	if _, err := universeWorld.Events.CompleteDue(ctx, 10); err != nil {
		t.Fatalf("CompleteDue() at return error = %v", err)
	}

	assertSingleValue(t, database, "SELECT COUNT(*) FROM scheduled_events WHERE last_error IS NOT NULL", 0)
	assertSingleText(t, database, "SELECT state FROM fleets WHERE id = ?", "completed", launched.ID)
	assertSingleValue(t, database, "SELECT quantity FROM planet_units WHERE planet_id = 1 AND unit_id = 'small_cargo'", 2)
}
