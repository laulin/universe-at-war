package tests

import (
	"context"
	"errors"
	"testing"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appeconomy "universeatwar/internal/app/economy"
	appfleet "universeatwar/internal/app/fleet"
	"universeatwar/internal/domain/building"
	domaineconomy "universeatwar/internal/domain/economy"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/unit"
)

func TestLunarTransportAndDeploymentKeepThePlanetSeparate(t *testing.T) {
	ctx := context.Background()
	db, w, clock, moonID := phalanxUniverse(t)
	bob := appauth.Principal{AccountID: 2}
	setUnits(t, ctx, db, 2, "small_cargo", 2)
	setResources(t, ctx, db, 2, 5000, 5000, 5000)
	setResources(t, ctx, db, moonID, 500000, 0, 10000)
	for i, mission := range []domainfleet.Mission{domainfleet.MissionTransport, domainfleet.MissionDeploy} {
		flight, err := w.Fleet.Launch(ctx, bob, 2, appfleet.LaunchRequest{
			Target: coordinateOf(t, 1, 1, 1), TargetKind: domainfleet.TargetMoon, Mission: mission,
			Composition: domainfleet.Composition{unit.SmallCargo: 1}, Cargo: domaineconomy.Resources{Metal: 100}, Percent: 100,
		}, string(mission))
		if err != nil {
			t.Fatalf("Launch(%s to moon): %v", mission, err)
		}
		assertSingleValue(t, db, "SELECT target_planet_id FROM fleets WHERE id = ?", int(moonID), flight.ID)
		clock.Set(flight.ArrivesAt)
		if _, err := w.Events.CompleteDue(ctx, 50); err != nil {
			t.Fatal(err)
		}
		assertSingleValue(t, db, "SELECT metal FROM planet_resources WHERE planet_id = ?", 500000+(i+1)*100, moonID)
		if mission == domainfleet.MissionTransport {
			clock.Set(*flight.ReturnsAt)
			if _, err := w.Events.CompleteDue(ctx, 50); err != nil {
				t.Fatal(err)
			}
		}
	}
	assertSingleValue(t, db, "SELECT quantity FROM planet_units WHERE planet_id = ? AND unit_id = 'small_cargo'", 1, moonID)
	assertSingleValue(t, db, "SELECT quantity FROM planet_units WHERE planet_id = 2 AND unit_id = 'small_cargo'", 1)
	// A moon can also launch the return deployment to the planet at the same coordinates.
	back, err := w.Fleet.Launch(ctx, bob, moonID, appfleet.LaunchRequest{
		Target: coordinateOf(t, 1, 1, 1), TargetKind: domainfleet.TargetPlanet, Mission: domainfleet.MissionDeploy,
		Composition: domainfleet.Composition{unit.SmallCargo: 1}, Percent: 100,
	}, "back-to-parent")
	if err != nil {
		t.Fatal(err)
	}
	clock.Set(back.ArrivesAt)
	if _, err := w.Events.CompleteDue(ctx, 50); err != nil {
		t.Fatal(err)
	}
	assertSingleValue(t, db, "SELECT quantity FROM planet_units WHERE planet_id = 2 AND unit_id = 'small_cargo'", 2)
	assertSingleValue(t, db, "SELECT COALESCE(SUM(quantity), 0) FROM planet_units WHERE planet_id = ? AND unit_id = 'small_cargo'", 0, moonID)
}

func TestLunarDestinationChecksOwnershipAndExistence(t *testing.T) {
	ctx := context.Background()
	db, w, _, moonID := phalanxUniverse(t)
	setUnits(t, ctx, db, 1, "cruiser", 1)
	for _, test := range []struct {
		name     string
		mission  domainfleet.Mission
		position int
	}{
		{"foreign deployment", domainfleet.MissionDeploy, 1},
		{"missing moon", domainfleet.MissionTransport, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := w.Fleet.Launch(ctx, appauth.Principal{AccountID: 1}, 1, appfleet.LaunchRequest{
				Target: coordinateOf(t, 1, 1, test.position), TargetKind: domainfleet.TargetMoon, Mission: test.mission,
				Composition: domainfleet.Composition{unit.Cruiser: 1}, Percent: 100,
			}, test.name)
			if !errors.Is(err, domainfleet.ErrInvalidTarget) {
				t.Fatalf("Launch(): %v", err)
			}
		})
	}
	assertSingleValue(t, db, "SELECT COUNT(*) FROM fleets", 0)
	assertSingleValue(t, db, "SELECT deuterium FROM planet_resources WHERE planet_id = ?", 10000, moonID)
}

