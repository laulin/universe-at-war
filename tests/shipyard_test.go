package tests

import (
	"context"
	"errors"
	"testing"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appeconomy "universeatwar/internal/app/economy"
	appshipyard "universeatwar/internal/app/shipyard"
	appclock "universeatwar/internal/clock"
	domaineconomy "universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/unit"
	storagesqlite "universeatwar/internal/storage/sqlite"
)

func TestProductionOrderDeliversIncrementallyAndCompletesOnce(t *testing.T) {
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC))
	database := economyDatabase(t, ctx, 1)
	universe := newWorld(t, database, clock)
	principal := appauth.Principal{AccountID: 1}

	planet, err := universe.Economy.CreateEmpire(ctx, principal, "Captain")
	if err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	setBuilding(t, ctx, database, planet.ID, "shipyard", 1)
	setResearch(t, ctx, database, 1, "combustion_drive", 1)
	setResources(t, ctx, database, planet.ID, 9000, 3000, 0)

	order, err := universe.Shipyard.Order(ctx, principal, planet.ID, unit.LightFighter, 3, "order-1")
	if err != nil {
		t.Fatalf("Order() error = %v", err)
	}
	if order.Quantity != 3 || order.UnitDuration != 2880*time.Second {
		t.Fatalf("order = %+v", order)
	}
	if order.TotalCost != (domaineconomy.Resources{Metal: 9000, Crystal: 3000}) {
		t.Fatalf("total cost = %v", order.TotalCost)
	}
	assertSingleValue(t, database, "SELECT metal FROM planet_resources WHERE planet_id = 1", 0)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM game_event_log WHERE event_type = 'production_ordered'", 1)

	replay, err := universe.Shipyard.Order(ctx, principal, planet.ID, unit.LightFighter, 3, "order-1")
	if err != nil || replay.ID != order.ID {
		t.Fatalf("idempotent replay = %+v %v", replay, err)
	}
	// A queued batch is paid for the moment it is ordered, so an empty purse
	// refuses it even though the defence queue is free.
	if _, err := universe.Shipyard.Order(ctx, principal, planet.ID, unit.RocketLauncher, 1, "order-2"); !errors.Is(err, domaineconomy.ErrInsufficientResources) {
		t.Fatalf("second order error = %v, want ErrInsufficientResources", err)
	}

	clock.Advance(2 * 2880 * time.Second)
	overview, err := universe.Shipyard.Ships(ctx, principal, planet.ID)
	if err != nil {
		t.Fatalf("Ships() error = %v", err)
	}
	if overview.Inventory[unit.LightFighter] != 2 {
		t.Fatalf("delivered units = %d, want 2", overview.Inventory[unit.LightFighter])
	}
	if len(overview.Queue) != 1 || overview.Queue[0].Delivered != 2 || overview.Queue[0].Quantity != 3 {
		t.Fatalf("running order = %+v", overview.Queue)
	}

	clock.Advance(2880 * time.Second)
	if _, err := universe.Events.CompleteDue(ctx, 10); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}
	assertSingleValue(t, database, "SELECT quantity FROM planet_units WHERE planet_id = 1 AND unit_id = 'light_fighter'", 3)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM production_orders WHERE state = 'completed'", 1)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM game_event_log WHERE event_type = 'production_completed'", 1)

	replayEvent(t, ctx, database, "production_completed")
	if _, err := universe.Events.CompleteDue(ctx, 10); err != nil {
		t.Fatalf("CompleteDue() after redelivery error = %v", err)
	}
	assertSingleValue(t, database, "SELECT quantity FROM planet_units WHERE planet_id = 1 AND unit_id = 'light_fighter'", 3)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM game_event_log WHERE event_type = 'production_completed'", 1)
}

