package tests

import (
	"context"
	"testing"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appfleet "universeatwar/internal/app/fleet"
	appclock "universeatwar/internal/clock"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/unit"
)

func TestActivitySnapshotCountsQueuesAndVisibleFleetTraffic(t *testing.T) {
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC))
	database := economyDatabase(t, ctx, 2)
	game := newWorld(t, database, clock)
	alice := appauth.Principal{AccountID: 1}
	bob := appauth.Principal{AccountID: 2}
	aliceHome, err := game.Economy.CreateEmpire(ctx, alice, "Alice")
	if err != nil {
		t.Fatal(err)
	}
	bobHome, err := game.Economy.CreateEmpire(ctx, bob, "Bob")
	if err != nil {
		t.Fatal(err)
	}

	setResources(t, ctx, database, aliceHome.ID, 50_000_000, 50_000_000, 50_000_000)
	setBuilding(t, ctx, database, aliceHome.ID, "metal_storage", 10)
	setBuilding(t, ctx, database, aliceHome.ID, "crystal_storage", 10)
	setBuilding(t, ctx, database, aliceHome.ID, "deuterium_tank", 10)
	setBuilding(t, ctx, database, aliceHome.ID, "research_lab", 1)
	setBuilding(t, ctx, database, aliceHome.ID, "shipyard", 2)
	setResearch(t, ctx, database, 1, "combustion_drive", 2)
	setUnits(t, ctx, database, aliceHome.ID, string(unit.LightFighter), 2)
	if _, err := game.Economy.EnqueueBuilding(ctx, alice, aliceHome.ID, "metal_mine", "building"); err != nil {
		t.Fatal(err)
	}
	if _, err := game.Research.EnqueueResearch(ctx, alice, aliceHome.ID, "energy_technology", "research"); err != nil {
		t.Fatal(err)
	}
	if _, err := game.Shipyard.OrderFamily(ctx, alice, aliceHome.ID, unit.SmallCargo, unit.Ship, 3, "ships"); err != nil {
		t.Fatal(err)
	}
	if _, err := game.Shipyard.OrderFamily(ctx, alice, aliceHome.ID, unit.RocketLauncher, unit.Defense, 4, "defenses"); err != nil {
		t.Fatal(err)
	}
	attack, err := game.Fleet.Launch(ctx, alice, aliceHome.ID, appfleet.LaunchRequest{
		Target: bobHome.Coordinate, TargetKind: domainfleet.TargetPlanet, Mission: domainfleet.MissionAttack,
		Composition: domainfleet.Composition{unit.LightFighter: 2}, Percent: 100,
	}, "attack")
	if err != nil {
		t.Fatal(err)
	}

	aliceActivity, err := game.Activity.Snapshot(ctx, alice)
	if err != nil {
		t.Fatal(err)
	}
	aliceBody := aliceActivity.Bodies[aliceHome.ID]
	if aliceBody.Buildings != 1 || aliceBody.Researches != 1 || aliceBody.Ships != 3 ||
		aliceBody.Defenses != 4 || aliceBody.OutboundFleets != 1 || aliceBody.IncomingAttacks != 0 {
		t.Fatalf("Alice activity = %+v", aliceBody)
	}
	if len(aliceActivity.Incoming) != 0 {
		t.Fatalf("Alice sees foreign traffic not aimed at her: %+v", aliceActivity.Incoming)
	}

	bobActivity, err := game.Activity.Snapshot(ctx, bob)
	if err != nil {
		t.Fatal(err)
	}
	bobBody := bobActivity.Bodies[bobHome.ID]
	if bobBody.IncomingAttacks != 1 || bobBody.OutboundFleets != 0 {
		t.Fatalf("Bob activity = %+v", bobBody)
	}
	if len(bobActivity.Incoming) != 1 {
		t.Fatalf("Bob incoming attacks = %+v", bobActivity.Incoming)
	}
	approach := bobActivity.Incoming[0]
	if approach.FleetID != attack.ID || approach.AttackerName != "Alice" ||
		approach.TargetPlanetID != bobHome.ID || approach.Composition[unit.LightFighter] != 2 ||
		approach.Origin != aliceHome.Coordinate || approach.Target != bobHome.Coordinate {
		t.Fatalf("incoming approach = %+v", approach)
	}
}
