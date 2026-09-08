package tests

import (
	"context"
	"testing"
	"time"

	appai "universeatwar/internal/app/ai"
	appauth "universeatwar/internal/app/authentication"
	appfleet "universeatwar/internal/app/fleet"
	appclock "universeatwar/internal/clock"
	domainai "universeatwar/internal/domain/ai"
	"universeatwar/internal/domain/building"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/unit"
)

// TestScenarioHAnOfflineArtificialPlayerReactsLegitimately proves an artificial
// player suffers an attack while it sleeps without answering it instantly, and
// picks its empire back up only when its hours come round again.
func TestScenarioHAnOfflineArtificialPlayerReactsLegitimately(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, admin := aiUniverse(t)
	setClock(t, universeWorld.Clock, time.Date(2042, time.September, 10, 22, 0, 0, 0, time.UTC))

	profile, err := universeWorld.AI.Create(ctx, admin, appai.Request{
		Name: "Dormeur", Archetype: domainai.CautiousMiner,
		Window: domainai.Window{Start: 8, End: 20}, Interval: 30 * time.Minute,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	setUnits(t, ctx, database, 2, "light_fighter", 10)
	setResources(t, ctx, database, 2, 20000, 10000, 5000)
	// The human next door is armed and awake at any hour, as humans are.
	alice := appauth.Principal{AccountID: 1}
	setUnits(t, ctx, database, 1, "cruiser", 30)
	setResources(t, ctx, database, 1, 90000, 90000, 90000)

	target, err := universeWorld.Economy.Planet(ctx, appauth.Principal{AccountID: profile.AccountID}, 2)
	if err != nil {
		t.Fatalf("Planet() error = %v", err)
	}
	attack, err := universeWorld.Fleet.Launch(ctx, alice, 1, appfleet.LaunchRequest{
		Target: target.Coordinate, TargetKind: domainfleet.TargetPlanet, Mission: domainfleet.MissionAttack,
		Composition: domainfleet.Composition{unit.Cruiser: 30}, Percent: 100,
	}, "night-raid")
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}

	// The night runs its course: reflections fire, and every one of them sleeps.
	setClock(t, universeWorld.Clock, attack.ArrivesAt)
	for cycle := 0; cycle < 8; cycle++ {
		universeWorld.Clock.Advance(30 * time.Minute)
		if _, err := universeWorld.Events.CompleteDue(ctx, 100); err != nil {
			t.Fatalf("CompleteDue() error = %v", err)
		}
		if _, err := universeWorld.Brain.ThinkDue(ctx, 10); err != nil {
			t.Fatalf("ThinkDue() error = %v", err)
		}
	}
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM reports WHERE recipient_player_id = 2 AND kind = 'combat_defense'", 1)
	// It was hit, it lost, and it did nothing about it during the night.
	assertSingleValue(t, database, "SELECT COUNT(*) FROM fleets WHERE owner_player_id = 2", 0)
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM ai_decisions WHERE outcome = 'done'", 0)
	assertSingleValue(t, database,
		"SELECT COUNT(*) > 0 FROM ai_decisions WHERE action = 'sleep'", 1)

	// Morning comes and the empire starts moving again, on its own resources.
	setClock(t, universeWorld.Clock, time.Date(2042, time.September, 11, 9, 0, 0, 0, time.UTC))
	for cycle := 0; cycle < 4; cycle++ {
		universeWorld.Clock.Advance(30 * time.Minute)
		if _, err := universeWorld.Events.CompleteDue(ctx, 100); err != nil {
			t.Fatalf("CompleteDue() error = %v", err)
		}
		if _, err := universeWorld.Brain.ThinkDue(ctx, 10); err != nil {
			t.Fatalf("ThinkDue() error = %v", err)
		}
	}
	assertSingleValue(t, database, "SELECT COUNT(*) > 0 FROM ai_decisions WHERE outcome = 'done'", 1)
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM planet_resources WHERE planet_id = 2 AND (metal < 0 OR crystal < 0 OR deuterium < 0)", 0)
}

// TestArtificialPlayerPaysTheSamePriceAsAHuman proves the two go through the
// very same catalogue, the very same purse and the very same waiting.
func TestArtificialPlayerPaysTheSamePriceAsAHuman(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, admin := aiUniverse(t)
	setClock(t, universeWorld.Clock, time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC))
	if _, err := universeWorld.AI.Create(ctx, admin, appai.Request{
		Name: "Kepler", Archetype: domainai.CautiousMiner,
		Window: domainai.Window{Start: 0, End: 0}, Interval: 5 * time.Minute,
	}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	setResources(t, ctx, database, 2, 5000, 5000, 5000)
	think(t, ctx, universeWorld)

	// The human raises the same building from the same level, right away.
	human := appauth.Principal{AccountID: 1}
	setResources(t, ctx, database, 1, 5000, 5000, 5000)
	settled, err := universeWorld.Economy.Planet(ctx, human, 1)
	if err != nil {
		t.Fatalf("Planet() error = %v", err)
	}
	before := settled.Stock.Metal
	if _, err := universeWorld.Economy.EnqueueBuilding(ctx, human, 1, building.MetalMine, "human-mine"); err != nil {
		t.Fatalf("EnqueueBuilding() error = %v", err)
	}
	settled, err = universeWorld.Economy.Planet(ctx, human, 1)
	if err != nil {
		t.Fatalf("Planet() error = %v", err)
	}
	after := settled.Stock.Metal

	var artificialCost, humanCost, artificialSeconds, humanSeconds int64
	if err := database.Read().QueryRowContext(ctx, `
		SELECT metal_cost, strftime('%s', completes_at) - strftime('%s', started_at)
		FROM building_queue WHERE planet_id = 2 AND building_id = 'metal_mine' ORDER BY id LIMIT 1
	`).Scan(&artificialCost, &artificialSeconds); err != nil {
		t.Fatal(err)
	}
	if err := database.Read().QueryRowContext(ctx, `
		SELECT metal_cost, strftime('%s', completes_at) - strftime('%s', started_at)
		FROM building_queue WHERE planet_id = 1 AND building_id = 'metal_mine' ORDER BY id LIMIT 1
	`).Scan(&humanCost, &humanSeconds); err != nil {
		t.Fatal(err)
	}
	if artificialCost != humanCost || artificialSeconds != humanSeconds {
		t.Fatalf("the machine paid %d over %d s and the human %d over %d s",
			artificialCost, artificialSeconds, humanCost, humanSeconds)
	}
	// Neither of them built anything on credit: both purses lost the price.
	assertSingleValue(t, database, "SELECT metal FROM planet_resources WHERE planet_id = 2", int(5000-artificialCost))
	if before-after != humanCost {
		t.Fatalf("the human paid %d for a mine priced at %d", before-after, humanCost)
	}
}

// TestScheduleKeepsTheWorkerAsleepBetweenReflections proves an artificial
// player costs nothing between two reflections: the next due event is its own,
// it lies in the future, and nothing is owed in the meantime.
func TestScheduleKeepsTheWorkerAsleepBetweenReflections(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, admin := aiUniverse(t)
	setClock(t, universeWorld.Clock, time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC))
	if _, err := universeWorld.AI.Create(ctx, admin, appai.Request{
		Name: "Kepler", Archetype: domainai.CautiousMiner,
		Window: domainai.Window{Start: 0, End: 0}, Interval: 30 * time.Minute,
	}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	think(t, ctx, universeWorld)

	// Nothing is due any more, and nothing is owed.
	processed, err := universeWorld.Events.CompleteDue(ctx, 100)
	if err != nil || processed != 0 {
		t.Fatalf("CompleteDue() = %d, %v", processed, err)
	}
	thought, err := universeWorld.Brain.ThinkDue(ctx, 10)
	if err != nil || thought != 0 {
		t.Fatalf("ThinkDue() = %d, %v", thought, err)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM ai_profiles WHERE due_think_at IS NOT NULL", 0)

	// Whatever comes next lies in the future, so the worker sleeps instead of
	// spinning, and the next reflection is a whole interval away.
	dueAt, ok, err := universeWorld.Events.NextDue(ctx)
	if err != nil || !ok {
		t.Fatalf("NextDue() = %v %v %v", dueAt, ok, err)
	}
	if !dueAt.After(universeWorld.Clock.Now().UTC()) {
		t.Fatalf("the worker would spin: the next event is due at %v", dueAt)
	}
	var nextThink string
	if err := database.Read().QueryRowContext(ctx,
		"SELECT due_at FROM scheduled_events WHERE event_type = 'ai_think' AND state = 'pending'").
		Scan(&nextThink); err != nil {
		t.Fatal(err)
	}
	planned, err := time.Parse(time.RFC3339Nano, nextThink)
	if err != nil {
		t.Fatal(err)
	}
	if delay := planned.Sub(universeWorld.Clock.Now().UTC()); delay < 20*time.Minute || delay > 40*time.Minute {
		t.Fatalf("the next reflection falls outside the jitter band: %v", delay)
	}
}

// BenchmarkArtificialReflections measures a long run of reflections, which is
// what a universe full of artificial players actually costs.
func BenchmarkArtificialReflections(b *testing.B) {
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC))
	database := benchmarkUniverse(b, ctx)
	universeWorld := newWorld(b, database, clock)
	admin := appauth.Principal{AccountID: 1, Roles: []appauth.Role{appauth.RoleAdmin}}
	if _, err := universeWorld.AI.Create(ctx, admin, appai.Request{
		Name: "Kepler", Archetype: domainai.CautiousMiner,
		Window: domainai.Window{Start: 0, End: 0}, Interval: 5 * time.Minute,
	}); err != nil {
		b.Fatal(err)
	}
	if _, err := database.Write().ExecContext(ctx,
		"UPDATE planet_resources SET metal = 100000, crystal = 100000, deuterium = 100000"); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for b.Loop() {
		universeWorld.Clock.Advance(5 * time.Minute)
		if _, err := universeWorld.Events.CompleteDue(ctx, 100); err != nil {
			b.Fatal(err)
		}
		if _, err := universeWorld.Brain.ThinkDue(ctx, 10); err != nil {
			b.Fatal(err)
		}
	}
}
