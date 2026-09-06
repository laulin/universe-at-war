package tests

import (
	"context"
	"errors"
	"testing"
	"time"

	appfleet "universeatwar/internal/app/fleet"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/unit"
	"universeatwar/internal/domain/universe"
)

// TestScenarioGGroupedAttackIsOneBattle proves the promise of a grouped attack:
// the allies fight a single battle, and the haul is shared by how much room each
// fleet has left rather than by who struck first.
func TestScenarioGGroupedAttackIsOneBattle(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, players := allianceUniverse(t, 3)
	alice, bob, carol := players[0], players[1], players[2]
	joinAlliance(t, ctx, universeWorld, alice, bob, "player2")

	aliceHome := homeOf(t, ctx, universeWorld, alice)
	bobHome := homeOf(t, ctx, universeWorld, bob)
	target := homeOf(t, ctx, universeWorld, carol)
	setUnits(t, ctx, database, aliceHome.ID, "cruiser", 6)
	setUnits(t, ctx, database, bobHome.ID, "cruiser", 3)
	setResources(t, ctx, database, aliceHome.ID, 50000, 50000, 50000)
	setResources(t, ctx, database, bobHome.ID, 50000, 50000, 50000)
	setUnits(t, ctx, database, target.ID, "rocket_launcher", 4)
	setBuilding(t, ctx, database, target.ID, "metal_storage", 4)
	setResources(t, ctx, database, target.ID, 40000, 20000, 6000)

	group, err := universeWorld.ACS.Create(ctx, alice, aliceHome.ID,
		fleetOf(target.Coordinate, unit.Cruiser, 6), "open")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	joined, err := universeWorld.ACS.Join(ctx, bob, group.ID, bobHome.ID,
		fleetOf(target.Coordinate, unit.Cruiser, 3), "join")
	if err != nil {
		t.Fatalf("Join() error = %v", err)
	}
	universeWorld.Clock.Set(joined.ArrivesAt)
	if _, err := universeWorld.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}

	// One battle, told once to each side, with both allies among the attackers.
	assertSingleValue(t, database, "SELECT COUNT(*) FROM game_event_log WHERE event_type = 'combat_resolved'", 1)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM reports WHERE kind = 'combat_attack'", 2)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM reports WHERE kind = 'combat_defense'", 1)
	assertSingleValue(t, database,
		`SELECT json_array_length(payload, '$.attackers') FROM reports WHERE kind = 'combat_defense'`, 2)
	assertSingleText(t, database,
		`SELECT json_extract(payload, '$.outcome') FROM reports WHERE kind = 'combat_defense'`, "attacker")
	// The defender alone learns which of its defenses were rebuilt.
	assertSingleValue(t, database,
		`SELECT COUNT(*) FROM reports WHERE kind = 'combat_attack' AND json_extract(payload, '$.rebuilt') IS NOT NULL`, 0)

	// The haul leaves the target once and lands in the two holds.
	var looted, carried int64
	if err := database.Read().QueryRowContext(ctx,
		`SELECT json_extract(payload, '$.loot_metal') + json_extract(payload, '$.loot_crystal')
		      + json_extract(payload, '$.loot_deuterium')
		 FROM game_event_log WHERE event_type = 'combat_resolved'`).Scan(&looted); err != nil {
		t.Fatal(err)
	}
	if err := database.Read().QueryRowContext(ctx,
		"SELECT SUM(metal + crystal + deuterium) FROM fleet_cargo").Scan(&carried); err != nil {
		t.Fatal(err)
	}
	if looted <= 0 || carried != looted {
		t.Fatalf("looted = %d, carried = %d", looted, carried)
	}
	assertSingleValue(t, database, "SELECT metal < 40000 FROM planet_resources WHERE planet_id = ?", 1, target.ID)

	// The larger fleet has the larger hold, so it carries the larger share.
	var aliceShare, bobShare int64
	if err := database.Read().QueryRowContext(ctx,
		"SELECT metal + crystal + deuterium FROM fleet_cargo WHERE fleet_id = 1").Scan(&aliceShare); err != nil {
		t.Fatal(err)
	}
	if err := database.Read().QueryRowContext(ctx,
		"SELECT metal + crystal + deuterium FROM fleet_cargo WHERE fleet_id = 2").Scan(&bobShare); err != nil {
		t.Fatal(err)
	}
	if bobShare <= 0 || aliceShare <= bobShare {
		t.Fatalf("shares = %d and %d, want the larger fleet to carry more", aliceShare, bobShare)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM fleets WHERE state = 'returning'", 2)
	assertSingleText(t, database, "SELECT state FROM acs_groups WHERE id = 1", "resolved")
}

