package tests

import (
	"context"
	"testing"
	"time"

	appai "universeatwar/internal/app/ai"
	domainai "universeatwar/internal/domain/ai"
)

// TestExpiredBeliefsFadeFromTheCommonMemory proves an alliance forgets: a
// belief past its expiry is no longer read, and decides nothing.
func TestExpiredBeliefsFadeFromTheCommonMemory(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, _, players := alliedArtificials(t)
	author := players[0]
	alliance, _, err := universeWorld.Teamwork.Alliance(ctx, author.playerID)
	if err != nil {
		t.Fatal(err)
	}
	now := universeWorld.Clock.Now().UTC()
	if err := universeWorld.Teamwork.Publish(ctx, alliance.ID, author.playerID, []domainai.Knowledge{{
		Kind: domainai.TargetKnowledge, Coordinate: bodyCoordinate(t, ctx, universeWorld, 1),
		ObservedAt: now, ExpiresAt: now.Add(time.Hour), Confidence: 1,
		Plunder: economyResources(90000), Complete: true, Summary: "vu de près",
	}}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if len(recallOf(t, ctx, database, universeWorld, author.playerID)) != 1 {
		t.Fatal("the belief was not written down")
	}
	// One hour later it means nothing, and the alliance plans nothing on it.
	setClock(t, universeWorld.Clock, now.Add(2*time.Hour))
	if len(recallOf(t, ctx, database, universeWorld, author.playerID)) != 0 {
		t.Fatal("an expired belief is still read")
	}
	think(t, ctx, universeWorld)
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM ai_alliance_objectives WHERE galaxy = 1 AND position = 8", 0)
	// The row survives: forgetting is a matter of reading, not of erasing.
	assertSingleValue(t, database, "SELECT COUNT(*) FROM ai_alliance_memory WHERE kind = 'target'", 1)
}

// TestTheSameSeedsGiveTheSameCollectivePlan proves two identical universes of
// machines reach the same plan, in the same order.
func TestTheSameSeedsGiveTheSameCollectivePlan(t *testing.T) {
	ctx := context.Background()
	run := func() []string {
		database, universeWorld, _, players := alliedArtificials(t)
		pinSeed(t, ctx, database, 4242, universeWorld.Clock.Now().Add(5*time.Minute))
		for _, member := range players {
			setUnits(t, ctx, database, member.bodyID, "espionage_probe", 6)
			setUnits(t, ctx, database, member.bodyID, "cruiser", 12)
			setResearch(t, ctx, database, member.playerID, "espionage_technology", 3)
			setResearch(t, ctx, database, member.playerID, "computer_technology", 3)
			setResources(t, ctx, database, member.bodyID, 300000, 300000, 300000)
		}
		setResources(t, ctx, database, 1, 90000, 60000, 20000)
		think(t, ctx, universeWorld)
		flyEverything(t, ctx, universeWorld)
		for cycle := 0; cycle < 4; cycle++ {
			universeWorld.Clock.Advance(time.Minute)
			think(t, ctx, universeWorld)
		}
		rows, err := database.Read().QueryContext(ctx, `
			SELECT player_id || ':' || action || ':' || outcome FROM ai_decisions
			WHERE layer IN ('strategic', 'tactical') ORDER BY id
		`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var trace []string
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatal(err)
			}
			trace = append(trace, line)
		}
		return trace
	}
	first, second := run(), run()
	if len(first) == 0 {
		t.Fatal("the alliance decided nothing at all")
	}
	if !sameActions(first, second) {
		t.Fatalf("two identical alliances diverged:\n%v\n%v", first, second)
	}
}

