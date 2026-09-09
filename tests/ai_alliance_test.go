package tests

import (
	"context"
	"database/sql"
	"testing"
	"time"

	appai "universeatwar/internal/app/ai"
	appauth "universeatwar/internal/app/authentication"
	appclock "universeatwar/internal/clock"
	domainai "universeatwar/internal/domain/ai"
	"universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/rules"
	"universeatwar/internal/domain/universe"
	storagesqlite "universeatwar/internal/storage/sqlite"
)

// TestSharedIntelligenceCarriesItsProvenanceAndCanBeRevoked proves the common
// memory of an alliance holds only what a member observed and shared, and that
// taking the sharing back takes the belief with it.
func TestSharedIntelligenceCarriesItsProvenanceAndCanBeRevoked(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, _, players := alliedArtificials(t)
	scout, quiet := players[0], players[1]

	// Only the scout has looked at the neighbour.
	setUnits(t, ctx, database, scout.bodyID, "espionage_probe", 5)
	setResearch(t, ctx, database, scout.playerID, "espionage_technology", 3)
	setResearch(t, ctx, database, scout.playerID, "computer_technology", 3)
	setResources(t, ctx, database, scout.bodyID, 200000, 200000, 200000)
	setResources(t, ctx, database, 1, 60000, 40000, 10000)

	think(t, ctx, universeWorld)
	flyEverything(t, ctx, universeWorld)
	think(t, ctx, universeWorld)

	// The scout shared what it saw, with its name and its date on it.
	beliefs := recallOf(t, ctx, database, universeWorld, scout.playerID)
	target, found := beliefOf(beliefs, domainai.TargetKnowledge)
	if !found {
		t.Fatalf("the alliance believes nothing about a target: %+v", beliefs)
	}
	if target.AuthorID != scout.playerID || target.AuthorName == "" {
		t.Fatalf("the belief has no author: %+v", target)
	}
	if target.ReportID == 0 || target.Confidence <= 0 || !target.ExpiresAt.After(target.ObservedAt) {
		t.Fatalf("the belief carries no provenance: %+v", target)
	}
	// Both members read the same common memory: sharing is what makes it common.
	if len(recallOf(t, ctx, database, universeWorld, quiet.playerID)) != len(beliefs) {
		t.Fatal("the two members do not read the same memory")
	}

	// Taking the sharing back revokes everything drawn from that report.
	if _, err := database.Write().ExecContext(ctx,
		"UPDATE reports SET shared_alliance_id = NULL WHERE id = ?", target.ReportID); err != nil {
		t.Fatal(err)
	}
	if _, found := beliefOf(recallOf(t, ctx, database, universeWorld, quiet.playerID), domainai.TargetKnowledge); found {
		t.Fatal("an unshared report is still believed")
	}
	// The row is still there: revocation is a matter of reading, not of erasing.
	assertSingleValue(t, database, "SELECT COUNT(*) FROM ai_alliance_memory WHERE kind = 'target'", 1)
}

// TestAnUnsharedReportStaysUnknownToTheAllies proves nothing leaks sideways:
// what a member keeps to itself, the alliance does not know.
func TestAnUnsharedReportStaysUnknownToTheAllies(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, _, players := alliedArtificials(t)
	scout, quiet := players[0], players[1]

	// A report nobody ever shared, planted straight into the shelf.
	if _, err := database.Write().ExecContext(ctx, `
		INSERT INTO reports(recipient_player_id, kind, subject_type, subject_id, galaxy, system, position,
			occurred_at, payload_version, payload, created_at)
		VALUES (?, 'espionage', 'planet', 1, 1, 1, 8, '2042-09-10T12:00:00Z', 1,
			json_object('target_player_name', 'Alice', 'level', 5), '2042-09-10T12:00:00Z')
	`, scout.playerID); err != nil {
		t.Fatal(err)
	}
	beliefs := recallOf(t, ctx, database, universeWorld, quiet.playerID)
	if _, found := beliefOf(beliefs, domainai.TargetKnowledge); found {
		t.Fatalf("an unshared report reached the alliance: %+v", beliefs)
	}
	// And the ally cannot read the report itself either.
	if _, err := universeWorld.Reports.Get(ctx,
		appauth.Principal{AccountID: quiet.accountID}, 1); err == nil {
		t.Fatal("an ally read a report that was never shared")
	}
}