func TestLunarCombatNeverUsesPlanetaryDefendersOrCreatesASatellite(t *testing.T) {
	ctx := context.Background()
	db, w, clock, moonID := phalanxUniverse(t)
	setUnits(t, ctx, db, 1, "cruiser", 10)
	setUnits(t, ctx, db, 2, "deathstar", 100)
	setUnits(t, ctx, db, moonID, "light_fighter", 1)
	flight, err := w.Fleet.Launch(ctx, appauth.Principal{AccountID: 1}, 1, appfleet.LaunchRequest{
		Target: coordinateOf(t, 1, 1, 1), TargetKind: domainfleet.TargetMoon, Mission: domainfleet.MissionAttack,
		Composition: domainfleet.Composition{unit.Cruiser: 10}, Percent: 100,
	}, "lunar-attack")
	if err != nil {
		t.Fatal(err)
	}
	clock.Set(flight.ArrivesAt)
	if _, err := w.Events.CompleteDue(ctx, 50); err != nil {
		t.Fatal(err)
	}
	assertSingleValue(t, db, "SELECT quantity FROM planet_units WHERE planet_id = 2 AND unit_id = 'deathstar'", 100)
	assertSingleValue(t, db, "SELECT COALESCE(SUM(quantity), 0) FROM planet_units WHERE planet_id = ? AND unit_id = 'light_fighter'", 0, moonID)
	assertSingleText(t, db, "SELECT json_extract(payload, '$.moon.outcome') FROM reports WHERE kind = 'combat_attack'", "target_not_planet")
	assertSingleValue(t, db, "SELECT json_extract(payload, '$.moon_chance') FROM reports WHERE kind = 'combat_attack'", 0)
	assertSingleText(t, db, "SELECT json_extract(payload, '$.target_kind') FROM reports WHERE kind = 'combat_attack'", "moon")
	assertSingleValue(t, db, "SELECT COUNT(*) FROM planets WHERE kind = 'moon'", 1)
}

func TestFullMoonCanExpandAndOperateItsOwnShipyard(t *testing.T) {
	ctx := context.Background()
	db, w, clock, moonID := phalanxUniverse(t)
	bob := appauth.Principal{AccountID: 2}
	setResources(t, ctx, db, moonID, 1000000, 1000000, 1000000)
	setResearch(t, ctx, db, 2, "combustion_drive", 1)
	// An old full moon can recover without deleting buildings or resources.
	if _, err := db.Write().ExecContext(ctx, "UPDATE planets SET used_fields = 10 WHERE id = ?", moonID); err != nil {
		t.Fatal(err)
	}
	base, err := w.Economy.EnqueueBuilding(ctx, bob, moonID, building.LunarBase, "expand")
	if err != nil {
		t.Fatal(err)
	}
	clock.Set(base.CompletesAt)
	if _, err := w.Events.CompleteDue(ctx, 50); err != nil {
		t.Fatal(err)
	}
	assertSingleValue(t, db, "SELECT used_fields FROM planets WHERE id = ?", 11, moonID)
	assertSingleValue(t, db, "SELECT total_fields FROM planets WHERE id = ?", 13, moonID)
	// Complete two local robot upgrades and the shipyard. The base can expand again when full.
	for i, id := range []building.ID{building.RoboticsFactory, building.RoboticsFactory, building.LunarBase, building.Shipyard} {
		entry, err := w.Economy.EnqueueBuilding(ctx, bob, moonID, id, string(id)+string(rune('a'+i)))
		if err != nil {
			t.Fatalf("EnqueueBuilding(%s): %v", id, err)
		}
		clock.Set(entry.CompletesAt)
		if _, err := w.Events.CompleteDue(ctx, 50); err != nil {
			t.Fatal(err)
		}
	}
	order, err := w.Shipyard.Order(ctx, bob, moonID, unit.LightFighter, 1, "lunar-fighter")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Economy.EnqueueBuilding(ctx, bob, moonID, building.Shipyard, "busy-yard"); !errors.Is(err, appeconomy.ErrFacilityBusy) {
		t.Fatalf("an active lunar shipyard accepted an upgrade: %v", err)
	}
	if _, err := w.Shipyard.Order(ctx, bob, moonID, unit.InterplanetaryMissile, 1, "no-silo"); err == nil {
		t.Fatal("a moon produced an interplanetary missile")
	}
	clock.Advance(order.UnitDuration + time.Second)
	if _, err := w.Events.CompleteDue(ctx, 50); err != nil {
		t.Fatal(err)
	}
	assertSingleValue(t, db, "SELECT quantity FROM planet_units WHERE planet_id = ? AND unit_id = 'light_fighter'", 1, moonID)
	assertSingleValue(t, db, "SELECT COALESCE(SUM(quantity), 0) FROM planet_units WHERE planet_id = 2 AND unit_id = 'light_fighter'", 0)
	// Recreating services preserves stock and the facility levels of the same saved moon.
	restarted := newWorld(t, db, clock)
	moon, err := restarted.Economy.Planet(ctx, bob, moonID)
	if err != nil {
		t.Fatal(err)
	}
	stock := moon.Stock
	clock.Advance(24 * time.Hour)
	moon, err = restarted.Economy.Planet(ctx, bob, moonID)
	if err != nil || moon.Stock != stock || moon.Levels[building.Shipyard] != 1 {
		t.Fatalf("saved moon changed: %+v, %v", moon, err)
	}
}