func TestDefenceOrdersUseTheirOwnFamily(t *testing.T) {
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC))
	database := economyDatabase(t, ctx, 1)
	universe := newWorld(t, database, clock)
	principal := appauth.Principal{AccountID: 1}

	planet, err := universe.Economy.CreateEmpire(ctx, principal, "Captain")
	if err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	setBuilding(t, ctx, database, planet.ID, "shipyard", 1)
	setResources(t, ctx, database, planet.ID, 4000, 0, 0)

	order, err := universe.Shipyard.Order(ctx, principal, planet.ID, unit.RocketLauncher, 2, "defense-1")
	if err != nil {
		t.Fatalf("Order() error = %v", err)
	}
	if order.Family != unit.Defense || order.UnitDuration != 1440*time.Second {
		t.Fatalf("order = %+v", order)
	}

	defense, err := universe.Shipyard.Defenses(ctx, principal, planet.ID)
	if err != nil {
		t.Fatalf("Defenses() error = %v", err)
	}
	for _, choice := range defense.Choices {
		if choice.Definition.Family != unit.Defense {
			t.Fatalf("defense page lists %s", choice.Definition.ID)
		}
	}
	ships, err := universe.Shipyard.Ships(ctx, principal, planet.ID)
	if err != nil {
		t.Fatalf("Ships() error = %v", err)
	}
	for _, choice := range ships.Choices {
		if choice.Definition.Family != unit.Ship {
			t.Fatalf("shipyard page lists %s", choice.Definition.ID)
		}
	}
}

func TestProductionRejectsInvalidQuantitiesWithoutMutating(t *testing.T) {
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC))
	database := economyDatabase(t, ctx, 1)
	universe := newWorld(t, database, clock)
	principal := appauth.Principal{AccountID: 1}

	planet, err := universe.Economy.CreateEmpire(ctx, principal, "Captain")
	if err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	setBuilding(t, ctx, database, planet.ID, "shipyard", 1)
	setResearch(t, ctx, database, 1, "combustion_drive", 1)
	setResources(t, ctx, database, planet.ID, 9000, 3000, 0)

	for _, quantity := range []int64{0, -3, unit.MaximumOrderQuantity + 1} {
		if _, err := universe.Shipyard.Order(ctx, principal, planet.ID, unit.LightFighter, quantity, "invalid"); !errors.Is(err, appshipyard.ErrInvalidQuantity) {
			t.Fatalf("Order(%d) error = %v, want ErrInvalidQuantity", quantity, err)
		}
	}
	if _, err := universe.Shipyard.Order(ctx, principal, planet.ID, unit.LightFighter, 100, "too-many"); !errors.Is(err, domaineconomy.ErrInsufficientResources) {
		t.Fatalf("Order() beyond the stock error = %v", err)
	}
	assertSingleValue(t, database, "SELECT metal FROM planet_resources WHERE planet_id = 1", 9000)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM production_orders", 0)

	if _, err := database.Write().ExecContext(ctx,
		"INSERT INTO planet_units(planet_id, unit_id, quantity) VALUES (1, 'light_fighter', -1)"); err == nil {
		t.Fatal("database accepted a negative inventory")
	}
}

func TestDeliveredSatellitesRaiseEnergy(t *testing.T) {
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC))
	database := economyDatabase(t, ctx, 1)
	universe := newWorld(t, database, clock)
	principal := appauth.Principal{AccountID: 1}

	planet, err := universe.Economy.CreateEmpire(ctx, principal, "Captain")
	if err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	setBuilding(t, ctx, database, planet.ID, "shipyard", 1)
	setResources(t, ctx, database, planet.ID, 0, 4000, 1000)

	order, err := universe.Shipyard.Order(ctx, principal, planet.ID, unit.SolarSatellite, 2, "satellites")
	if err != nil {
		t.Fatalf("Order() error = %v", err)
	}
	before, err := universe.Economy.Planet(ctx, principal, planet.ID)
	if err != nil {
		t.Fatalf("Planet() error = %v", err)
	}
	if before.Energy.Produced != 0 {
		t.Fatalf("energy before delivery = %d, want 0", before.Energy.Produced)
	}

	clock.Advance(2 * order.UnitDuration)
	after, err := universe.Economy.Planet(ctx, principal, planet.ID)
	if err != nil {
		t.Fatalf("Planet() error = %v", err)
	}
	if after.Units[unit.SolarSatellite] != 2 {
		t.Fatalf("satellites = %d, want 2", after.Units[unit.SolarSatellite])
	}
	if after.Energy.Produced != 66 {
		t.Fatalf("energy after delivery = %d, want 66", after.Energy.Produced)
	}
}

