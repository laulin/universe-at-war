package tests

import (
	"context"
	"errors"
	"testing"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appfleet "universeatwar/internal/app/fleet"
	appclock "universeatwar/internal/clock"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/unit"
	storagesqlite "universeatwar/internal/storage/sqlite"
)

func TestScenarioEFromEspionageToRecycling(t *testing.T) {
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
	setUnits(t, ctx, database, home.ID, "espionage_probe", 5)
	setUnits(t, ctx, database, home.ID, "cruiser", 12)
	setUnits(t, ctx, database, home.ID, "recycler", 2)
	setResearch(t, ctx, database, 1, "espionage_technology", 4)
	setResearch(t, ctx, database, 1, "computer_technology", 3)
	setResources(t, ctx, database, home.ID, 5000, 5000, 5000)

	setUnits(t, ctx, database, target.ID, "light_fighter", 20)
	setUnits(t, ctx, database, target.ID, "rocket_launcher", 10)
	setBuilding(t, ctx, database, target.ID, "metal_storage", 4)
	setResources(t, ctx, database, target.ID, 60000, 30000, 5000)

	// A spy flight first: three probes see everything a level of four allows.
	spy, err := universe.Fleet.Launch(ctx, alice, home.ID, appfleet.LaunchRequest{
		Target: target.Coordinate, TargetKind: domainfleet.TargetPlanet, Mission: domainfleet.MissionEspionage,
		Composition: domainfleet.Composition{unit.EspionageProbe: 3}, Percent: 100,
	}, "spy")
	if err != nil {
		t.Fatalf("Launch(espionage) error = %v", err)
	}
	clock.Set(spy.ArrivesAt)
	if _, err := universe.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM reports WHERE recipient_player_id = 1 AND kind = 'espionage'", 1)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM reports WHERE recipient_player_id = 2 AND kind = 'espionage_detected'", 1)
	assertSingleValue(t, database, `SELECT json_extract(payload, '$.fleet.light_fighter') FROM reports WHERE kind = 'espionage'`, 20)
	assertSingleValue(t, database, `SELECT json_extract(payload, '$.defenses.rocket_launcher') FROM reports WHERE kind = 'espionage'`, 10)
	assertSingleValue(t, database, `SELECT COUNT(*) FROM reports WHERE kind = 'espionage_detected' AND payload LIKE '%light_fighter%'`, 0)

	// Then the attack itself.
	attack, err := universe.Fleet.Launch(ctx, alice, home.ID, appfleet.LaunchRequest{
		Target: target.Coordinate, TargetKind: domainfleet.TargetPlanet, Mission: domainfleet.MissionAttack,
		Composition: domainfleet.Composition{unit.Cruiser: 12}, Percent: 100,
	}, "attack")
	if err != nil {
		t.Fatalf("Launch(attack) error = %v", err)
	}
	clock.Set(attack.ArrivesAt)
	if _, err := universe.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM reports WHERE recipient_player_id = 1 AND kind = 'combat_attack'", 1)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM reports WHERE recipient_player_id = 2 AND kind = 'combat_defense'", 1)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM game_event_log WHERE event_type = 'combat_resolved'", 1)

	var outcome string
	if err := database.Read().QueryRowContext(ctx,
		`SELECT json_extract(payload, '$.outcome') FROM game_event_log WHERE event_type = 'combat_resolved'`).Scan(&outcome); err != nil {
		t.Fatal(err)
	}
	if outcome != "attacker" {
		t.Fatalf("twelve cruisers lost against twenty fighters and ten launchers: %q", outcome)
	}

	var lootMetal, debrisMetal, debrisCrystal int64
	if err := database.Read().QueryRowContext(ctx,
		`SELECT json_extract(payload, '$.loot_metal') FROM game_event_log WHERE event_type = 'combat_resolved'`).Scan(&lootMetal); err != nil {
		t.Fatal(err)
	}
	if lootMetal <= 0 {
		t.Fatalf("the winner took no metal: %d", lootMetal)
	}
	if err := database.Read().QueryRowContext(ctx,
		"SELECT metal, crystal FROM debris_fields WHERE galaxy = ? AND system = ? AND position = ?",
		target.Coordinate.Galaxy, target.Coordinate.System, target.Coordinate.Position).Scan(&debrisMetal, &debrisCrystal); err != nil {
		t.Fatalf("no debris field was created: %v", err)
	}
	if debrisMetal <= 0 {
		t.Fatalf("the battle left no metal debris: %d", debrisMetal)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM fleet_cargo WHERE fleet_id = 2 AND metal > 0", 1)

	// The recyclers then lift the wreckage.
	recycle, err := universe.Fleet.Launch(ctx, alice, home.ID, appfleet.LaunchRequest{
		Target: target.Coordinate, TargetKind: domainfleet.TargetDebris, Mission: domainfleet.MissionRecycle,
		Composition: domainfleet.Composition{unit.Recycler: 2}, Percent: 100,
	}, "recycle")
	if err != nil {
		t.Fatalf("Launch(recycle) error = %v", err)
	}
	clock.Set(recycle.ArrivesAt)
	if _, err := universe.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM reports WHERE kind = 'recycling'", 1)
	var collected int64
	if err := database.Read().QueryRowContext(ctx,
		`SELECT json_extract(payload, '$.metal') FROM game_event_log WHERE event_type = 'debris_recycled'`).Scan(&collected); err != nil {
		t.Fatal(err)
	}
	if collected <= 0 || collected > debrisMetal {
		t.Fatalf("recycled %d out of %d metal", collected, debrisMetal)
	}

	// Everything comes home.
	clock.Advance(48 * time.Hour)
	if _, err := universe.Events.CompleteDue(ctx, 50); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM fleets WHERE state IN ('outbound', 'returning', 'recalled')", 0)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM scheduled_events WHERE state = 'pending'", 0)
	var stationedRecyclers, negatives int64
	if err := database.Read().QueryRowContext(ctx,
		"SELECT quantity FROM planet_units WHERE planet_id = 1 AND unit_id = 'recycler'").Scan(&stationedRecyclers); err != nil {
		t.Fatal(err)
	}
	if stationedRecyclers != 2 {
		t.Fatalf("recyclers home = %d, want 2", stationedRecyclers)
	}
	if err := database.Read().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM planet_units WHERE quantity < 0").Scan(&negatives); err != nil {
		t.Fatal(err)
	}
	if negatives != 0 {
		t.Fatal("an inventory went negative during the campaign")
	}
}

