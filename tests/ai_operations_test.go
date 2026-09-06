package tests

import (
	"context"
	"testing"
	"time"

	domainai "universeatwar/internal/domain/ai"
)

// TestTwoMachinesScoutThenStrikeTogether walks the whole collective loop: the
// alliance looks first, gathers, and resolves one grouped attack.
func TestTwoMachinesScoutThenStrikeTogether(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, _, players := alliedArtificials(t)
	leader, second := players[0], players[1]

	for _, member := range players {
		setUnits(t, ctx, database, member.bodyID, "espionage_probe", 6)
		setUnits(t, ctx, database, member.bodyID, "cruiser", 12)
		setResearch(t, ctx, database, member.playerID, "espionage_technology", 3)
		setResearch(t, ctx, database, member.playerID, "computer_technology", 3)
		setResources(t, ctx, database, member.bodyID, 300000, 300000, 300000)
	}
	// A neighbour worth the trip, with nothing but resources.
	setResources(t, ctx, database, 1, 90000, 60000, 20000)

	// The alliance looks before it strikes: the first flights are probes.
	think(t, ctx, universeWorld)
	assertSingleValue(t, database,
		"SELECT COUNT(*) > 0 FROM fleets WHERE mission = 'espionage'", 1)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM fleets WHERE mission = 'attack'", 0)

	flyEverything(t, ctx, universeWorld)
	universeWorld.Clock.Advance(time.Minute)
	think(t, ctx, universeWorld)
	assertSingleText(t, database, "SELECT kind FROM ai_alliance_objectives WHERE id = 1", "raid")

	// Then the leader opens the operation and the ally joins it.
	for cycle := 0; cycle < 3; cycle++ {
		universeWorld.Clock.Advance(time.Minute)
		think(t, ctx, universeWorld)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM acs_groups", 1)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM acs_participants", 2)
	assertSingleValue(t, database,
		"SELECT COUNT(*) > 0 FROM ai_decisions WHERE player_id = ? AND action LIKE 'open operation%'", 1, leader.playerID)
	assertSingleValue(t, database,
		"SELECT COUNT(*) > 0 FROM ai_decisions WHERE player_id = ? AND action LIKE 'join operation%'", 1, second.playerID)

	// The operation lands as one battle, and the plan is closed.
	flyEverything(t, ctx, universeWorld)
	assertSingleText(t, database, "SELECT state FROM acs_groups WHERE id = 1", "resolved")
	assertSingleValue(t, database, "SELECT COUNT(*) FROM game_event_log WHERE event_type = 'acs_resolved'", 1)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM reports WHERE kind = 'combat_attack'", 2)

	universeWorld.Clock.Advance(time.Minute)
	think(t, ctx, universeWorld)
	assertSingleText(t, database, "SELECT state FROM ai_alliance_objectives WHERE id = 1", "resolved")
	// Nothing was conjured: every ship that left is either back, flying or lost.
	assertSingleValue(t, database, "SELECT COUNT(*) FROM planet_units WHERE quantity < 0", 0)
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM planet_resources WHERE metal < 0 OR crystal < 0 OR deuterium < 0", 0)
}

// TestAnAllianceDefendsTheMemberThatCallsForHelp proves a machine goes to the
// rescue of an ally that has just been hit, and says so when it cannot.
func TestAnAllianceDefendsTheMemberThatCallsForHelp(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, _, players := alliedArtificials(t)
	victim, helper := players[0], players[1]

	setUnits(t, ctx, database, helper.bodyID, "cruiser", 15)
	for _, member := range players {
		setResearch(t, ctx, database, member.playerID, "computer_technology", 3)
		setResources(t, ctx, database, member.bodyID, 300000, 300000, 300000)
	}

	// The victim has just been attacked: it holds a fresh defence report.
	now := universeWorld.Clock.Now().UTC()
	at := bodyCoordinate(t, ctx, universeWorld, victim.bodyID)
	if _, err := database.Write().ExecContext(ctx, `
		INSERT INTO reports(recipient_player_id, kind, subject_type, subject_id, galaxy, system, position,
			occurred_at, payload_version, payload, created_at)
		VALUES (?, 'combat_defense', 'planet', ?, ?, ?, ?, ?, 1, json_object('outcome', 'attacker'), ?)
	`, victim.playerID, victim.bodyID, at.Galaxy, at.System, at.Position,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}

	think(t, ctx, universeWorld)
	assertSingleText(t, database, "SELECT kind FROM ai_alliance_objectives WHERE id = 1", "defence")
	universeWorld.Clock.Advance(time.Minute)
	think(t, ctx, universeWorld)

	// The ally with ships stands over the threatened body.
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM fleets WHERE mission = 'hold' AND owner_player_id = ?", 1, helper.playerID)
	assertSingleValue(t, database,
		"SELECT COUNT(*) > 0 FROM ai_decisions WHERE action LIKE 'defend %' AND outcome = 'done'", 1)
	// And it really goes to the ally, not somewhere convenient.
	assertSingleValue(t, database, `
		SELECT COUNT(*) FROM fleets WHERE mission = 'hold'
		AND target_galaxy = ? AND target_system = ? AND target_position = ?`, 1,
		at.Galaxy, at.System, at.Position)

	// The guard arrives and waits there.
	flyEverything(t, ctx, universeWorld)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM fleets WHERE state = 'holding'", 1)
}

// TestAMemberWithoutShipsSaysSoRatherThanPretend proves an alliance that lacks
// the means to defend records the refusal instead of inventing a fleet.
func TestAMemberWithoutShipsSaysSoRatherThanPretend(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, _, players := alliedArtificials(t)
	victim := players[0]

	for _, member := range players {
		setResources(t, ctx, database, member.bodyID, 300000, 300000, 300000)
	}
	now := universeWorld.Clock.Now().UTC()
	at := bodyCoordinate(t, ctx, universeWorld, victim.bodyID)
	if _, err := database.Write().ExecContext(ctx, `
		INSERT INTO reports(recipient_player_id, kind, subject_type, subject_id, galaxy, system, position,
			occurred_at, payload_version, payload, created_at)
		VALUES (?, 'combat_defense', 'planet', ?, ?, ?, ?, ?, 1, json_object('outcome', 'attacker'), ?)
	`, victim.playerID, victim.bodyID, at.Galaxy, at.System, at.Position,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}

	think(t, ctx, universeWorld)
	universeWorld.Clock.Advance(time.Minute)
	think(t, ctx, universeWorld)

	assertSingleValue(t, database, "SELECT COUNT(*) FROM fleets WHERE mission = 'hold'", 0)
	assertSingleValue(t, database, `
		SELECT COUNT(*) > 0 FROM ai_decisions
		WHERE action LIKE 'defend %' AND outcome = 'skipped' AND reason = 'no ship to send'`, 1)
}

var _ = domainai.ScoutRole
