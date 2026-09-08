package tests

import (
	"context"
	"testing"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appfleet "universeatwar/internal/app/fleet"
	appclock "universeatwar/internal/clock"
	"universeatwar/internal/domain/building"
	domaineconomy "universeatwar/internal/domain/economy"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/unit"
	storagesqlite "universeatwar/internal/storage/sqlite"
)

func TestABigBattleGathersAMoonExactlyOnce(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, alice, clock := moonBattlefield(t, 7)
	fought := resolveMoonBattle(t, ctx, database, universeWorld, alice, clock, 7, "first")

	if !fought {
		t.Fatal("a certain moon chance did not create a moon")
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM planets WHERE kind = 'moon'", 1)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM game_event_log WHERE event_type = 'moon_created'", 1)
	var owner, parent int64
	var fields int
	if err := database.Read().QueryRowContext(ctx,
		"SELECT owner_player_id, parent_planet_id, total_fields FROM planets WHERE kind = 'moon'").Scan(&owner, &parent, &fields); err != nil {
		t.Fatal(err)
	}
	if owner != 2 || parent != 2 {
		t.Fatalf("the moon belongs to player %d and orbits planet %d", owner, parent)
	}
	if fields != 1 {
		t.Fatalf("a new moon has %d fields, want the configured base of 1", fields)
	}

	// A second battle on the same position never adds a second moon.
	resolveMoonBattle(t, ctx, database, universeWorld, alice, clock, 9, "second")
	assertSingleValue(t, database, "SELECT COUNT(*) FROM planets WHERE kind = 'moon'", 1)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM game_event_log WHERE event_type = 'moon_created'", 1)
}

func TestMoonCreationIsReproducibleFromItsSeed(t *testing.T) {
	outcomes := make([]bool, 0, 2)
	for range 2 {
		ctx := context.Background()
		database, universeWorld, alice, clock := smallMoonBattlefield(t)
		outcomes = append(outcomes, resolveMoonBattle(t, ctx, database, universeWorld, alice, clock, 4242, "battle"))
	}
	if outcomes[0] != outcomes[1] {
		t.Fatalf("the same seed gave two different moons: %v", outcomes)
	}
}

func TestAMoonProducesNothingAndOnlyAcceptsLunarBuildings(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, alice, clock := moonBattlefield(t, 7)
	resolveMoonBattle(t, ctx, database, universeWorld, alice, clock, 7, "battle")

	bob := appauth.Principal{AccountID: 2}
	moonID := int64(3)
	assertSingleText(t, database, "SELECT kind FROM planets WHERE id = 3", "moon")
	setResources(t, ctx, database, moonID, 100000, 100000, 100000)

	moon, err := universeWorld.Economy.Planet(ctx, bob, moonID)
	if err != nil {
		t.Fatalf("Planet(moon) error = %v", err)
	}
	if moon.Kind != building.OnMoon || moon.Rates != (domaineconomy.Rates{}) {
		t.Fatalf("a moon produces something: %+v", moon.Rates)
	}
	clock.Advance(10 * time.Hour)
	settled, err := universeWorld.Economy.Planet(ctx, bob, moonID)
	if err != nil {
		t.Fatalf("Planet(moon) error = %v", err)
	}
	if settled.Stock.Metal != 100000 {
		t.Fatalf("a moon holds %d metal after ten hours, want the 100000 landed on it", settled.Stock.Metal)
	}

	_, choices, err := universeWorld.Economy.Buildings(ctx, bob, moonID)
	if err != nil {
		t.Fatalf("Buildings(moon) error = %v", err)
	}
	if len(choices) != 3 {
		t.Fatalf("the moon offers %d buildings, want the three lunar ones", len(choices))
	}
	for _, choice := range choices {
		if choice.Definition.Placement != building.OnMoon {
			t.Fatalf("the moon offers %s", choice.Definition.ID)
		}
	}
	if _, err := universeWorld.Economy.EnqueueBuilding(ctx, bob, moonID, building.MetalMine, "mine-on-moon"); err == nil {
		t.Fatal("a mine was accepted on a moon")
	}
	if _, err := universeWorld.Economy.EnqueueBuilding(ctx, bob, moonID, building.LunarBase, "base"); err != nil {
		t.Fatalf("EnqueueBuilding(lunar base) error = %v", err)
	}
	clock.Advance(200 * time.Hour)
	if _, err := universeWorld.Events.CompleteDue(ctx, 50); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}
	// The lunar base widens the moon by the configured number of fields.
	assertSingleValue(t, database, "SELECT total_fields FROM planets WHERE id = 3", 4)
	assertSingleValue(t, database, "SELECT used_fields FROM planets WHERE id = 3", 1)
}