// TestASleepingMemberIsNotCountedOn proves an alliance plans with who is
// actually there, and gives up rather than strike alone.
func TestASleepingMemberIsNotCountedOn(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, admin, players := alliedArtificials(t)
	leader, sleeper := players[0], players[1]

	// The second member keeps office hours, and it is the middle of the day
	// for the first but the middle of the night for it.
	if _, err := database.Write().ExecContext(ctx, `
		UPDATE ai_profiles SET activity_start_hour = 20, activity_end_hour = 23 WHERE player_id = ?
	`, sleeper.playerID); err != nil {
		t.Fatal(err)
	}
	for _, member := range players {
		setUnits(t, ctx, database, member.bodyID, "espionage_probe", 6)
		setUnits(t, ctx, database, member.bodyID, "cruiser", 12)
		setResearch(t, ctx, database, member.playerID, "espionage_technology", 3)
		setResearch(t, ctx, database, member.playerID, "computer_technology", 3)
		setResources(t, ctx, database, member.bodyID, 300000, 300000, 300000)
	}
	setResources(t, ctx, database, 1, 90000, 60000, 20000)

	think(t, ctx, universeWorld)
	flyEverything(t, ctx, universeWorld)
	for cycle := 0; cycle < 6; cycle++ {
		universeWorld.Clock.Advance(time.Minute)
		think(t, ctx, universeWorld)
	}
	// The sleeper never declared itself awake, so it carries no active role.
	roles, err := universeWorld.Teamwork.Roles(ctx, allianceOf(t, ctx, universeWorld, leader.playerID))
	if err != nil {
		t.Fatal(err)
	}
	if roles[sleeper.playerID] != domainai.MinerRole && roles[sleeper.playerID] != "" {
		t.Fatalf("a sleeping member was given %s", roles[sleeper.playerID])
	}
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM fleets WHERE owner_player_id = ?", 0, sleeper.playerID)

	// Alone at the last reflection before the landing, the leader pulls its
	// fleet out rather than throw it away.
	assertSingleValue(t, database, "SELECT COUNT(*) FROM acs_participants", 1)
	var arrival string
	if err := database.Read().QueryRowContext(ctx,
		"SELECT arrives_at FROM acs_groups WHERE id = 1").Scan(&arrival); err != nil {
		t.Fatal(err)
	}
	landing, err := time.Parse(time.RFC3339Nano, arrival)
	if err != nil {
		t.Fatal(err)
	}
	setClock(t, universeWorld.Clock, landing.Add(-time.Minute))
	think(t, ctx, universeWorld)
	assertSingleValue(t, database,
		"SELECT COUNT(*) > 0 FROM ai_decisions WHERE action LIKE 'abandon %' AND outcome = 'done'", 1)
	assertSingleText(t, database, "SELECT state FROM acs_groups WHERE id = 1", "cancelled")
	_ = admin
}

// TestATargetThatMovesEndsTheOperation proves a plan survives contact with a
// world that changes: the target is gone, so the fleets come home.
func TestATargetThatMovesEndsTheOperation(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, _, players := alliedArtificials(t)
	for _, member := range players {
		setUnits(t, ctx, database, member.bodyID, "espionage_probe", 6)
		setUnits(t, ctx, database, member.bodyID, "cruiser", 12)
		setResearch(t, ctx, database, member.playerID, "espionage_technology", 3)
		setResearch(t, ctx, database, member.playerID, "computer_technology", 3)
		setResources(t, ctx, database, member.bodyID, 300000, 300000, 300000)
	}
	setResources(t, ctx, database, 1, 90000, 60000, 20000)

	think(t, ctx, universeWorld)
	flyEverything(t, ctx, universeWorld)
	for cycle := 0; cycle < 4; cycle++ {
		universeWorld.Clock.Advance(time.Minute)
		think(t, ctx, universeWorld)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM acs_participants", 2)

	// The neighbour moves out of the way before the operation lands.
	if _, err := database.Write().ExecContext(ctx,
		"UPDATE planets SET position = 12 WHERE id = 1"); err != nil {
		t.Fatal(err)
	}
	flyEverything(t, ctx, universeWorld)
	assertSingleText(t, database, "SELECT state FROM acs_groups WHERE id = 1", "cancelled")
	assertSingleValue(t, database, "SELECT COUNT(*) FROM reports WHERE kind = 'combat_attack'", 0)
	assertSingleValue(t, database, `
		SELECT COUNT(*) FROM fleets f JOIN acs_participants a ON a.fleet_id = f.id
		WHERE f.state IN ('returning', 'completed')`, 2)

	universeWorld.Clock.Advance(time.Minute)
	think(t, ctx, universeWorld)
	assertSingleText(t, database, "SELECT state FROM ai_alliance_objectives WHERE id = 1", "abandoned")
}

// TestCoordinationCreatesNothing proves a whole coordinated campaign moves
// things about without ever making any.
func TestCoordinationCreatesNothing(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, _, players := alliedArtificials(t)
	for _, member := range players {
		setUnits(t, ctx, database, member.bodyID, "espionage_probe", 6)
		setUnits(t, ctx, database, member.bodyID, "cruiser", 12)
		setResearch(t, ctx, database, member.playerID, "espionage_technology", 3)
		setResearch(t, ctx, database, member.playerID, "computer_technology", 3)
		setResources(t, ctx, database, member.bodyID, 300000, 300000, 300000)
	}
	setUnits(t, ctx, database, 1, "rocket_launcher", 20)
	setResources(t, ctx, database, 1, 90000, 60000, 20000)
	before := shipCensus(t, ctx, database)

	think(t, ctx, universeWorld)
	flyEverything(t, ctx, universeWorld)
	for cycle := 0; cycle < 6; cycle++ {
		universeWorld.Clock.Advance(time.Minute)
		think(t, ctx, universeWorld)
		flyEverything(t, ctx, universeWorld)
	}

	// Ships only ever left the census through a battle the reports account for.
	after := shipCensus(t, ctx, database)
	if after > before {
		t.Fatalf("the campaign created ships: %d became %d", before, after)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM planet_units WHERE quantity < 0", 0)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM fleet_ships WHERE quantity <= 0", 0)
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM planet_resources WHERE metal < 0 OR crystal < 0 OR deuterium < 0", 0)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM fleet_cargo WHERE metal < 0 OR crystal < 0", 0)
	// And every belief the alliance holds names the member it came from.
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM ai_alliance_memory WHERE author_player_id NOT IN (SELECT id FROM players)", 0)
}

