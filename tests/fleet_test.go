package tests

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appfleet "universeatwar/internal/app/fleet"
	appclock "universeatwar/internal/clock"
	domaineconomy "universeatwar/internal/domain/economy"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/unit"
	storagesqlite "universeatwar/internal/storage/sqlite"
)

func TestScenarioDTransportLeavesArrivesAndComesBack(t *testing.T) {
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC))
	database := economyDatabase(t, ctx, 2)
	universe := newWorld(t, database, clock)
	alice := appauth.Principal{AccountID: 1}
	bob := appauth.Principal{AccountID: 2}

	home, err := universe.Economy.CreateEmpire(ctx, alice, "Alice")
	if err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	target, err := universe.Economy.CreateEmpire(ctx, bob, "Bob")
	if err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	setUnits(t, ctx, database, home.ID, "small_cargo", 2)
	setResources(t, ctx, database, home.ID, 1000, 500, 200)

	request := appfleet.LaunchRequest{
		Target:      target.Coordinate,
		TargetKind:  domainfleet.TargetPlanet,
		Mission:     domainfleet.MissionTransport,
		Composition: domainfleet.Composition{unit.SmallCargo: 2},
		Cargo:       domaineconomy.Resources{Metal: 900, Crystal: 400},
		Percent:     100,
	}
	plan, err := universe.Fleet.Preview(ctx, alice, home.ID, request)
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if plan.Fuel <= 0 || plan.ReturnsAt == nil {
		t.Fatalf("preview = %+v", plan)
	}

	launched, err := universe.Fleet.Launch(ctx, alice, home.ID, request, "launch-1")
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if launched.State != domainfleet.Outbound || launched.ReturnsAt == nil {
		t.Fatalf("fleet = %+v", launched)
	}
	replay, err := universe.Fleet.Launch(ctx, alice, home.ID, request, "launch-1")
	if err != nil || replay.ID != launched.ID {
		t.Fatalf("idempotent replay = %+v %v", replay, err)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM planet_units WHERE planet_id = 1 AND unit_id = 'small_cargo' AND quantity > 0", 0)
	assertSingleValue(t, database, "SELECT metal FROM planet_resources WHERE planet_id = 1", 100)
	assertSingleValue(t, database, "SELECT deuterium FROM planet_resources WHERE planet_id = 1", int(200-launched.Fuel))
	assertSingleValue(t, database, "SELECT COUNT(*) FROM game_event_log WHERE event_type = 'fleet_launched'", 1)

	clock.Set(launched.ArrivesAt)
	if _, err := universe.Events.CompleteDue(ctx, 10); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}
	// The target keeps producing during the flight, so the delivery itself is
	// read from the journal rather than from an absolute stock.
	assertSingleValue(t, database, `SELECT json_extract(payload, '$.delivered_metal') FROM game_event_log WHERE event_type = 'fleet_arrived'`, 900)
	assertSingleValue(t, database, `SELECT json_extract(payload, '$.delivered_crystal') FROM game_event_log WHERE event_type = 'fleet_arrived'`, 400)
	var targetMetal int64
	if err := database.Read().QueryRowContext(ctx, "SELECT metal FROM planet_resources WHERE planet_id = 2").Scan(&targetMetal); err != nil {
		t.Fatal(err)
	}
	if targetMetal < 500+900 {
		t.Fatalf("target metal = %d, want at least %d", targetMetal, 500+900)
	}
	assertSingleText(t, database, "SELECT state FROM fleets WHERE id = 1", "returning")

	clock.Set(*launched.ReturnsAt)
	if _, err := universe.Events.CompleteDue(ctx, 10); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}
	assertSingleValue(t, database, "SELECT quantity FROM planet_units WHERE planet_id = 1 AND unit_id = 'small_cargo'", 2)
	assertSingleText(t, database, "SELECT state FROM fleets WHERE id = 1", "completed")
	assertSingleValue(t, database, "SELECT COUNT(*) FROM game_event_log WHERE event_type IN ('fleet_launched', 'fleet_arrived', 'fleet_returned')", 3)

	replayEvent(t, ctx, database, "fleet_returned")
	if _, err := universe.Events.CompleteDue(ctx, 10); err != nil {
		t.Fatalf("CompleteDue() after redelivery error = %v", err)
	}
	assertSingleValue(t, database, "SELECT quantity FROM planet_units WHERE planet_id = 1 AND unit_id = 'small_cargo'", 2)
}