func TestCombatRedeliveryChangesNothing(t *testing.T) {
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC))
	database, universe, alice, attack := launchedAttack(t, ctx, clock)

	clock.Set(attack.ArrivesAt)
	if _, err := universe.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}
	var debrisBefore int64
	if err := database.Read().QueryRowContext(ctx, "SELECT COALESCE(SUM(metal), 0) FROM debris_fields").Scan(&debrisBefore); err != nil {
		t.Fatal(err)
	}

	replayEvent(t, ctx, database, "combat_resolved")
	if _, err := universe.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatalf("CompleteDue() after redelivery error = %v", err)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM reports WHERE kind = 'combat_attack'", 1)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM game_event_log WHERE event_type = 'combat_resolved'", 1)
	var debrisAfter int64
	if err := database.Read().QueryRowContext(ctx, "SELECT COALESCE(SUM(metal), 0) FROM debris_fields").Scan(&debrisAfter); err != nil {
		t.Fatal(err)
	}
	if debrisAfter != debrisBefore {
		t.Fatalf("a redelivered battle created debris again: %d then %d", debrisBefore, debrisAfter)
	}
	_ = alice
}

func TestTwoRecyclersNeverTakeMoreThanTheField(t *testing.T) {
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC))
	database := economyDatabase(t, ctx, 2)
	universe := newWorld(t, database, clock)
	alice := appauth.Principal{AccountID: 1}

	home, err := universe.Economy.CreateEmpire(ctx, alice, "Alice")
	if err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	field := coordinateOf(t, 1, 1, 5)
	if _, err := database.Write().ExecContext(ctx, `
		INSERT INTO debris_fields(galaxy, system, position, metal, crystal, created_at, updated_at)
		VALUES (?, ?, ?, 30000, 0, '2042-09-10T11:12:13Z', '2042-09-10T11:12:13Z')
	`, field.Galaxy, field.System, field.Position); err != nil {
		t.Fatal(err)
	}
	setUnits(t, ctx, database, home.ID, "recycler", 2)
	setResearch(t, ctx, database, 1, "computer_technology", 3)
	setResources(t, ctx, database, home.ID, 5000, 5000, 5000)

	first, err := universe.Fleet.Launch(ctx, alice, home.ID, appfleet.LaunchRequest{
		Target: field, TargetKind: domainfleet.TargetDebris, Mission: domainfleet.MissionRecycle,
		Composition: domainfleet.Composition{unit.Recycler: 1}, Percent: 100,
	}, "first")
	if err != nil {
		t.Fatalf("Launch(first) error = %v", err)
	}
	second, err := universe.Fleet.Launch(ctx, alice, home.ID, appfleet.LaunchRequest{
		Target: field, TargetKind: domainfleet.TargetDebris, Mission: domainfleet.MissionRecycle,
		Composition: domainfleet.Composition{unit.Recycler: 1}, Percent: 100,
	}, "second")
	if err != nil {
		t.Fatalf("Launch(second) error = %v", err)
	}

	clock.Set(second.ArrivesAt)
	if _, err := universe.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}
	var collected int64
	if err := database.Read().QueryRowContext(ctx,
		`SELECT COALESCE(SUM(json_extract(payload, '$.metal')), 0) FROM game_event_log WHERE event_type = 'debris_recycled'`).Scan(&collected); err != nil {
		t.Fatal(err)
	}
	if collected != 30000 {
		t.Fatalf("the two recyclers lifted %d out of a 30000 field", collected)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM debris_fields", 0)
	_ = first
}