// TestArtificialPlayersMeetTheAcceptanceChecklist walks the list of section 60:
// what a machine must be able to do, read out of its own diary.
func TestArtificialPlayersMeetTheAcceptanceChecklist(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, _, players := alliedArtificials(t)
	for _, member := range players {
		setUnits(t, ctx, database, member.bodyID, "espionage_probe", 6)
		setUnits(t, ctx, database, member.bodyID, "cruiser", 12)
		setBuilding(t, ctx, database, member.bodyID, "research_lab", 2)
		setBuilding(t, ctx, database, member.bodyID, "shipyard", 4)
		setResearch(t, ctx, database, member.playerID, "espionage_technology", 3)
		setResearch(t, ctx, database, member.playerID, "computer_technology", 3)
		setResearch(t, ctx, database, member.playerID, "combustion_drive", 2)
		setResources(t, ctx, database, member.bodyID, 400000, 400000, 400000)
	}
	setResources(t, ctx, database, 1, 90000, 60000, 20000)

	// The alliance looks, gathers, and only then does the operation land: a
	// grouped attack needs the time to be joined.
	think(t, ctx, universeWorld)
	flyEverything(t, ctx, universeWorld)
	for cycle := 0; cycle < 5; cycle++ {
		universeWorld.Clock.Advance(time.Minute)
		think(t, ctx, universeWorld)
	}
	flyEverything(t, ctx, universeWorld)
	universeWorld.Clock.Advance(time.Minute)
	think(t, ctx, universeWorld)

	for _, expected := range []struct {
		what  string
		query string
	}{
		{"develops its economy", "SELECT COUNT(*) > 0 FROM ai_decisions WHERE action LIKE 'build %' AND outcome = 'done'"},
		{"reaches for technology", "SELECT COUNT(*) > 0 FROM ai_decisions WHERE action LIKE 'research %' AND outcome = 'done'"},
		{"builds a fleet", "SELECT COUNT(*) > 0 FROM ai_decisions WHERE action LIKE 'produce %' AND outcome = 'done'"},
		{"looks before it strikes", "SELECT COUNT(*) > 0 FROM fleets WHERE mission = 'espionage'"},
		{"learns from a report", "SELECT COUNT(*) > 0 FROM ai_memory WHERE kind = 'target'"},
		{"shares with its allies", "SELECT COUNT(*) > 0 FROM reports WHERE shared_alliance_id IS NOT NULL"},
		{"coordinates a grouped attack", "SELECT COUNT(*) >= 2 FROM acs_participants"},
		{"resolves that attack", "SELECT COUNT(*) > 0 FROM game_event_log WHERE event_type = 'acs_resolved'"},
		{"loses ships like anybody", "SELECT COUNT(*) > 0 FROM reports WHERE kind = 'combat_attack'"},
		{"writes down why", "SELECT COUNT(*) > 0 FROM ai_decisions WHERE outcome = 'skipped' AND reason <> ''"},
	} {
		assertSingleValue(t, database, expected.query, 1)
		_ = expected.what
	}
}

// allianceOf reads the team of a player straight from the shared memory port.
func allianceOf(t *testing.T, ctx context.Context, universeWorld *world, playerID int64) int64 {
	t.Helper()
	alliance, found, err := universeWorld.Teamwork.Alliance(ctx, playerID)
	if err != nil || !found {
		t.Fatalf("Alliance(%d) = %v %v", playerID, found, err)
	}
	return alliance.ID
}

var _ = appai.Request{}