// TestStationedAlliesDefendAndGoHome proves that a fleet lent to an ally fights
// for them, pays for it, and leaves when its watch ends.
func TestStationedAlliesDefendAndGoHome(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, players := allianceUniverse(t, 3)
	alice, bob, carol := players[0], players[1], players[2]
	joinAlliance(t, ctx, universeWorld, bob, carol, "player3")

	aliceHome := homeOf(t, ctx, universeWorld, alice)
	bobHome := homeOf(t, ctx, universeWorld, bob)
	target := homeOf(t, ctx, universeWorld, carol)
	setUnits(t, ctx, database, aliceHome.ID, "cruiser", 12)
	setUnits(t, ctx, database, bobHome.ID, "light_fighter", 30)
	setResources(t, ctx, database, aliceHome.ID, 50000, 50000, 50000)
	setResources(t, ctx, database, bobHome.ID, 50000, 50000, 50000)
	setUnits(t, ctx, database, target.ID, "rocket_launcher", 6)
	setResources(t, ctx, database, target.ID, 10000, 5000, 2000)

	// Only an ally may stand guard over somebody else's planet.
	if _, err := universeWorld.Fleet.Launch(ctx, alice, aliceHome.ID,
		holdRequest(target.Coordinate, unit.Cruiser, 1, universeWorld.Clock.Now().Add(6*time.Hour)),
		"intruder"); !errors.Is(err, domainfleet.ErrInvalidTarget) {
		t.Fatalf("a stranger standing guard error = %v", err)
	}

	guard, err := universeWorld.Fleet.Launch(ctx, bob, bobHome.ID,
		holdRequest(target.Coordinate, unit.LightFighter, 30, universeWorld.Clock.Now().Add(6*time.Hour)), "guard")
	if err != nil {
		t.Fatalf("Launch(hold) error = %v", err)
	}
	universeWorld.Clock.Set(guard.ArrivesAt)
	if _, err := universeWorld.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}
	assertSingleText(t, database, "SELECT state FROM fleets WHERE id = 1", "holding")
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM scheduled_events WHERE event_type = 'holding_ended' AND state = 'pending'", 1)

	attack, err := universeWorld.Fleet.Launch(ctx, alice, aliceHome.ID,
		fleetOf(target.Coordinate, unit.Cruiser, 12), "attack")
	if err != nil {
		t.Fatalf("Launch(attack) error = %v", err)
	}
	universeWorld.Clock.Set(attack.ArrivesAt)
	if _, err := universeWorld.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}

	// The guard fought: it is among the defenders and it is told about it.
	assertSingleValue(t, database,
		`SELECT json_array_length(payload, '$.defenders') FROM reports WHERE kind = 'combat_defense' LIMIT 1`, 2)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM reports WHERE kind = 'combat_defense'", 2)
	assertSingleValue(t, database,
		"SELECT COUNT(*) > 0 FROM reports WHERE recipient_player_id = 2 AND kind = 'combat_defense'", 1)
	var remaining int64
	if err := database.Read().QueryRowContext(ctx,
		"SELECT COALESCE(SUM(quantity), 0) FROM fleet_ships WHERE fleet_id = 1").Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining >= 30 {
		t.Fatalf("the guard came out of the battle untouched: %d fighters", remaining)
	}

	// A guard that survives waits out its watch, then flies home.
	if remaining == 0 {
		assertSingleText(t, database, "SELECT state FROM fleets WHERE id = 1", "destroyed")
		return
	}
	assertSingleText(t, database, "SELECT state FROM fleets WHERE id = 1", "holding")
	universeWorld.Clock.Set(universeWorld.Clock.Now().Add(6 * time.Hour))
	if _, err := universeWorld.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}
	assertSingleText(t, database, "SELECT state FROM fleets WHERE id = 1", "returning")
	universeWorld.Clock.Set(universeWorld.Clock.Now().Add(12 * time.Hour))
	if _, err := universeWorld.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}
	assertSingleText(t, database, "SELECT state FROM fleets WHERE id = 1", "completed")
	assertSingleValue(t, database,
		"SELECT quantity FROM planet_units WHERE planet_id = ? AND unit_id = 'light_fighter'",
		int(remaining), bobHome.ID)
}

func fleetOf(target universe.Coordinate, id unit.ID, quantity int64) appfleet.LaunchRequest {
	return appfleet.LaunchRequest{
		Target: target, TargetKind: domainfleet.TargetPlanet, Mission: domainfleet.MissionAttack,
		Composition: domainfleet.Composition{id: quantity}, Percent: 100,
	}
}

func holdRequest(target universe.Coordinate, id unit.ID, quantity int64, until time.Time) appfleet.LaunchRequest {
	return appfleet.LaunchRequest{
		Target: target, TargetKind: domainfleet.TargetPlanet, Mission: domainfleet.MissionHold,
		Composition: domainfleet.Composition{id: quantity}, Percent: 100, HoldUntil: until,
	}
}
