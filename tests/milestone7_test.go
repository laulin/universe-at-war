package tests

import (
	"context"
	"errors"
	"testing"

	domainacs "universeatwar/internal/domain/acs"
	"universeatwar/internal/domain/unit"
	storagesqlite "universeatwar/internal/storage/sqlite"
)

// TestWithdrawalClosesExactlyAtTheArrival proves the window shuts on the very
// second the team lands, and not a moment earlier or later.
func TestWithdrawalClosesExactlyAtTheArrival(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, players := allianceUniverse(t, 3)
	alice, bob, carol := players[0], players[1], players[2]
	joinAlliance(t, ctx, universeWorld, alice, bob, "player2")

	aliceHome := homeOf(t, ctx, universeWorld, alice)
	bobHome := homeOf(t, ctx, universeWorld, bob)
	target := homeOf(t, ctx, universeWorld, carol)
	for _, planet := range []int64{aliceHome.ID, bobHome.ID} {
		setUnits(t, ctx, database, planet, "cruiser", 8)
		setResources(t, ctx, database, planet, 50000, 50000, 50000)
	}
	group, err := universeWorld.ACS.Create(ctx, alice, aliceHome.ID,
		fleetOf(target.Coordinate, unit.Cruiser, 4), "open")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := universeWorld.ACS.Join(ctx, bob, group.ID, bobHome.ID,
		fleetOf(target.Coordinate, unit.Cruiser, 4), "join"); err != nil {
		t.Fatalf("Join() error = %v", err)
	}

	// One second before the landing the window is still open.
	universeWorld.Clock.Set(group.ArrivesAt.Add(-1e9))
	if err := universeWorld.ACS.Withdraw(ctx, bob, 2, "early"); err != nil {
		t.Fatalf("Withdraw() one second early = %v", err)
	}
	// At the landing itself it is shut.
	universeWorld.Clock.Set(group.ArrivesAt)
	if err := universeWorld.ACS.Withdraw(ctx, alice, 1, "late"); !errors.Is(err, domainacs.ErrTooLate) {
		t.Fatalf("Withdraw() at the arrival = %v", err)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM acs_participants", 1)
}

// TestLeavingTheAllianceKeepsTheFleetButClosesTheDoor proves an administrative
// decision never pulls ships out of the sky, and never lets a stranger back in.
func TestLeavingTheAllianceKeepsTheFleetButClosesTheDoor(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, players := allianceUniverse(t, 3)
	alice, bob, carol := players[0], players[1], players[2]
	joinAlliance(t, ctx, universeWorld, alice, bob, "player2")

	aliceHome := homeOf(t, ctx, universeWorld, alice)
	bobHome := homeOf(t, ctx, universeWorld, bob)
	target := homeOf(t, ctx, universeWorld, carol)
	for _, planet := range []int64{aliceHome.ID, bobHome.ID} {
		setUnits(t, ctx, database, planet, "cruiser", 8)
		setResources(t, ctx, database, planet, 50000, 50000, 50000)
	}
	group, err := universeWorld.ACS.Create(ctx, alice, aliceHome.ID,
		fleetOf(target.Coordinate, unit.Cruiser, 4), "open")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := universeWorld.ACS.Join(ctx, bob, group.ID, bobHome.ID,
		fleetOf(target.Coordinate, unit.Cruiser, 2), "join"); err != nil {
		t.Fatalf("Join() error = %v", err)
	}
	if err := universeWorld.Alliance.Leave(ctx, bob); err != nil {
		t.Fatalf("Leave() error = %v", err)
	}

	assertSingleValue(t, database, "SELECT COUNT(*) FROM acs_participants WHERE player_id = 2", 1)
	assertSingleText(t, database, "SELECT state FROM fleets WHERE id = 2", "outbound")
	if _, err := universeWorld.ACS.Join(ctx, bob, group.ID, bobHome.ID,
		fleetOf(target.Coordinate, unit.Cruiser, 2), "again"); !errors.Is(err, domainacs.ErrNotAMember) {
		t.Fatalf("a former member joining error = %v", err)
	}
	if _, err := universeWorld.ACS.Group(ctx, bob, group.ID); !errors.Is(err, domainacs.ErrNotAMember) {
		t.Fatalf("a former member reading error = %v", err)
	}

	// The fleet that is already flying still fights when the team lands.
	universeWorld.Clock.Set(group.ArrivesAt)
	if _, err := universeWorld.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}
	assertSingleText(t, database, "SELECT state FROM acs_groups WHERE id = 1", "resolved")
	assertSingleValue(t, database, "SELECT COUNT(*) FROM reports WHERE kind = 'combat_attack'", 2)
}