func TestLaunchRefusalsLeaveNoTrace(t *testing.T) {
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
	setUnits(t, ctx, database, home.ID, "small_cargo", 2)
	setResources(t, ctx, database, home.ID, 1000, 500, 0)

	base := appfleet.LaunchRequest{
		Target: target.Coordinate, TargetKind: domainfleet.TargetPlanet,
		Mission: domainfleet.MissionTransport, Composition: domainfleet.Composition{unit.SmallCargo: 2},
		Percent: 100,
	}
	if _, err := universe.Fleet.Launch(ctx, alice, home.ID, base, "no-fuel"); !errors.Is(err, domainfleet.ErrInsufficientFuel) {
		t.Fatalf("Launch() without deuterium error = %v", err)
	}
	setResources(t, ctx, database, home.ID, 1000, 500, 200)

	tooMany := base
	tooMany.Composition = domainfleet.Composition{unit.SmallCargo: 3}
	if _, err := universe.Fleet.Launch(ctx, alice, home.ID, tooMany, "too-many"); !errors.Is(err, domainfleet.ErrInsufficientUnits) {
		t.Fatalf("Launch() beyond the inventory error = %v", err)
	}
	tooHeavy := base
	tooHeavy.Cargo = domaineconomy.Resources{Metal: 100000}
	if _, err := universe.Fleet.Launch(ctx, alice, home.ID, tooHeavy, "too-heavy"); !errors.Is(err, domainfleet.ErrCargoExceedsCapacity) {
		t.Fatalf("Launch() with too much cargo error = %v", err)
	}
	foreignDeploy := base
	foreignDeploy.Mission = domainfleet.MissionDeploy
	if _, err := universe.Fleet.Launch(ctx, alice, home.ID, foreignDeploy, "foreign"); !errors.Is(err, domainfleet.ErrInvalidTarget) {
		t.Fatalf("deploying to a foreign planet error = %v", err)
	}
	empty := base
	empty.Target = coordinateOf(t, 1, 9, 9)
	if _, err := universe.Fleet.Launch(ctx, alice, home.ID, empty, "empty"); !errors.Is(err, domainfleet.ErrInvalidTarget) {
		t.Fatalf("transport to an empty position error = %v", err)
	}

	assertSingleValue(t, database, "SELECT COUNT(*) FROM fleets", 0)
	assertSingleValue(t, database, "SELECT quantity FROM planet_units WHERE planet_id = 1 AND unit_id = 'small_cargo'", 2)
	assertSingleValue(t, database, "SELECT deuterium FROM planet_resources WHERE planet_id = 1", 200)
}

func TestTransportDeliveryIsCappedByStorage(t *testing.T) {
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
	setUnits(t, ctx, database, home.ID, "small_cargo", 2)
	setResources(t, ctx, database, home.ID, 5000, 500, 200)
	setResources(t, ctx, database, target.ID, 9500, 0, 0)

	launched, err := universe.Fleet.Launch(ctx, alice, home.ID, appfleet.LaunchRequest{
		Target: target.Coordinate, TargetKind: domainfleet.TargetPlanet,
		Mission: domainfleet.MissionTransport, Composition: domainfleet.Composition{unit.SmallCargo: 2},
		Cargo: domaineconomy.Resources{Metal: 1000}, Percent: 100,
	}, "capped")
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}

	clock.Set(launched.ArrivesAt)
	if _, err := universe.Events.CompleteDue(ctx, 10); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}
	// The store is filled to the brim and the surplus stays in the hold.
	assertSingleValue(t, database, "SELECT metal FROM planet_resources WHERE planet_id = 2", 10000)
	var delivered, remaining int64
	if err := database.Read().QueryRowContext(ctx,
		`SELECT json_extract(payload, '$.delivered_metal') FROM game_event_log WHERE event_type = 'fleet_arrived'`).Scan(&delivered); err != nil {
		t.Fatal(err)
	}
	if err := database.Read().QueryRowContext(ctx, "SELECT metal FROM fleet_cargo WHERE fleet_id = 1").Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if delivered+remaining != 1000 || remaining == 0 {
		t.Fatalf("delivered %d and kept %d of a 1000 metal cargo", delivered, remaining)
	}

	clock.Set(*launched.ReturnsAt)
	if _, err := universe.Events.CompleteDue(ctx, 10); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}
	assertSingleValue(t, database, "SELECT metal FROM fleet_cargo WHERE fleet_id = 1", 0)
	var metal int64
	if err := database.Read().QueryRowContext(ctx, "SELECT metal FROM planet_resources WHERE planet_id = 1").Scan(&metal); err != nil {
		t.Fatal(err)
	}
	if metal < 4500 {
		t.Fatalf("returned cargo was lost: metal = %d", metal)
	}
}

