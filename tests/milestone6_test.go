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
	"universeatwar/internal/domain/universe"
)

func TestColonizationSurvivesARedeliveredArrival(t *testing.T) {
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC))
	database := economyDatabase(t, ctx, 1)
	universeWorld := newWorld(t, database, clock)
	alice := appauth.Principal{AccountID: 1}

	home, err := universeWorld.Economy.CreateEmpire(ctx, alice, "Alice")
	if err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	setUnits(t, ctx, database, home.ID, "colony_ship", 1)
	setResearch(t, ctx, database, 1, "astrophysics", 1)
	setResources(t, ctx, database, home.ID, 5000, 5000, 5000)

	launched, err := universeWorld.Fleet.Launch(ctx, alice, home.ID, appfleet.LaunchRequest{
		Target: universe.Coordinate{Galaxy: 1, System: 1, Position: 4}, TargetKind: domainfleet.TargetEmpty,
		Mission: domainfleet.MissionColonize, Composition: domainfleet.Composition{unit.ColonyShip: 1}, Percent: 100,
	}, "colonize")
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	clock.Set(launched.ArrivesAt)
	if _, err := universeWorld.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM planets", 2)

	replayEvent(t, ctx, database, "fleet_arrived")
	if _, err := universeWorld.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatalf("CompleteDue() after redelivery error = %v", err)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM planets", 2)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM game_event_log WHERE event_type = 'colony_founded'", 1)
}

func TestMoonCreationSurvivesARedeliveredBattle(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, alice, clock := moonBattlefield(t, 7)
	if !resolveMoonBattle(t, ctx, database, universeWorld, alice, clock, 7, "battle") {
		t.Fatal("the battle did not gather a moon")
	}

	replayEvent(t, ctx, database, "combat_resolved")
	if _, err := universeWorld.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatalf("CompleteDue() after redelivery error = %v", err)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM planets WHERE kind = 'moon'", 1)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM game_event_log WHERE event_type = 'moon_created'", 1)
}

func TestLunarPagesOfAnotherAccountStayInvisible(t *testing.T) {
	ctx := context.Background()
	database := economyDatabase(t, ctx, 2)
	universeWorld := newWorld(t, database, appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC)))
	if _, err := universeWorld.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: 1}, "Alice"); err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	if _, err := universeWorld.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: 2}, "Bob"); err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	moon := insertMoon(t, ctx, database, 2, 2, 1, 1, 1)
	setBuilding(t, ctx, database, moon, "lunar_base", 1)
	setBuilding(t, ctx, database, moon, "sensor_phalanx", 3)
	setBuilding(t, ctx, database, moon, "jump_gate", 1)
	setUnits(t, ctx, database, moon, "battlecruiser", 424242)
	setResources(t, ctx, database, moon, 0, 0, 313131)

	assertRoutesHideSecrets(t, universeWorld, appauth.Principal{AccountID: 1, Username: "player1"},
		[]string{
			"/", "/galaxy/1/1",
			pathFor("/planets/%d/phalanx", moon),
			pathFor("/planets/%d/jump", moon),
			pathFor("/planets/%d", moon),
		},
		[]string{"424242", "313131", "battlecruiser"})
}