// TestMembersOfAnotherAllianceShareNothing proves the memory of a team stops at
// its own borders.
func TestMembersOfAnotherAllianceShareNothing(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, admin, players := alliedArtificials(t)
	scout := players[0]

	outsider, err := universeWorld.AI.Create(ctx, admin, appai.Request{
		Name: "Etranger", Archetype: domainai.Raider,
		Window: domainai.Window{Start: 0, End: 0}, Interval: 5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := universeWorld.AI.Enlist(ctx, admin, outsider.PlayerID, "Les Rivaux", "riv"); err != nil {
		t.Fatalf("Enlist() error = %v", err)
	}
	setUnits(t, ctx, database, scout.bodyID, "espionage_probe", 5)
	setResearch(t, ctx, database, scout.playerID, "espionage_technology", 3)
	setResearch(t, ctx, database, scout.playerID, "computer_technology", 3)
	setResources(t, ctx, database, scout.bodyID, 200000, 200000, 200000)
	setResources(t, ctx, database, 1, 60000, 40000, 10000)

	think(t, ctx, universeWorld)
	flyEverything(t, ctx, universeWorld)
	think(t, ctx, universeWorld)

	if len(recallOf(t, ctx, database, universeWorld, scout.playerID)) == 0 {
		t.Fatal("the alliance of the scout believes nothing")
	}
	if _, found := beliefOf(recallOf(t, ctx, database, universeWorld, outsider.PlayerID), domainai.TargetKnowledge); found {
		t.Fatal("a rival alliance read the intelligence of another")
	}
}

// artificial names one artificial player of a test.
type artificial struct {
	playerID  int64
	accountID int64
	bodyID    int64
}

// alliedArtificials prepares two artificial players in the same alliance, next
// to one human worth looking at.
func alliedArtificials(t *testing.T) (*storagesqlite.Database, *world, appauth.Principal, []artificial) {
	t.Helper()
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC))
	database := economyDatabase(t, ctx, 1)
	// These fixtures are about machines acting as one, so the universe has to
	// ask them to: at the default degree they answer only some of the calls.
	setRules(t, ctx, database, func(configured *rules.Ruleset) { configured.AI.Coordination = 1 })
	universeWorld := newWorld(t, database, clock)
	if _, err := universeWorld.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: 1}, "Alice"); err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	admin := appauth.Principal{AccountID: 1, Roles: []appauth.Role{appauth.RoleAdmin}}
	var players []artificial
	for index, name := range []string{"Eclaireur", "Compagnon"} {
		profile, err := universeWorld.AI.Create(ctx, admin, appai.Request{
			Name: name, Archetype: domainai.Scout,
			Window: domainai.Window{Start: 0, End: 0}, Interval: 5 * time.Minute,
		})
		if err != nil {
			t.Fatalf("Create(%s) error = %v", name, err)
		}
		if err := universeWorld.AI.Enlist(ctx, admin, profile.PlayerID, "Les Machines", "mch"); err != nil {
			t.Fatalf("Enlist(%s) error = %v", name, err)
		}
		players = append(players, artificial{
			playerID: profile.PlayerID, accountID: profile.AccountID, bodyID: int64(index) + 2,
		})
	}
	return database, universeWorld, admin, players
}

// recallOf reads the common memory as one member sees it.
func recallOf(t *testing.T, ctx context.Context, database *storagesqlite.Database,
	universeWorld *world, playerID int64) []domainai.Knowledge {
	t.Helper()
	alliance, found, err := universeWorld.Teamwork.Alliance(ctx, playerID)
	if err != nil || !found {
		t.Fatalf("Alliance(%d) = %v %v", playerID, found, err)
	}
	beliefs, err := universeWorld.Teamwork.Recall(ctx, alliance.ID, universeWorld.Clock.Now().UTC())
	if err != nil {
		t.Fatalf("Recall() error = %v", err)
	}
	return beliefs
}

func beliefOf(beliefs []domainai.Knowledge, kind domainai.KnowledgeKind) (domainai.Knowledge, bool) {
	for _, belief := range beliefs {
		if belief.Kind == kind {
			return belief, true
		}
	}
	return domainai.Knowledge{}, false
}

// flightHorizon bounds how far a test lets the clock run to settle missions, so
// that watching a stationed fleet does not also watch it come home again.
const flightHorizon = 90 * time.Minute

// flyEverything settles the missions that land within the horizon.
func flyEverything(t *testing.T, ctx context.Context, universeWorld *world) {
	t.Helper()
	limit := universeWorld.Clock.Now().UTC().Add(flightHorizon)
	for attempt := 0; attempt < 12; attempt++ {
		var due sql.NullString
		if err := universeWorld.Database.Read().QueryRowContext(ctx, `
			SELECT MIN(due_at) FROM scheduled_events WHERE state = 'pending' AND event_type <> 'ai_think'
		`).Scan(&due); err != nil {
			t.Fatal(err)
		}
		if !due.Valid {
			return
		}
		at, err := time.Parse(time.RFC3339Nano, due.String)
		if err != nil {
			t.Fatal(err)
		}
		if at.After(limit) {
			return
		}
		if at.After(universeWorld.Clock.Now().UTC()) {
			setClock(t, universeWorld.Clock, at)
		}
		if _, err := universeWorld.Events.CompleteDue(ctx, 200); err != nil {
			t.Fatalf("CompleteDue() error = %v", err)
		}
	}
}

