package tests

import (
	"context"
	"strings"
	"testing"
	"time"

	appai "universeatwar/internal/app/ai"
	domainai "universeatwar/internal/domain/ai"
	storagesqlite "universeatwar/internal/storage/sqlite"
)

// TestArtificialPlayerDevelopsAnEmptyEmpire proves an artificial player starts
// with nothing, pays for everything and waits like anybody else.
func TestArtificialPlayerDevelopsAnEmptyEmpire(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, admin := aiUniverse(t)
	setClock(t, universeWorld.Clock, time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC))

	profile, err := universeWorld.AI.Create(ctx, admin, appai.Request{
		Name: "Kepler", Archetype: domainai.CautiousMiner,
		Window: domainai.Window{Start: 0, End: 0}, Interval: 5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM planet_buildings WHERE planet_id = 2", 0)

	// Half a day of reflections, each one paid out of the same purse a human
	// would have.
	for cycle := 0; cycle < 120; cycle++ {
		universeWorld.Clock.Advance(5 * time.Minute)
		if _, err := universeWorld.Events.CompleteDue(ctx, 100); err != nil {
			t.Fatalf("CompleteDue() error = %v", err)
		}
		if _, err := universeWorld.Brain.ThinkDue(ctx, 10); err != nil {
			t.Fatalf("ThinkDue() error = %v", err)
		}
	}

	var mines, buildings int
	if err := database.Read().QueryRowContext(ctx, `
		SELECT COALESCE(SUM(level), 0), COUNT(*) FROM planet_buildings WHERE planet_id = 2
	`).Scan(&mines, &buildings); err != nil {
		t.Fatal(err)
	}
	if mines < 4 || buildings < 2 {
		t.Fatalf("after half a day the empire holds %d levels over %d buildings", mines, buildings)
	}
	// It never created anything: every level was paid for and queued.
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM planet_resources WHERE metal < 0 OR crystal < 0 OR deuterium < 0", 0)
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM building_queue WHERE planet_id = 2 AND completes_at <= started_at", 0)
	assertSingleValue(t, database,
		"SELECT COUNT(*) > 0 FROM building_queue WHERE planet_id = 2 AND state = 'completed'", 1)
	// And it wrote down what it did, reflection after reflection.
	assertSingleValue(t, database,
		"SELECT COUNT(*) > 0 FROM ai_decisions WHERE outcome = 'done' AND action LIKE 'build %'", 1)

	inspected, err := universeWorld.AI.Inspect(ctx, admin, profile.PlayerID)
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if inspected.LastThinkAt == nil || len(inspected.Decisions) == 0 {
		t.Fatalf("the diary is empty: %+v", inspected)
	}
}

func TestArtificialPlayerNamesANewColonyWithoutOverwritingChosenNames(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, admin := aiUniverse(t)
	setClock(t, universeWorld.Clock, time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC))
	profile, err := universeWorld.AI.Create(ctx, admin, appai.Request{
		Name: "Kepler", Archetype: domainai.CautiousMiner,
		Window: domainai.Window{Start: 0, End: 0}, Interval: 5 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	pinSeed(t, ctx, database, 42, universeWorld.Clock.Now().Add(5*time.Minute))
	colony := insertColony(t, ctx, database, profile.PlayerID, "Colonie", 1, 2, 4)
	chosen := insertColony(t, ctx, database, profile.PlayerID, "Forge", 1, 2, 5)

	universeWorld.Clock.Advance(5 * time.Minute)
	if _, err := universeWorld.Events.CompleteDue(ctx, 100); err != nil {
		t.Fatal(err)
	}
	if thought, err := universeWorld.Brain.ThinkDue(ctx, 10); err != nil || thought != 1 {
		t.Fatalf("ThinkDue() = %d, %v", thought, err)
	}

	assertSingleText(t, database, "SELECT name FROM planets WHERE id = ?", "Kepler 1:2:4", colony)
	assertSingleText(t, database, "SELECT name FROM planets WHERE id = ?", "Forge", chosen)
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM ai_decisions WHERE body_id = ? AND action = 'rename 1:2:4' AND outcome = 'done'", 1, colony)
}

func TestArtificialPlayerColonizesThenDevelopsTheNewPlanet(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, admin := aiUniverse(t)
	setClock(t, universeWorld.Clock, time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC))
	profile, err := universeWorld.AI.Create(ctx, admin, appai.Request{
		Name: "Pionnier", Archetype: domainai.Logistician,
		Window: domainai.Window{Start: 0, End: 0}, Interval: 5 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	setResearch(t, ctx, database, profile.PlayerID, "astrophysics", 1)
	setResearch(t, ctx, database, profile.PlayerID, "computer_technology", 2)
	setUnits(t, ctx, database, 2, "colony_ship", 1)
	setResources(t, ctx, database, 2, 500000, 500000, 500000)

	think(t, ctx, universeWorld)
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM fleets WHERE owner_player_id = ? AND mission = 'colonize'", 1, profile.PlayerID)
	var arrivesText string
	if err := database.Read().QueryRowContext(ctx, `
		SELECT arrives_at FROM fleets WHERE owner_player_id = ? AND mission = 'colonize'
	`, profile.PlayerID).Scan(&arrivesText); err != nil {
		t.Fatal(err)
	}
	arrivesAt, err := time.Parse(time.RFC3339Nano, arrivesText)
	if err != nil {
		t.Fatal(err)
	}
	setClock(t, universeWorld.Clock, arrivesAt)
	if _, err := universeWorld.Events.CompleteDue(ctx, 200); err != nil {
		t.Fatal(err)
	}
	var colonyID int64
	if err := database.Read().QueryRowContext(ctx,
		"SELECT id FROM planets WHERE owner_player_id = ? AND id <> 2", profile.PlayerID).Scan(&colonyID); err != nil {
		t.Fatal(err)
	}
	setResources(t, ctx, database, colonyID, 100000, 100000, 100000)
	think(t, ctx, universeWorld)
	assertSingleValue(t, database,
		"SELECT COUNT(*) > 0 FROM building_queue WHERE planet_id = ?", 1, colonyID)
	assertSingleValue(t, database, `
		SELECT COUNT(*) > 0 FROM ai_decisions
		WHERE body_id = ? AND action LIKE 'build %' AND outcome = 'done'`, 1, colonyID)
}

func TestArtificialPlayerDecisionDiaryIsBounded(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, admin := aiUniverse(t)
	setClock(t, universeWorld.Clock, time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC))
	profile, err := universeWorld.AI.Create(ctx, admin, appai.Request{
		Name: "Archiviste", Archetype: domainai.CautiousMiner,
		Window: domainai.Window{Start: 0, End: 0}, Interval: 5 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 300; index++ {
		if _, err := database.Write().ExecContext(ctx, `
			INSERT INTO ai_decisions(player_id, decided_at, layer, action, outcome, reason)
			VALUES (?, ?, 'strategic', 'sleep', 'skipped', 'fixture')
		`, profile.PlayerID, universeWorld.Clock.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			t.Fatal(err)
		}
	}
	think(t, ctx, universeWorld)
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM ai_decisions WHERE player_id = ?", 250, profile.PlayerID)
}

// TestReflectionIsReproducibleAndArchetypesDiffer proves the same state and the
// same seed give the same decision, and that two characters do not.
func TestReflectionIsReproducibleAndArchetypesDiffer(t *testing.T) {
	ctx := context.Background()
	run := func(archetype domainai.Archetype, cycles int) []string {
		database, universeWorld, admin := aiUniverse(t)
		setClock(t, universeWorld.Clock, time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC))
		if _, err := universeWorld.AI.Create(ctx, admin, appai.Request{
			Name: "Twin", Archetype: archetype,
			Window: domainai.Window{Start: 0, End: 0}, Interval: 5 * time.Minute,
		}); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		// The seed of a new player is drawn from the system; pinning it is what
		// turns "the same universe" into a statement one can actually test.
		pinSeed(t, ctx, database, 42, universeWorld.Clock.Now().Add(5*time.Minute))
		// A well-supplied yard, so what it produces is a choice of character
		// rather than an accident of poverty.
		setBuilding(t, ctx, database, 2, "shipyard", 8)
		setBuilding(t, ctx, database, 2, "solar_plant", 20)
		setBuilding(t, ctx, database, 2, "robotics_factory", 6)
		setBuilding(t, ctx, database, 2, "nanite_factory", 4)
		// With a drive available, ships and defences are both real options.
		setResearch(t, ctx, database, 2, "combustion_drive", 2)
		for cycle := 0; cycle < cycles; cycle++ {
			setResources(t, ctx, database, 2, 30000, 20000, 10000)
			universeWorld.Clock.Advance(5 * time.Minute)
			if _, err := universeWorld.Events.CompleteDue(ctx, 100); err != nil {
				t.Fatalf("CompleteDue() error = %v", err)
			}
			if _, err := universeWorld.Brain.ThinkDue(ctx, 10); err != nil {
				t.Fatalf("ThinkDue() error = %v", err)
			}
		}
		rows, err := database.Read().QueryContext(ctx,
			"SELECT action FROM ai_decisions WHERE outcome = 'done' ORDER BY id")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var actions []string
		for rows.Next() {
			var action string
			if err := rows.Scan(&action); err != nil {
				t.Fatal(err)
			}
			actions = append(actions, action)
		}
		return actions
	}

	miner, second := run(domainai.CautiousMiner, 6), run(domainai.CautiousMiner, 6)
	if len(miner) == 0 {
		t.Fatal("the miner decided nothing at all")
	}
	if !sameActions(miner, second) {
		t.Fatalf("two identical universes diverged: %v and %v", miner, second)
	}

	// A turtle points its yard at the ground far more often than a raider does.
	if defensive(run(domainai.Turtle, 60)) <= defensive(run(domainai.Raider, 60)) {
		t.Fatal("a turtle and a raider built the same defences")
	}
}

// defensive counts the production orders that point at the ground.
func defensive(actions []string) int {
	count := 0
	for _, action := range actions {
		if strings.Contains(action, "rocket_launcher") || strings.Contains(action, "light_laser") {
			count++
		}
	}
	return count
}

func sameActions(first, second []string) bool {
	if len(first) != len(second) {
		return false
	}
	for index := range first {
		if first[index] != second[index] {
			return false
		}
	}
	return true
}

// pinSeed fixes the seed and the first reflection of every artificial player,
// so two runs of the same scenario really are the same universe.
func pinSeed(t *testing.T, ctx context.Context, database *storagesqlite.Database, seed int64, first time.Time) {
	t.Helper()
	stamp := first.UTC().Format(time.RFC3339Nano)
	if _, err := database.Write().ExecContext(ctx,
		"UPDATE ai_profiles SET seed = ?, next_think_at = ?", seed, stamp); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Write().ExecContext(ctx,
		"UPDATE scheduled_events SET due_at = ? WHERE event_type = 'ai_think' AND state = 'pending'",
		stamp); err != nil {
		t.Fatal(err)
	}
}
