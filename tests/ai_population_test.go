package tests

import (
	"context"
	"fmt"
	"testing"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appclock "universeatwar/internal/clock"
	domainai "universeatwar/internal/domain/ai"
	"universeatwar/internal/domain/rules"
	storagesqlite "universeatwar/internal/storage/sqlite"
)

// populationDatabase prepares a running universe whose ruleset asks for a given
// artificial population. At least one account exists, because a ruleset names
// the administrator who settled it.
func populationDatabase(t *testing.T, ctx context.Context, accounts int, mutate func(*rules.Ruleset)) *storagesqlite.Database {
	t.Helper()
	database := freshDatabase(t, ctx, "population.db")
	now := "2042-09-10T11:12:13Z"
	for id := 1; id <= accounts; id++ {
		name := fmt.Sprintf("player%d", id)
		if _, err := database.Write().ExecContext(ctx,
			"INSERT INTO accounts(id, username, username_normalized, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
			id, name, name, now, now); err != nil {
			t.Fatal(err)
		}
	}
	configured := rules.Default()
	if mutate != nil {
		mutate(&configured)
	}
	document, err := rules.Encode(configured)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Write().ExecContext(ctx,
		"INSERT INTO ruleset_versions(version, status, document, checksum, author_account_id, effective_at, created_at) VALUES (1, 'active', ?, 'test', 1, ?, ?)",
		string(document), now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Write().ExecContext(ctx,
		"INSERT INTO server_state(id, state, version, updated_at) VALUES (1, 'RUNNING', 1, ?)", now); err != nil {
		t.Fatal(err)
	}
	return database
}

// populationWorld wires a running universe and the reconciler that fills it.
func populationWorld(t *testing.T, ctx context.Context, accounts int, mutate func(*rules.Ruleset)) *world {
	t.Helper()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC))
	return newWorld(t, populationDatabase(t, ctx, accounts, mutate), clock)
}

// settle runs the reconciler the way the worker does, a few players per pass,
// until the universe stops changing.
func settle(t *testing.T, ctx context.Context, universeWorld *world) int {
	t.Helper()
	born := 0
	for pass := 0; pass < 500; pass++ {
		created, err := universeWorld.Population.Populate(ctx, 3)
		if err != nil {
			t.Fatalf("Populate() error = %v", err)
		}
		if created == 0 {
			return born
		}
		born += created
	}
	t.Fatal("the population never settled")
	return 0
}

func count(t *testing.T, ctx context.Context, universeWorld *world, query string, arguments ...any) int {
	t.Helper()
	var total int
	if err := universeWorld.Database.Write().QueryRowContext(ctx, query, arguments...).Scan(&total); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return total
}

func artificialPopulation(mutate func(*rules.Ruleset)) func(*rules.Ruleset) {
	return func(configured *rules.Ruleset) {
		configured.AI.Total = 7
		configured.AI.IndependentCount = 0
		configured.AI.AllianceCount = 1
		configured.AI.AllianceSize = 2
		if mutate != nil {
			mutate(configured)
		}
	}
}

// TestThePopulationTheRulesetOrderedAppears is the whole point of the
// reconciler: a universe holds the artificial players its ruleset asked for,
// without an administrator filling a form once per player.
func TestThePopulationTheRulesetOrderedAppears(t *testing.T) {
	ctx := context.Background()
	universeWorld := populationWorld(t, ctx, 1, artificialPopulation(nil))
	human, err := universeWorld.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: 1}, "Alice")
	if err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}

	if born := settle(t, ctx, universeWorld); born != 7 {
		t.Fatalf("settle() = %d, want 7", born)
	}
	if total := count(t, ctx, universeWorld, "SELECT COUNT(*) FROM ai_profiles WHERE state = 'active'"); total != 7 {
		t.Fatalf("active artificial players = %d, want 7", total)
	}
	if members := count(t, ctx, universeWorld,
		"SELECT COUNT(*) FROM alliance_members m JOIN alliances a ON a.id = m.alliance_id WHERE a.tag = 'IA1'"); members != 2 {
		t.Fatalf("members of IA1 = %d, want 2", members)
	}
	if loners := count(t, ctx, universeWorld, `
		SELECT COUNT(*) FROM ai_profiles p
		WHERE NOT EXISTS (SELECT 1 FROM alliance_members m WHERE m.player_id = p.player_id)
	`); loners != 5 {
		t.Fatalf("unallied artificial players = %d, want 5", loners)
	}
	// Every one of them owns exactly one world, and no two share a position.
	if bodies := count(t, ctx, universeWorld, `
		SELECT COUNT(*) FROM planets pl JOIN ai_profiles p ON p.player_id = pl.owner_player_id
	`); bodies != 7 {
		t.Fatalf("artificial home worlds = %d, want 7", bodies)
	}
	if positions := count(t, ctx, universeWorld,
		"SELECT COUNT(DISTINCT galaxy || ':' || system || ':' || position) FROM planets"); positions != 8 {
		t.Fatalf("distinct positions = %d, want 8", positions)
	}
	if home := count(t, ctx, universeWorld,
		"SELECT COUNT(*) FROM planets WHERE id = ? AND galaxy = 1 AND system = 1 AND position = 8", human.ID); home != 1 {
		t.Fatalf("the human empire moved from 1:1:8")
	}
}