// TestTheLeaderHandsOutRolesAndOpensOnePlan proves an alliance of machines
// organises itself from what its members declared, and pursues one plan at a
// time.
func TestTheLeaderHandsOutRolesAndOpensOnePlan(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, _, players := alliedArtificials(t)
	scout, fleeter := players[0], players[1]

	setUnits(t, ctx, database, scout.bodyID, "espionage_probe", 9)
	setUnits(t, ctx, database, fleeter.bodyID, "light_fighter", 40)
	for _, member := range players {
		setResearch(t, ctx, database, member.playerID, "espionage_technology", 3)
		setResearch(t, ctx, database, member.playerID, "computer_technology", 3)
		setResources(t, ctx, database, member.bodyID, 200000, 200000, 200000)
	}
	setResources(t, ctx, database, 1, 60000, 40000, 10000)

	// A first reflection: everybody declares itself and the scout goes looking.
	think(t, ctx, universeWorld)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM ai_alliance_memory WHERE kind = 'capability'", 2)
	// Nothing is worth a plan yet: nobody has seen anything.
	assertSingleValue(t, database, "SELECT COUNT(*) FROM ai_alliance_objectives", 0)

	// On the next one the leader ranks everybody on what they declared.
	universeWorld.Clock.Advance(time.Minute)
	think(t, ctx, universeWorld)
	assertSingleText(t, database,
		"SELECT role FROM ai_alliance_roles WHERE player_id = ?", "scout", scout.playerID)
	assertSingleText(t, database,
		"SELECT role FROM ai_alliance_roles WHERE player_id = ?", "fleeter", fleeter.playerID)

	flyEverything(t, ctx, universeWorld)
	think(t, ctx, universeWorld)

	// The report is shared, so the alliance opens a plan and moves to gathering.
	assertSingleValue(t, database, "SELECT COUNT(*) FROM ai_alliance_objectives", 1)
	assertSingleText(t, database, "SELECT kind FROM ai_alliance_objectives", "raid")
	think(t, ctx, universeWorld)
	assertSingleText(t, database, "SELECT state FROM ai_alliance_objectives", "assembling")
	// One plan at a time, whatever else the memory holds.
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM ai_alliance_objectives WHERE state IN ('scouting', 'assembling')", 1)
}

// TestAPlanNobodyLooksAtIsGivenUp proves an alliance does not wait for ever on
// intelligence that never comes.
func TestAPlanNobodyLooksAtIsGivenUp(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, _, players := alliedArtificials(t)
	leader := players[0]

	// A belief good enough to plan on, with nobody able to confirm it.
	alliance, _, err := universeWorld.Teamwork.Alliance(ctx, leader.playerID)
	if err != nil {
		t.Fatal(err)
	}
	now := universeWorld.Clock.Now().UTC()
	if err := universeWorld.Teamwork.Publish(ctx, alliance.ID, leader.playerID, []domainai.Knowledge{{
		Kind: domainai.TargetKnowledge, Coordinate: bodyCoordinate(t, ctx, universeWorld, 1),
		ObservedAt: now, ExpiresAt: now.Add(48 * time.Hour), Confidence: 1,
		Plunder: economyResources(90000), Complete: false, Summary: "vu de loin",
	}}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	think(t, ctx, universeWorld)
	assertSingleText(t, database, "SELECT state FROM ai_alliance_objectives", "scouting")

	// Six reflections later the window closes and the plan is dropped.
	for cycle := 0; cycle < 8; cycle++ {
		universeWorld.Clock.Advance(5 * time.Minute)
		think(t, ctx, universeWorld)
	}
	assertSingleText(t, database, "SELECT state FROM ai_alliance_objectives WHERE id = 1", "abandoned")
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM ai_decisions WHERE action LIKE 'abandon %' AND outcome = 'done'", 1)
}

// bodyCoordinate reads where a body sits, the way any page would show it.
func bodyCoordinate(t *testing.T, ctx context.Context, universeWorld *world, bodyID int64) universe.Coordinate {
	t.Helper()
	var at universe.Coordinate
	if err := universeWorld.Database.Read().QueryRowContext(ctx,
		"SELECT galaxy, system, position FROM planets WHERE id = ?", bodyID).
		Scan(&at.Galaxy, &at.System, &at.Position); err != nil {
		t.Fatal(err)
	}
	return at
}

func economyResources(metal int64) economy.Resources {
	return economy.Resources{Metal: metal}
}