// TestVanishedTargetSendsTheWholeOperationHome proves an operation never lands
// on nothing: it calls itself off and every fleet turns around.
func TestVanishedTargetSendsTheWholeOperationHome(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, players := allianceUniverse(t, 3)
	alice, bob, carol := players[0], players[1], players[2]
	joinAlliance(t, ctx, universeWorld, alice, bob, "player2")

	aliceHome := homeOf(t, ctx, universeWorld, alice)
	bobHome := homeOf(t, ctx, universeWorld, bob)
	target := homeOf(t, ctx, universeWorld, carol)
	for _, planet := range []int64{aliceHome.ID, bobHome.ID} {
		setUnits(t, ctx, database, planet, "cruiser", 8)
		setResources(t, ctx, database, planet, 50000, 50000, 50000)
	}
	group, err := universeWorld.ACS.Create(ctx, alice, aliceHome.ID,
		fleetOf(target.Coordinate, unit.Cruiser, 4), "open")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := universeWorld.ACS.Join(ctx, bob, group.ID, bobHome.ID,
		fleetOf(target.Coordinate, unit.Cruiser, 4), "join"); err != nil {
		t.Fatalf("Join() error = %v", err)
	}
	// The target moves out of the way before the team lands.
	if _, err := database.Write().ExecContext(ctx,
		"UPDATE planets SET position = 12 WHERE id = ?", target.ID); err != nil {
		t.Fatal(err)
	}

	universeWorld.Clock.Set(group.ArrivesAt)
	if _, err := universeWorld.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}
	assertSingleText(t, database, "SELECT state FROM acs_groups WHERE id = 1", "cancelled")
	assertSingleValue(t, database, "SELECT COUNT(*) FROM fleets WHERE state = 'returning'", 2)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM reports", 0)
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM game_event_log WHERE event_type = 'mission_aborted'", 2)
}

// TestGroupedBattleCreatesNoShipsAndNoResources proves the accounting of a
// multi-party battle: every ship is either a survivor or a loss, and every
// resource taken from the target is carried by somebody.
func TestGroupedBattleCreatesNoShipsAndNoResources(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, players := allianceUniverse(t, 3)
	alice, bob, carol := players[0], players[1], players[2]
	joinAlliance(t, ctx, universeWorld, alice, bob, "player2")

	aliceHome := homeOf(t, ctx, universeWorld, alice)
	bobHome := homeOf(t, ctx, universeWorld, bob)
	target := homeOf(t, ctx, universeWorld, carol)
	setUnits(t, ctx, database, aliceHome.ID, "cruiser", 10)
	setUnits(t, ctx, database, bobHome.ID, "cruiser", 6)
	setResources(t, ctx, database, aliceHome.ID, 50000, 50000, 50000)
	setResources(t, ctx, database, bobHome.ID, 50000, 50000, 50000)
	setUnits(t, ctx, database, target.ID, "light_fighter", 40)
	setUnits(t, ctx, database, target.ID, "rocket_launcher", 10)
	setResources(t, ctx, database, target.ID, 30000, 15000, 5000)

	before := shipCensus(t, ctx, database)
	group, err := universeWorld.ACS.Create(ctx, alice, aliceHome.ID,
		fleetOf(target.Coordinate, unit.Cruiser, 10), "open")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	joined, err := universeWorld.ACS.Join(ctx, bob, group.ID, bobHome.ID,
		fleetOf(target.Coordinate, unit.Cruiser, 6), "join")
	if err != nil {
		t.Fatalf("Join() error = %v", err)
	}
	universeWorld.Clock.Set(joined.ArrivesAt)
	if _, err := universeWorld.Events.CompleteDue(ctx, 40); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}

	// Every ship that left the census died in the battle, and the two reports
	// of the two allies agree on how many.
	after := shipCensus(t, ctx, database)
	var reportedLosses int64
	if err := database.Read().QueryRowContext(ctx, `
		SELECT SUM(lost) FROM (
			SELECT (SELECT COALESCE(SUM(value), 0) FROM json_each(json_extract(payload, '$.attackers[0].losses'))) AS lost
			FROM reports WHERE kind = 'combat_defense'
			UNION ALL
			SELECT (SELECT COALESCE(SUM(value), 0) FROM json_each(json_extract(payload, '$.attackers[1].losses')))
			FROM reports WHERE kind = 'combat_defense'
			UNION ALL
			SELECT (SELECT COALESCE(SUM(value), 0) FROM json_each(json_extract(payload, '$.defenders[0].losses')))
			FROM reports WHERE kind = 'combat_defense'
		)
	`).Scan(&reportedLosses); err != nil {
		t.Fatal(err)
	}
	if before-after != reportedLosses {
		t.Fatalf("ships vanished = %d, losses told = %d", before-after, reportedLosses)
	}

	// Nothing is ever negative, anywhere.
	assertSingleValue(t, database, "SELECT COUNT(*) FROM planet_units WHERE quantity < 0", 0)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM fleet_ships WHERE quantity <= 0", 0)
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM planet_resources WHERE metal < 0 OR crystal < 0 OR deuterium < 0", 0)
	// The haul told by the journal is exactly what the fleets carry away.
	var looted, carried int64
	if err := database.Read().QueryRowContext(ctx, `
		SELECT json_extract(payload, '$.loot_metal') + json_extract(payload, '$.loot_crystal')
		     + json_extract(payload, '$.loot_deuterium')
		FROM game_event_log WHERE event_type = 'combat_resolved'`).Scan(&looted); err != nil {
		t.Fatal(err)
	}
	if err := database.Read().QueryRowContext(ctx,
		"SELECT COALESCE(SUM(metal + crystal + deuterium), 0) FROM fleet_cargo").Scan(&carried); err != nil {
		t.Fatal(err)
	}
	if carried != looted {
		t.Fatalf("looted = %d, carried = %d", looted, carried)
	}
}

// shipCensus counts every ship in the universe, parked or in flight.
func shipCensus(t *testing.T, ctx context.Context, database *storagesqlite.Database) int64 {
	t.Helper()
	var total int64
	if err := database.Read().QueryRowContext(ctx, `
		SELECT (SELECT COALESCE(SUM(quantity), 0) FROM planet_units)
		     + (SELECT COALESCE(SUM(quantity), 0) FROM fleet_ships)
	`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	return total
}