func TestMoonLifecycleFromTwentyPercentBattleToPersistentJumpGate(t *testing.T) {
	ctx := context.Background()
	db, w, alice, clock := battlefieldWithMoonChance(t, .2, 1, 2500)
	if !resolveMoonBattle(t, ctx, db, w, alice, clock, 2, "formation") {
		t.Fatal("the deterministic battle did not create Bob's moon")
	}
	bob := appauth.Principal{AccountID: 2}
	const moonID = int64(3)
	setResearch(t, ctx, db, 2, "hyperspace_technology", 7)
	setResearch(t, ctx, db, 2, "combustion_drive", 1)
	for _, id := range []string{"metal_storage", "crystal_storage", "deuterium_tank"} {
		setBuilding(t, ctx, db, 2, id, 14)
	}
	setResources(t, ctx, db, 2, 5000000, 6000000, 4000000)
	setUnits(t, ctx, db, 2, "large_cargo", 500)
	supply, err := w.Fleet.Launch(ctx, bob, 2, appfleet.LaunchRequest{
		Target: coordinateOf(t, 1, 1, 1), TargetKind: domainfleet.TargetMoon, Mission: domainfleet.MissionTransport,
		Composition: domainfleet.Composition{unit.LargeCargo: 500}, Percent: 100,
		Cargo: domaineconomy.Resources{Metal: 3000000, Crystal: 5000000, Deuterium: 3000000},
	}, "finance-the-moon")
	if err != nil {
		t.Fatal(err)
	}
	clock.Set(supply.ArrivesAt)
	if _, err := w.Events.CompleteDue(ctx, 100); err != nil {
		t.Fatal(err)
	}
	for i, id := range []building.ID{building.LunarBase, building.RoboticsFactory, building.RoboticsFactory, building.Shipyard, building.LunarBase, building.SensorPhalanx, building.JumpGate} {
		entry, err := w.Economy.EnqueueBuilding(ctx, bob, moonID, id, "lifecycle-"+string(rune('a'+i)))
		if err != nil {
			t.Fatalf("construct %s: %v", id, err)
		}
		clock.Set(entry.CompletesAt)
		if _, err := w.Events.CompleteDue(ctx, 100); err != nil {
			t.Fatal(err)
		}
	}
	order, err := w.Shipyard.Order(ctx, bob, moonID, unit.LightFighter, 3, "lifecycle-fighters")
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(3*order.UnitDuration + time.Second)
	if _, err := w.Events.CompleteDue(ctx, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Phalanx.Scan(ctx, bob, moonID, coordinateOf(t, 1, 1, 8)); err != nil {
		t.Fatal(err)
	}
	colony := insertColony(t, ctx, db, 2, "Colonie", 1, 2, 4)
	otherMoon := insertMoon(t, ctx, db, 2, colony, 1, 2, 4)
	setBuilding(t, ctx, db, otherMoon, "lunar_base", 1)
	setBuilding(t, ctx, db, otherMoon, "jump_gate", 1)
	setResources(t, ctx, db, otherMoon, 123, 456, 789)
	before, err := w.Economy.Planet(ctx, bob, moonID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.JumpGate.Jump(ctx, bob, moonID, otherMoon, domainfleet.Composition{unit.LightFighter: 2}, "lifecycle-jump"); err != nil {
		t.Fatal(err)
	}
	assertSingleValue(t, db, "SELECT quantity FROM planet_units WHERE planet_id = ? AND unit_id = 'light_fighter'", 1, moonID)
	assertSingleValue(t, db, "SELECT quantity FROM planet_units WHERE planet_id = ? AND unit_id = 'light_fighter'", 2, otherMoon)
	restarted := newWorld(t, db, clock)
	gate, err := restarted.JumpGate.Overview(ctx, bob, moonID)
	if err != nil || gate.Ready || gate.Moon.Stock != before.Stock {
		t.Fatalf("restarted gate changed cooldown or cargo: %+v, %v", gate, err)
	}
	other, err := restarted.Economy.Planet(ctx, bob, otherMoon)
	if err != nil || other.Stock != (domaineconomy.Resources{Metal: 123, Crystal: 456, Deuterium: 789}) {
		t.Fatalf("jump moved resources: %+v, %v", other.Stock, err)
	}
}