// TestTheTeamsAreFilledBeforeTheLoners proves the alliances the ruleset asked
// for are staffed first, so a universe never ends up with a full population and
// an empty team.
func TestTheTeamsAreFilledBeforeTheLoners(t *testing.T) {
	ctx := context.Background()
	universeWorld := populationWorld(t, ctx, 1, artificialPopulation(nil))
	settle(t, ctx, universeWorld)

	rows, err := universeWorld.Database.Write().QueryContext(ctx,
		"SELECT player_id FROM alliance_members ORDER BY player_id")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var enlisted []int64
	for rows.Next() {
		var playerID int64
		if err := rows.Scan(&playerID); err != nil {
			t.Fatal(err)
		}
		enlisted = append(enlisted, playerID)
	}
	if len(enlisted) != 2 || enlisted[0] != 1 || enlisted[1] != 2 {
		t.Fatalf("enlisted players = %v, want the first two born", enlisted)
	}
}

// TestPopulatingTwiceCreatesNobody is the idempotence the whole design rests
// on: the reconciler runs on every pass of the worker, for the life of the
// server.
func TestPopulatingTwiceCreatesNobody(t *testing.T) {
	ctx := context.Background()
	universeWorld := populationWorld(t, ctx, 1, artificialPopulation(nil))
	settle(t, ctx, universeWorld)
	before := count(t, ctx, universeWorld, "SELECT COUNT(*) FROM accounts")

	if born := settle(t, ctx, universeWorld); born != 0 {
		t.Fatalf("second settle() = %d, want nobody", born)
	}
	if after := count(t, ctx, universeWorld, "SELECT COUNT(*) FROM accounts"); after != before {
		t.Fatalf("accounts = %d after a second pass, want %d", after, before)
	}
}

// TestARetiredPlayerIsNotBornAgain proves a decision an administrator took
// sticks: the slot a retired player spent stays spent.
func TestARetiredPlayerIsNotBornAgain(t *testing.T) {
	ctx := context.Background()
	universeWorld := populationWorld(t, ctx, 1, artificialPopulation(nil))
	admin := appauth.Principal{AccountID: 1, Roles: []appauth.Role{appauth.RoleAdmin}}
	settle(t, ctx, universeWorld)
	accounts := count(t, ctx, universeWorld, "SELECT COUNT(*) FROM accounts")

	var retired int64
	if err := universeWorld.Database.Write().QueryRowContext(ctx,
		"SELECT player_id FROM ai_profiles ORDER BY player_id LIMIT 1").Scan(&retired); err != nil {
		t.Fatal(err)
	}
	if err := universeWorld.AI.Retire(ctx, admin, retired); err != nil {
		t.Fatalf("Retire() error = %v", err)
	}

	if born := settle(t, ctx, universeWorld); born != 0 {
		t.Fatalf("settle() after a retirement = %d, want nobody", born)
	}
	if total := count(t, ctx, universeWorld, "SELECT COUNT(*) FROM ai_profiles"); total != 7 {
		t.Fatalf("artificial profiles = %d, want the same 7", total)
	}
	if still := count(t, ctx, universeWorld,
		"SELECT COUNT(*) FROM ai_profiles WHERE player_id = ? AND state = 'retired'", retired); still != 1 {
		t.Fatalf("the retired player came back")
	}
	if after := count(t, ctx, universeWorld, "SELECT COUNT(*) FROM accounts"); after != accounts {
		t.Fatalf("accounts = %d, want %d", after, accounts)
	}
}

// TestNothingIsBornBeforeTheUniverseRuns proves the reconciler keeps quiet
// while the wizard is still open, and while no ruleset has been settled.
func TestNothingIsBornBeforeTheUniverseRuns(t *testing.T) {
	for _, this := range []struct {
		name    string
		prepare string
	}{
		{"the wizard is still open", "UPDATE server_state SET state = 'SETUP_IN_PROGRESS' WHERE id = 1"},
		{"the universe is paused", "UPDATE server_state SET state = 'PAUSED' WHERE id = 1"},
		{"no state was ever written", "DELETE FROM server_state"},
		{"no ruleset is active", "DELETE FROM ruleset_versions"},
	} {
		t.Run(this.name, func(t *testing.T) {
			ctx := context.Background()
			universeWorld := populationWorld(t, ctx, 1, artificialPopulation(nil))
			if _, err := universeWorld.Database.Write().ExecContext(ctx, this.prepare); err != nil {
				t.Fatal(err)
			}
			if born, err := universeWorld.Population.Populate(ctx, 5); err != nil || born != 0 {
				t.Fatalf("Populate() = %d, %v, want nobody and no error", born, err)
			}
			if total := count(t, ctx, universeWorld, "SELECT COUNT(*) FROM ai_profiles"); total != 0 {
				t.Fatalf("artificial profiles = %d, want none", total)
			}
		})
	}
}