func TestDeploymentMovesShipsAndCargoForGood(t *testing.T) {
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC))
	database := economyDatabase(t, ctx, 1)
	universe := newWorld(t, database, clock)
	alice := appauth.Principal{AccountID: 1}

	home, err := universe.Economy.CreateEmpire(ctx, alice, "Alice")
	if err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	colony := insertColony(t, ctx, database, 1, "Colonie", 1, 2, 4)
	setUnits(t, ctx, database, home.ID, "small_cargo", 2)
	setResources(t, ctx, database, home.ID, 5000, 500, 200)

	launched, err := universe.Fleet.Launch(ctx, alice, home.ID, appfleet.LaunchRequest{
		Target: coordinateOf(t, 1, 2, 4), TargetKind: domainfleet.TargetPlanet,
		Mission: domainfleet.MissionDeploy, Composition: domainfleet.Composition{unit.SmallCargo: 2},
		Cargo: domaineconomy.Resources{Metal: 1000}, Percent: 100,
	}, "deploy")
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if launched.ReturnsAt != nil {
		t.Fatalf("a deployment must not plan a return: %+v", launched)
	}

	clock.Set(launched.ArrivesAt)
	if _, err := universe.Events.CompleteDue(ctx, 10); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}
	assertSingleValue(t, database, fmt.Sprintf("SELECT quantity FROM planet_units WHERE planet_id = %d AND unit_id = 'small_cargo'", colony), 2)
	assertSingleText(t, database, "SELECT state FROM fleets WHERE id = 1", "completed")
	assertSingleValue(t, database, "SELECT COUNT(*) FROM scheduled_events WHERE state = 'pending'", 0)
}

func TestScenarioJRulesetChangeKeepsRunningFleetsUnchanged(t *testing.T) {
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
	setUnits(t, ctx, database, home.ID, "small_cargo", 4)
	setResearch(t, ctx, database, 1, "computer_technology", 1) // a second fleet slot
	setResources(t, ctx, database, home.ID, 5000, 5000, 5000)

	request := appfleet.LaunchRequest{
		Target: target.Coordinate, TargetKind: domainfleet.TargetPlanet,
		Mission: domainfleet.MissionTransport, Composition: domainfleet.Composition{unit.SmallCargo: 2},
		Percent: 100,
	}
	slow, err := universe.Fleet.Launch(ctx, alice, home.ID, request, "before")
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	activateFasterFleetRuleset(t, ctx, database)

	overview, err := universe.Fleet.Overview(ctx, alice, home.ID)
	if err != nil {
		t.Fatalf("Overview() error = %v", err)
	}
	if len(overview.Fleets) != 1 || !overview.Fleets[0].ArrivesAt.Equal(slow.ArrivesAt) {
		t.Fatalf("the running fleet changed after the ruleset changed: %+v", overview.Fleets)
	}

	fast, err := universe.Fleet.Launch(ctx, alice, home.ID, request, "after")
	if err != nil {
		t.Fatalf("Launch() after the change error = %v", err)
	}
	slowDuration := slow.ArrivesAt.Sub(slow.DepartedAt)
	fastDuration := fast.ArrivesAt.Sub(fast.DepartedAt)
	if fastDuration >= slowDuration {
		t.Fatalf("new fleets ignore the new ruleset: %v then %v", slowDuration, fastDuration)
	}
}

func activateFasterFleetRuleset(t *testing.T, ctx context.Context, database *storagesqlite.Database) {
	t.Helper()
	if _, err := database.Write().ExecContext(ctx, "UPDATE ruleset_versions SET status = 'superseded' WHERE status = 'active'"); err != nil {
		t.Fatalf("supersede ruleset: %v", err)
	}
	if _, err := database.Write().ExecContext(ctx, `
		INSERT INTO ruleset_versions(version, status, document, checksum, author_account_id, effective_at, created_at)
		SELECT 2, 'active', json_set(document, '$.time.peaceful_fleet_speed', 10.0), 'test-2', author_account_id, effective_at, created_at
		FROM ruleset_versions WHERE version = 1
	`); err != nil {
		t.Fatalf("activate faster ruleset: %v", err)
	}
}
