package tests

import (
	"context"
	"errors"
	"testing"
	"time"

	appacs "universeatwar/internal/app/acs"
	appauth "universeatwar/internal/app/authentication"
	appeconomy "universeatwar/internal/app/economy"
	appfleet "universeatwar/internal/app/fleet"
	domainacs "universeatwar/internal/domain/acs"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/unit"
	"universeatwar/internal/domain/universe"
)

// TestGroupedOperationSynchronisesEveryFleet proves the central promise of a
// grouped operation: whoever joins, everybody lands at the same second, and a
// slow fleet delays the whole team instead of arriving alone.
func TestGroupedOperationSynchronisesEveryFleet(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, players := allianceUniverse(t, 4)
	alice, bob, carol, stranger := players[0], players[1], players[2], players[3]
	joinAlliance(t, ctx, universeWorld, alice, bob, "player2")

	aliceHome := homeOf(t, ctx, universeWorld, alice)
	bobHome := homeOf(t, ctx, universeWorld, bob)
	strangerHome := homeOf(t, ctx, universeWorld, stranger)
	target := homeOf(t, ctx, universeWorld, carol)
	for _, planet := range []int64{aliceHome.ID, bobHome.ID, strangerHome.ID} {
		setUnits(t, ctx, database, planet, "cruiser", 10)
		setResources(t, ctx, database, planet, 50000, 50000, 50000)
	}

	departure := universeWorld.Clock.Now().UTC()
	group, err := universeWorld.ACS.Create(ctx, alice, aliceHome.ID, attackRequest(target.Coordinate, 100), "open")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if group.State != domainacs.Forming || len(group.Participants) != 1 || group.MaximumSize != 5 {
		t.Fatalf("group = %+v", group)
	}
	firstArrival := group.ArrivesAt

	// A half-speed fleet takes twice as long, so the whole team waits for it.
	preview, err := universeWorld.ACS.Preview(ctx, bob, group.ID, bobHome.ID, attackRequest(target.Coordinate, 50))
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if !preview.DelaysTheTeam || !preview.GroupArrival.After(firstArrival) {
		t.Fatalf("preview = %+v", preview)
	}
	joined, err := universeWorld.ACS.Join(ctx, bob, group.ID, bobHome.ID, attackRequest(target.Coordinate, 50), "join")
	if err != nil {
		t.Fatalf("Join() error = %v", err)
	}
	if len(joined.Participants) != 2 || !joined.ArrivesAt.Equal(preview.GroupArrival) {
		t.Fatalf("joined = %+v", joined)
	}
	assertSingleText(t, database,
		"SELECT COUNT(DISTINCT arrives_at) FROM fleets WHERE state = 'outbound'", "1")
	assertSingleText(t, database,
		"SELECT arrives_at FROM acs_groups WHERE id = 1", timestampOf(joined.ArrivesAt))
	assertSingleText(t, database,
		"SELECT due_at FROM scheduled_events WHERE event_type = 'acs_resolved' AND state = 'pending'",
		timestampOf(joined.ArrivesAt))
	// No fleet keeps an arrival of its own: the operation owns the landing.
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM scheduled_events WHERE event_type IN ('combat_resolved', 'fleet_arrived')", 0)
	// Waiting for the team costs the fast fleet nothing on the way home: each
	// one still flies back for its own travel time.
	assertSingleText(t, database, "SELECT returns_at FROM fleets WHERE id = 1",
		timestampOf(joined.ArrivesAt.Add(firstArrival.Sub(departure))))
	assertSingleText(t, database, "SELECT returns_at FROM fleets WHERE id = 2",
		timestampOf(joined.ArrivesAt.Add(joined.ArrivesAt.Sub(departure))))

	// An outsider cannot join, and cannot even tell the operation exists.
	if _, err := universeWorld.ACS.Join(ctx, stranger, group.ID, strangerHome.ID,
		attackRequest(target.Coordinate, 100), "intruder"); !errors.Is(err, domainacs.ErrNotAMember) {
		t.Fatalf("a stranger joining error = %v", err)
	}
	if _, err := universeWorld.ACS.Group(ctx, stranger, group.ID); !errors.Is(err, domainacs.ErrNotAMember) {
		t.Fatalf("a stranger reading error = %v", err)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM acs_participants", 2)
}

// TestWithdrawingTheLastFleetCancelsTheOperation proves that leaving a group is
// an ordinary recall, and that an empty group stops existing.
func TestWithdrawingTheLastFleetCancelsTheOperation(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, players := allianceUniverse(t, 3)
	alice, bob, carol := players[0], players[1], players[2]
	joinAlliance(t, ctx, universeWorld, alice, bob, "player2")

	aliceHome := homeOf(t, ctx, universeWorld, alice)
	bobHome := homeOf(t, ctx, universeWorld, bob)
	target := homeOf(t, ctx, universeWorld, carol)
	for _, planet := range []int64{aliceHome.ID, bobHome.ID} {
		setUnits(t, ctx, database, planet, "cruiser", 10)
		setResources(t, ctx, database, planet, 50000, 50000, 50000)
	}
	group, err := universeWorld.ACS.Create(ctx, alice, aliceHome.ID, attackRequest(target.Coordinate, 100), "open")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := universeWorld.ACS.Join(ctx, bob, group.ID, bobHome.ID, attackRequest(target.Coordinate, 100), "join"); err != nil {
		t.Fatalf("Join() error = %v", err)
	}

	// One cannot pull somebody else's fleet out of the sky.
	if err := universeWorld.ACS.Withdraw(ctx, alice, 2, "steal"); !errors.Is(err, appacs.ErrForbidden) {
		t.Fatalf("withdrawing another fleet error = %v", err)
	}
	if err := universeWorld.ACS.Withdraw(ctx, bob, 2, "leave"); err != nil {
		t.Fatalf("Withdraw() error = %v", err)
	}
	assertSingleText(t, database, "SELECT state FROM fleets WHERE id = 2", "recalled")
	assertSingleText(t, database, "SELECT state FROM acs_groups WHERE id = 1", "forming")
	assertSingleValue(t, database, "SELECT COUNT(*) FROM acs_participants", 1)

	if err := universeWorld.ACS.Withdraw(ctx, alice, 1, "leave"); err != nil {
		t.Fatalf("Withdraw() error = %v", err)
	}
	assertSingleText(t, database, "SELECT state FROM acs_groups WHERE id = 1", "cancelled")
	assertSingleText(t, database,
		"SELECT state FROM scheduled_events WHERE event_type = 'acs_resolved'", "cancelled")
	// Both fleets fly home, and no landing is left pending anywhere.
	assertSingleValue(t, database, "SELECT COUNT(*) FROM fleets WHERE state = 'recalled'", 2)
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM scheduled_events WHERE state = 'pending' AND event_type <> 'fleet_returned'", 0)
}

// TestGroupedArrivalIsResolvedOnceForTheWholeTeam proves the landing is applied
// once, to every engaged fleet, and that replaying it changes nothing.
func TestGroupedArrivalIsResolvedOnceForTheWholeTeam(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, players := allianceUniverse(t, 3)
	alice, bob, carol := players[0], players[1], players[2]
	joinAlliance(t, ctx, universeWorld, alice, bob, "player2")

	aliceHome := homeOf(t, ctx, universeWorld, alice)
	bobHome := homeOf(t, ctx, universeWorld, bob)
	target := homeOf(t, ctx, universeWorld, carol)
	for _, planet := range []int64{aliceHome.ID, bobHome.ID} {
		setUnits(t, ctx, database, planet, "cruiser", 10)
		setResources(t, ctx, database, planet, 50000, 50000, 50000)
	}
	setUnits(t, ctx, database, target.ID, "rocket_launcher", 12)
	setResources(t, ctx, database, target.ID, 20000, 10000, 4000)

	group, err := universeWorld.ACS.Create(ctx, alice, aliceHome.ID, attackRequest(target.Coordinate, 100), "open")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	joined, err := universeWorld.ACS.Join(ctx, bob, group.ID, bobHome.ID, attackRequest(target.Coordinate, 100), "join")
	if err != nil {
		t.Fatalf("Join() error = %v", err)
	}

	// The window closes at the very second the team lands.
	universeWorld.Clock.Set(joined.ArrivesAt)
	if _, err := universeWorld.ACS.Join(ctx, bob, group.ID, bobHome.ID,
		attackRequest(target.Coordinate, 100), "late"); !errors.Is(err, domainacs.ErrTooLate) {
		t.Fatalf("a late join error = %v", err)
	}
	if _, err := universeWorld.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}
	assertSingleText(t, database, "SELECT state FROM acs_groups WHERE id = 1", "resolved")
	assertSingleValue(t, database, "SELECT COUNT(*) FROM game_event_log WHERE event_type = 'acs_resolved'", 1)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM fleets WHERE state = 'returning'", 2)
	// Both attackers and the defender are told about the same landing.
	for _, player := range []int{1, 2} {
		assertSingleValue(t, database,
			"SELECT COUNT(*) > 0 FROM reports WHERE recipient_player_id = ? AND kind = 'combat_attack'", 1, player)
	}
	assertSingleValue(t, database,
		"SELECT COUNT(*) > 0 FROM reports WHERE recipient_player_id = 3 AND kind = 'combat_defense'", 1)
	assertSingleValue(t, database,
		"SELECT quantity < 12 FROM planet_units WHERE planet_id = ? AND unit_id = 'rocket_launcher'", 1, target.ID)

	// Replaying the landing must not fight a second battle.
	var reportsBefore int
	if err := database.Read().QueryRowContext(ctx, "SELECT COUNT(*) FROM reports").Scan(&reportsBefore); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Write().ExecContext(ctx,
		"UPDATE scheduled_events SET state = 'pending', processed_at = NULL WHERE event_type = 'acs_resolved'"); err != nil {
		t.Fatal(err)
	}
	if _, err := universeWorld.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatalf("replayed CompleteDue() error = %v", err)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM game_event_log WHERE event_type = 'acs_resolved'", 1)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM fleets WHERE state = 'returning'", 2)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM reports", reportsBefore)
}

func attackRequest(target universe.Coordinate, percent int) appfleet.LaunchRequest {
	return appfleet.LaunchRequest{
		Target: target, TargetKind: domainfleet.TargetPlanet, Mission: domainfleet.MissionAttack,
		Composition: domainfleet.Composition{unit.Cruiser: 5}, Percent: percent,
	}
}

// homeOf returns the first body of a player, the one every test launches from.
func homeOf(t *testing.T, ctx context.Context, universeWorld *world, principal appauth.Principal) appeconomy.Planet {
	t.Helper()
	planets, err := universeWorld.Economy.Planets(ctx, principal)
	if err != nil || len(planets) == 0 {
		t.Fatalf("Planets() = %d %v", len(planets), err)
	}
	return planets[0]
}

func timestampOf(at time.Time) string {
	return at.UTC().Format(time.RFC3339Nano)
}