// TestAnEndHourOfTwentyFourIsAnEndOfDay covers the one value the wizard accepts
// and a profile cannot hold. Without settling it, every birth would be refused.
func TestAnEndHourOfTwentyFourIsAnEndOfDay(t *testing.T) {
	ctx := context.Background()
	universeWorld := populationWorld(t, ctx, 1, artificialPopulation(func(configured *rules.Ruleset) {
		configured.AI.Total = 3
		configured.AI.AllianceCount = 0
		configured.AI.ActivityEndHour = 24
	}))

	if born := settle(t, ctx, universeWorld); born != 3 {
		t.Fatalf("settle() = %d, want 3", born)
	}
	if ends := count(t, ctx, universeWorld,
		"SELECT COUNT(*) FROM ai_profiles WHERE activity_end_hour = 0"); ends != 3 {
		t.Fatalf("profiles ending at midnight = %d, want 3", ends)
	}
}

// TestANameAlreadyWornIsSteppedOver proves a name a human took does not cost
// the universe a player.
func TestANameAlreadyWornIsSteppedOver(t *testing.T) {
	ctx := context.Background()
	universeWorld := populationWorld(t, ctx, 1, artificialPopulation(func(configured *rules.Ruleset) {
		configured.AI.Total = 4
		configured.AI.AllianceCount = 0
	}))
	if _, err := universeWorld.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: 1}, domainai.Name(0)); err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}

	if born := settle(t, ctx, universeWorld); born != 4 {
		t.Fatalf("settle() = %d, want 4", born)
	}
	if names := count(t, ctx, universeWorld, "SELECT COUNT(DISTINCT display_name) FROM players"); names != 5 {
		t.Fatalf("distinct player names = %d, want 5", names)
	}
}

// TestArtificialEmpiresDoNotPileUpOnTheHumans proves a population founded in
// one go is spread over the map instead of stacked in the corner the first
// players hold.
func TestArtificialEmpiresDoNotPileUpOnTheHumans(t *testing.T) {
	ctx := context.Background()
	universeWorld := populationWorld(t, ctx, 1, artificialPopulation(func(configured *rules.Ruleset) {
		configured.AI.Total = 12
		configured.AI.AllianceCount = 0
	}))
	settle(t, ctx, universeWorld)

	if galaxies := count(t, ctx, universeWorld, "SELECT COUNT(DISTINCT galaxy) FROM planets"); galaxies != 3 {
		t.Fatalf("galaxies settled = %d, want 3", galaxies)
	}
	if systems := count(t, ctx, universeWorld, "SELECT COUNT(DISTINCT galaxy || ':' || system) FROM planets"); systems < 8 {
		t.Fatalf("systems settled = %d, want the population spread wider", systems)
	}
	if corner := count(t, ctx, universeWorld, "SELECT COUNT(*) FROM planets WHERE galaxy = 1 AND system = 1"); corner != 0 {
		t.Fatalf("%d artificial empires settled in 1:1, where a human registers", corner)
	}
}

// TestHumansStillSettleWhereTheyAlwaysHave guards the promise that spreading a
// population out changed nothing for whoever registers by hand.
func TestHumansStillSettleWhereTheyAlwaysHave(t *testing.T) {
	ctx := context.Background()
	universeWorld := populationWorld(t, ctx, 2, artificialPopulation(func(configured *rules.Ruleset) {
		configured.AI.Total = 6
		configured.AI.AllianceCount = 0
	}))

	first, err := universeWorld.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: 1}, "Alice")
	if err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	if first.Coordinate.String() != "1:1:8" {
		t.Fatalf("first empire = %s, want 1:1:8", first.Coordinate)
	}
	settle(t, ctx, universeWorld)
	second, err := universeWorld.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: 2}, "Bob")
	if err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	if second.Coordinate.String() != "1:1:1" {
		t.Fatalf("second empire = %s, want 1:1:1 even with a population on the map", second.Coordinate)
	}
}

// TestAnArtificialPlayerBornThisWayThinksLikeAnyOther proves the population is
// alive, not merely present: the players the reconciler founded reflect through
// the very same brain as one an administrator adds by hand.
func TestAnArtificialPlayerBornThisWayThinksLikeAnyOther(t *testing.T) {
	ctx := context.Background()
	universeWorld := populationWorld(t, ctx, 1, artificialPopulation(func(configured *rules.Ruleset) {
		configured.AI.Total = 3
		configured.AI.AllianceCount = 0
	}))
	settle(t, ctx, universeWorld)

	universeWorld.Clock.Advance(time.Hour)
	think(t, ctx, universeWorld)

	if decisions := count(t, ctx, universeWorld, "SELECT COUNT(DISTINCT player_id) FROM ai_decisions"); decisions != 3 {
		t.Fatalf("players that wrote a decision = %d, want 3", decisions)
	}
}