func TestEspionageAndAttackRefuseTheirOwnPlanets(t *testing.T) {
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC))
	database := economyDatabase(t, ctx, 2)
	universe := newWorld(t, database, clock)
	alice := appauth.Principal{AccountID: 1}

	home, err := universe.Economy.CreateEmpire(ctx, alice, "Alice")
	if err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	colony := insertColony(t, ctx, database, 1, "Colonie", 1, 1, 5)
	setUnits(t, ctx, database, home.ID, "espionage_probe", 3)
	setUnits(t, ctx, database, home.ID, "light_fighter", 3)
	setUnits(t, ctx, database, home.ID, "recycler", 1)
	setResources(t, ctx, database, home.ID, 5000, 5000, 5000)

	own := coordinateOf(t, 1, 1, 5)
	if _, err := universe.Fleet.Launch(ctx, alice, home.ID, appfleet.LaunchRequest{
		Target: own, TargetKind: domainfleet.TargetPlanet, Mission: domainfleet.MissionEspionage,
		Composition: domainfleet.Composition{unit.EspionageProbe: 1}, Percent: 100,
	}, "self-spy"); !errors.Is(err, domainfleet.ErrInvalidTarget) {
		t.Fatalf("spying on one's own colony error = %v", err)
	}
	if _, err := universe.Fleet.Launch(ctx, alice, home.ID, appfleet.LaunchRequest{
		Target: own, TargetKind: domainfleet.TargetPlanet, Mission: domainfleet.MissionAttack,
		Composition: domainfleet.Composition{unit.LightFighter: 1}, Percent: 100,
	}, "self-attack"); !errors.Is(err, domainfleet.ErrInvalidTarget) {
		t.Fatalf("attacking one's own colony error = %v", err)
	}
	// Recycling is the intentional exception: a player may time recyclers
	// behind a future battle even while the position is still empty.
	if _, err := universe.Fleet.Launch(ctx, alice, home.ID, appfleet.LaunchRequest{
		Target: own, TargetKind: domainfleet.TargetDebris, Mission: domainfleet.MissionRecycle,
		Composition: domainfleet.Composition{unit.Recycler: 1}, Percent: 100,
	}, "empty-field"); err != nil {
		t.Fatalf("prelaunching recyclers to an empty position error = %v", err)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM fleets", 1)
	_ = colony
}

// launchedAttack prepares an attack already under way.
func launchedAttack(t *testing.T, ctx context.Context, clock *appclock.Fake) (*storagesqlite.Database, *world, appauth.Principal, appfleet.Fleet) {
	t.Helper()
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
	setUnits(t, ctx, database, home.ID, "cruiser", 10)
	setUnits(t, ctx, database, target.ID, "light_fighter", 10)
	setResources(t, ctx, database, home.ID, 5000, 5000, 5000)
	setResources(t, ctx, database, target.ID, 8000, 4000, 1000)

	attack, err := universe.Fleet.Launch(ctx, alice, home.ID, appfleet.LaunchRequest{
		Target: target.Coordinate, TargetKind: domainfleet.TargetPlanet, Mission: domainfleet.MissionAttack,
		Composition: domainfleet.Composition{unit.Cruiser: 10}, Percent: 100,
	}, "attack")
	if err != nil {
		t.Fatalf("Launch(attack) error = %v", err)
	}
	return database, universe, alice, attack
}