// moonBattlefield prepares a battle whose wreckage is certain to gather a moon.
func moonBattlefield(t *testing.T, _ int64) (*storagesqlite.Database, *world, appauth.Principal, *appclock.Fake) {
	return battlefieldWithMoonChance(t, 1, 1, 2500)
}

// smallMoonBattlefield leaves the moon to chance, which is what the
// reproducibility test needs.
func smallMoonBattlefield(t *testing.T) (*storagesqlite.Database, *world, appauth.Principal, *appclock.Fake) {
	return battlefieldWithMoonChance(t, .2, .3, 400)
}

func battlefieldWithMoonChance(t *testing.T, maximumChance, shipsToDebris float64, defenders int64) (*storagesqlite.Database, *world, appauth.Principal, *appclock.Fake) {
	t.Helper()
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC))
	database := economyDatabase(t, ctx, 2)
	universeWorld := newWorld(t, database, clock)
	alice := appauth.Principal{AccountID: 1}

	home, err := universeWorld.Economy.CreateEmpire(ctx, alice, "Alice")
	if err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	target, err := universeWorld.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: 2}, "Bob")
	if err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	if _, err := database.Write().ExecContext(ctx, `
		UPDATE ruleset_versions
		SET document = json_set(json_set(document, '$.combat.maximum_moon_chance', ?), '$.combat.ships_to_debris', ?)
		WHERE status = 'active'
	`, maximumChance, shipsToDebris); err != nil {
		t.Fatal(err)
	}
	setUnits(t, ctx, database, home.ID, "battleship", 2000)
	setUnits(t, ctx, database, target.ID, "light_fighter", defenders)
	// A fleet that large burns a lot of deuterium, which needs somewhere to sit.
	setBuilding(t, ctx, database, home.ID, "deuterium_tank", 6)
	setResources(t, ctx, database, home.ID, 50000, 50000, 600000)
	return database, universeWorld, alice, clock
}

// resolveMoonBattle launches an attack with a forced seed and reports whether a
// moon appeared.
func resolveMoonBattle(t *testing.T, ctx context.Context, database *storagesqlite.Database, universeWorld *world,
	alice appauth.Principal, clock *appclock.Fake, seed int64, key string) bool {
	t.Helper()
	before := moonCount(t, ctx, database)
	launched, err := universeWorld.Fleet.Launch(ctx, alice, 1, appfleet.LaunchRequest{
		Target: coordinateOf(t, 1, 1, 1), TargetKind: domainfleet.TargetPlanet, Mission: domainfleet.MissionAttack,
		Composition: domainfleet.Composition{unit.Battleship: 2000}, Percent: 100,
	}, key)
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if _, err := database.Write().ExecContext(ctx, "UPDATE fleets SET seed = ? WHERE id = ?", seed, launched.ID); err != nil {
		t.Fatal(err)
	}
	clock.Set(launched.ArrivesAt)
	if _, err := universeWorld.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}
	clock.Advance(48 * time.Hour)
	if _, err := universeWorld.Events.CompleteDue(ctx, 50); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}
	return moonCount(t, ctx, database) > before
}

func moonCount(t *testing.T, ctx context.Context, database *storagesqlite.Database) int {
	t.Helper()
	var count int
	if err := database.Read().QueryRowContext(ctx, "SELECT COUNT(*) FROM planets WHERE kind = 'moon'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
