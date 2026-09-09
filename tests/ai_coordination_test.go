package tests

import (
	"context"
	"testing"
	"time"

	"universeatwar/internal/domain/rules"
)

// TestAnUncoordinatedAllianceStillPlansButNobodyMarches proves the degree of
// coordination is no longer inert. The leader reasons for the alliance exactly
// as before — it looks, it decides what is worth doing and it says so — and at a
// degree of nothing not one member acts on it.
func TestAnUncoordinatedAllianceStillPlansButNobodyMarches(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, _, players := alliedArtificials(t)
	setRules(t, ctx, database, func(configured *rules.Ruleset) { configured.AI.Coordination = 0 })

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
	universeWorld.Clock.Advance(time.Minute)
	think(t, ctx, universeWorld)

	// The alliance still holds an opinion about what is worth doing.
	assertSingleText(t, database, "SELECT kind FROM ai_alliance_objectives WHERE id = 1", "raid")

	for cycle := 0; cycle < 3; cycle++ {
		universeWorld.Clock.Advance(time.Minute)
		think(t, ctx, universeWorld)
	}
	// And nobody answered the call: no grouped operation was ever opened.
	assertSingleValue(t, database, "SELECT COUNT(*) FROM acs_groups", 0)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM acs_participants", 0)
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM ai_decisions WHERE action LIKE 'open operation%' OR action LIKE 'join operation%'", 0)
}

// TestACoordinatedAllianceMarchesTogether is the other end of the same dial, and
// it is what every other alliance fixture in this suite relies on.
func TestACoordinatedAllianceMarchesTogether(t *testing.T) {
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
	universeWorld.Clock.Advance(time.Minute)
	think(t, ctx, universeWorld)
	for cycle := 0; cycle < 3; cycle++ {
		universeWorld.Clock.Advance(time.Minute)
		think(t, ctx, universeWorld)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM acs_participants", 2)
}