func TestProductionKeepsItsCostsAfterARulesetChange(t *testing.T) {
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC))
	database := economyDatabase(t, ctx, 1)
	universe := newWorld(t, database, clock)
	principal := appauth.Principal{AccountID: 1}

	planet, err := universe.Economy.CreateEmpire(ctx, principal, "Captain")
	if err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	setBuilding(t, ctx, database, planet.ID, "shipyard", 1)
	setResources(t, ctx, database, planet.ID, 9000, 3000, 0)

	order, err := universe.Shipyard.Order(ctx, principal, planet.ID, unit.RocketLauncher, 2, "before-change")
	if err != nil {
		t.Fatalf("Order() error = %v", err)
	}
	activateFasterRuleset(t, ctx, database)

	overview, err := universe.Shipyard.Defenses(ctx, principal, planet.ID)
	if err != nil {
		t.Fatalf("Defenses() error = %v", err)
	}
	if len(overview.Queue) != 1 || !overview.Queue[0].CompletesAt.Equal(order.CompletesAt) {
		t.Fatalf("running order changed after the ruleset changed: %+v", overview.Queue)
	}
	for _, choice := range overview.Choices {
		if choice.Definition.ID == unit.RocketLauncher && choice.UnitDuration != 144*time.Second {
			t.Fatalf("new orders ignore the new ruleset: %v", choice.UnitDuration)
		}
	}
}

func TestShipyardUpgradeAndProductionExcludeEachOther(t *testing.T) {
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC))
	database := economyDatabase(t, ctx, 1)
	universe := newWorld(t, database, clock)
	principal := appauth.Principal{AccountID: 1}

	planet, err := universe.Economy.CreateEmpire(ctx, principal, "Captain")
	if err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	setBuilding(t, ctx, database, planet.ID, "shipyard", 1)
	setBuilding(t, ctx, database, planet.ID, "robotics_factory", 2)
	setResources(t, ctx, database, planet.ID, 50000, 50000, 50000)

	if _, err := universe.Shipyard.Order(ctx, principal, planet.ID, unit.RocketLauncher, 1, "busy"); err != nil {
		t.Fatalf("Order() error = %v", err)
	}
	if _, err := universe.Economy.EnqueueBuilding(ctx, principal, planet.ID, "shipyard", "upgrade"); !errors.Is(err, appeconomy.ErrFacilityBusy) {
		t.Fatalf("shipyard upgrade during production error = %v, want ErrFacilityBusy", err)
	}
}

// activateFasterRuleset publishes a new active ruleset version that halves every
// production time, which must not touch the orders already running.
func activateFasterRuleset(t *testing.T, ctx context.Context, database *storagesqlite.Database) {
	t.Helper()
	if _, err := database.Write().ExecContext(ctx, "UPDATE ruleset_versions SET status = 'superseded' WHERE status = 'active'"); err != nil {
		t.Fatalf("supersede ruleset: %v", err)
	}
	if _, err := database.Write().ExecContext(ctx, `
		INSERT INTO ruleset_versions(version, status, document, checksum, author_account_id, effective_at, created_at)
		SELECT 2, 'active', json_set(document, '$.time.defense_speed', 10.0), 'test-2', author_account_id, effective_at, created_at
		FROM ruleset_versions WHERE version = 1
	`); err != nil {
		t.Fatalf("activate faster ruleset: %v", err)
	}
}
