package tests

import (
	"context"
	"testing"
	"time"

	appai "universeatwar/internal/app/ai"
	appauth "universeatwar/internal/app/authentication"
	domainai "universeatwar/internal/domain/ai"
	storagesqlite "universeatwar/internal/storage/sqlite"
)

// TestArtificialPlayerSpiesBeforeItRaids proves an artificial player learns
// what it knows the way everybody else does, and never strikes blind.
func TestArtificialPlayerSpiesBeforeItRaids(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, admin := aiUniverse(t)
	setClock(t, universeWorld.Clock, time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC))

	if _, err := universeWorld.AI.Create(ctx, admin, appai.Request{
		Name: "Corsaire", Archetype: domainai.Raider,
		Window: domainai.Window{Start: 0, End: 0}, Interval: 5 * time.Minute,
	}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	setUnits(t, ctx, database, 2, "espionage_probe", 5)
	setUnits(t, ctx, database, 2, "light_fighter", 40)
	setResearch(t, ctx, database, 2, "espionage_technology", 3)
	setResearch(t, ctx, database, 2, "computer_technology", 3)
	setResources(t, ctx, database, 2, 200000, 200000, 200000)
	// A neighbour worth the trip, with nothing to defend itself.
	setResources(t, ctx, database, 1, 60000, 40000, 10000)

	think(t, ctx, universeWorld)
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM fleets WHERE mission = 'espionage' AND owner_player_id = 2", 1)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM fleets WHERE mission = 'attack'", 0)
	assertSingleValue(t, database,
		"SELECT COUNT(*) > 0 FROM ai_decisions WHERE action LIKE 'spy %' AND outcome = 'done'", 1)

	// Once the probes are back with a complete report, the raid follows.
	var arrival string
	if err := database.Read().QueryRowContext(ctx,
		"SELECT returns_at FROM fleets WHERE mission = 'espionage'").Scan(&arrival); err != nil {
		t.Fatal(err)
	}
	returnsAt, err := time.Parse(time.RFC3339Nano, arrival)
	if err != nil {
		t.Fatal(err)
	}
	setClock(t, universeWorld.Clock, returnsAt.Add(time.Minute))
	if _, err := universeWorld.Events.CompleteDue(ctx, 100); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM reports WHERE recipient_player_id = 2 AND kind = 'espionage'", 1)

	think(t, ctx, universeWorld)
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM fleets WHERE mission = 'attack' AND owner_player_id = 2", 1)
	// It took its fighters and enough holds for what the report promised.
	assertSingleValue(t, database, `
		SELECT COUNT(*) > 0 FROM fleet_ships s JOIN fleets f ON f.id = s.fleet_id
		WHERE f.mission = 'attack' AND s.unit_id = 'light_fighter'`, 1)
	assertSingleValue(t, database, `
		SELECT COUNT(*) FROM fleet_ships s JOIN fleets f ON f.id = s.fleet_id
		WHERE f.mission = 'attack' AND s.unit_id IN ('espionage_probe', 'rocket_launcher')`, 0)
	// And it wrote down what it believed, from its own report and nothing else.
	assertSingleValue(t, database, "SELECT COUNT(*) > 0 FROM ai_memory WHERE kind = 'target'", 1)
	assertSingleValue(t, database,
		"SELECT COUNT(*) > 0 FROM ai_decisions WHERE action LIKE 'raid %' AND outcome = 'done'", 1)

	// Once the combat report exists, the same snapshot cannot drive an endless
	// loop of raids. The target cools down, then must be observed again.
	var attackArrivalText string
	if err := database.Read().QueryRowContext(ctx, `
		SELECT arrives_at FROM fleets
		WHERE mission = 'attack' AND owner_player_id = 2 ORDER BY id DESC LIMIT 1
	`).Scan(&attackArrivalText); err != nil {
		t.Fatal(err)
	}
	attackArrival, err := time.Parse(time.RFC3339Nano, attackArrivalText)
	if err != nil {
		t.Fatal(err)
	}
	setClock(t, universeWorld.Clock, attackArrival)
	if _, err := universeWorld.Events.CompleteDue(ctx, 200); err != nil {
		t.Fatal(err)
	}
	var raidedText string
	if err := database.Read().QueryRowContext(ctx, `
		SELECT occurred_at FROM reports
		WHERE recipient_player_id = 2 AND kind = 'combat_attack' ORDER BY id DESC LIMIT 1
	`).Scan(&raidedText); err != nil {
		t.Fatal(err)
	}
	raidedAt, err := time.Parse(time.RFC3339Nano, raidedText)
	if err != nil {
		t.Fatal(err)
	}
	setClock(t, universeWorld.Clock, raidedAt.Add(10*time.Minute))
	think(t, ctx, universeWorld)
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM fleets WHERE mission = 'attack' AND owner_player_id = 2", 1)
	setClock(t, universeWorld.Clock, raidedAt.Add(31*time.Minute))
	think(t, ctx, universeWorld)
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM fleets WHERE mission = 'espionage' AND owner_player_id = 2", 2)
}

// TestArtificialPlayerBuildsScoutsAndFindsADistantTarget covers the complete
// autonomous loop that used to be impossible: the planner builds probes and a
// combat fleet, looks outside its home system, refreshes its report and attacks.
func TestArtificialPlayerBuildsScoutsAndFindsADistantTarget(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, admin := aiUniverse(t)
	setClock(t, universeWorld.Clock, time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC))
	// Leave the AI alone in system 1. The human target is public, but two
	// systems away, so a home-system-only scout would never discover it.
	if _, err := database.Write().ExecContext(ctx, "UPDATE planets SET system = 3 WHERE id = 1"); err != nil {
		t.Fatal(err)
	}
	profile, err := universeWorld.AI.Create(ctx, admin, appai.Request{
		Name: "Autonome", Archetype: domainai.Raider,
		Window: domainai.Window{Start: 0, End: 0}, Interval: 5 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	custom := domainai.Raider.Tuning()
	custom.DefenceShare = 0
	custom.BatchSize = 5
	profile, err = universeWorld.AI.Update(ctx, admin, profile.PlayerID, appai.UpdateRequest{
		Version: profile.Version, Archetype: domainai.Raider,
		Window: profile.Window, Interval: profile.Interval, Custom: &custom,
	})
	if err != nil {
		t.Fatal(err)
	}
	setBuilding(t, ctx, database, 2, "shipyard", 3)
	setBuilding(t, ctx, database, 2, "research_lab", 4)
	setResearch(t, ctx, database, profile.PlayerID, "combustion_drive", 3)
	setResearch(t, ctx, database, profile.PlayerID, "espionage_technology", 3)
	setResearch(t, ctx, database, profile.PlayerID, "computer_technology", 3)
	setResources(t, ctx, database, 2, 500000, 500000, 500000)
	setResources(t, ctx, database, 1, 100000, 80000, 40000)

	// First reflection: no fixture gives it probes; it orders them itself.
	think(t, ctx, universeWorld)
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM production_orders WHERE planet_id = 2 AND unit_id = 'espionage_probe'", 1)
	advanceToProduction(t, ctx, database, universeWorld, "espionage_probe")
	assertSingleValue(t, database,
		"SELECT quantity > 0 FROM planet_units WHERE planet_id = 2 AND unit_id = 'espionage_probe'", 1)

	// It now secures the missing cargo holds and sends its own probes to system
	// 3. Utility batches are completed before the yard turns to combat.
	setResources(t, ctx, database, 2, 500000, 500000, 500000)
	think(t, ctx, universeWorld)
	assertSingleValue(t, database,
		"SELECT COUNT(*) > 0 FROM production_orders WHERE planet_id = 2 AND unit_id IN ('small_cargo', 'large_cargo')", 1)
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM fleets WHERE owner_player_id = ? AND mission = 'espionage' AND target_system = 3", 1, profile.PlayerID)
	finishProduction(t, ctx, database, universeWorld, 2)
	setResources(t, ctx, database, 2, 500000, 500000, 500000)
	think(t, ctx, universeWorld)
	for cycle := 0; cycle < 6; cycle++ {
		var fighters int
		if err := database.Read().QueryRowContext(ctx, `
			SELECT COUNT(*) FROM production_orders
			WHERE planet_id = 2 AND unit_id = 'light_fighter'
		`).Scan(&fighters); err != nil {
			t.Fatal(err)
		}
		if fighters > 0 {
			break
		}
		finishProduction(t, ctx, database, universeWorld, 2)
		setResources(t, ctx, database, 2, 500000, 500000, 500000)
		think(t, ctx, universeWorld)
	}
	assertSingleValue(t, database,
		"SELECT COUNT(*) > 0 FROM production_orders WHERE planet_id = 2 AND unit_id = 'light_fighter'", 1)

	// Fighters take longer than a report stays fresh. Once they are ready the
	// AI refreshes the intelligence, then attacks on the following reflection.
	finishProduction(t, ctx, database, universeWorld, 2)
	setResources(t, ctx, database, 2, 500000, 500000, 500000)
	think(t, ctx, universeWorld)
	var attacks int
	if err := database.Read().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM fleets WHERE owner_player_id = ? AND mission = 'attack'", profile.PlayerID).Scan(&attacks); err != nil {
		t.Fatal(err)
	}
	if attacks == 0 {
		var returnsText string
		if err := database.Read().QueryRowContext(ctx, `
		SELECT returns_at FROM fleets WHERE owner_player_id = ? AND mission = 'espionage'
			AND state IN ('outbound', 'returning') ORDER BY id DESC LIMIT 1
	`, profile.PlayerID).Scan(&returnsText); err != nil {
			t.Fatal(err)
		}
		returnsAt, err := time.Parse(time.RFC3339Nano, returnsText)
		if err != nil {
			t.Fatal(err)
		}
		setClock(t, universeWorld.Clock, returnsAt)
		if _, err := universeWorld.Events.CompleteDue(ctx, 100); err != nil {
			t.Fatal(err)
		}
		think(t, ctx, universeWorld)
	}
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM fleets WHERE owner_player_id = ? AND mission = 'attack' AND target_system = 3", 1, profile.PlayerID)
}

// TestArtificialPlayerSpacesOutReconnaissanceAndRotatesTargets is the
// regression for an AI probing the same uninteresting planet at every thought.
// One mission is allowed at a time, a recent report pauses the campaign, and a
// still-useful report makes the scout move on to another body.
func TestArtificialPlayerSpacesOutReconnaissanceAndRotatesTargets(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, admin := aiUniverse(t)
	setClock(t, universeWorld.Clock, time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC))

	profile, err := universeWorld.AI.Create(ctx, admin, appai.Request{
		Name: "Observateur", Archetype: domainai.Raider,
		Window: domainai.Window{Start: 0, End: 0}, Interval: 5 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Alice has two poor planets in the AI's home system. Neither justifies an
	// attack, but both are legitimate discoveries.
	if _, err := database.Write().ExecContext(ctx, `
		INSERT INTO planets(owner_player_id, name, galaxy, system, position, total_fields,
			minimum_temperature, maximum_temperature, created_at)
		VALUES (1, 'Avant-poste', 1, 1, 2, 150, 10, 50, '2042-09-10T12:00:00Z')
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Write().ExecContext(ctx,
		"INSERT INTO planet_resources(planet_id, produced_at) VALUES (3, '2042-09-10T12:00:00Z')"); err != nil {
		t.Fatal(err)
	}
	setUnits(t, ctx, database, 2, "espionage_probe", 6)
	setResearch(t, ctx, database, profile.PlayerID, "espionage_technology", 3)
	setResearch(t, ctx, database, profile.PlayerID, "computer_technology", 3)
	setResources(t, ctx, database, 2, 200000, 200000, 200000)

	think(t, ctx, universeWorld)
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM fleets WHERE owner_player_id = ? AND mission = 'espionage'", 1, profile.PlayerID)
	var returnsText string
	if err := database.Read().QueryRowContext(ctx, `
		SELECT returns_at FROM fleets WHERE owner_player_id = ? AND mission = 'espionage'
		ORDER BY id LIMIT 1
	`, profile.PlayerID).Scan(&returnsText); err != nil {
		t.Fatal(err)
	}
	returnsAt, err := time.Parse(time.RFC3339Nano, returnsText)
	if err != nil {
		t.Fatal(err)
	}
	// Even with spare probes and slots, the next thought does not fan out more
	// missions while the first reconnaissance is underway.
	now := universeWorld.Clock.Now()
	setClock(t, universeWorld.Clock, now.Add(returnsAt.Sub(now)/2))
	think(t, ctx, universeWorld)
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM fleets WHERE owner_player_id = ? AND mission = 'espionage'", 1, profile.PlayerID)

	setClock(t, universeWorld.Clock, returnsAt)
	if _, err := universeWorld.Events.CompleteDue(ctx, 100); err != nil {
		t.Fatal(err)
	}
	think(t, ctx, universeWorld)
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM fleets WHERE owner_player_id = ? AND mission = 'espionage'", 1, profile.PlayerID)

	// Once the one-hour reconnaissance cadence has elapsed, the known planet
	// is still covered, so the next probe goes to Alice's other planet.
	setClock(t, universeWorld.Clock, returnsAt.Add(time.Hour+time.Minute))
	think(t, ctx, universeWorld)
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM fleets WHERE owner_player_id = ? AND mission = 'espionage'", 2, profile.PlayerID)
	assertSingleValue(t, database, `
		SELECT COUNT(DISTINCT target_position) FROM fleets
		WHERE owner_player_id = ? AND mission = 'espionage'`, 2, profile.PlayerID)
}

func advanceToProduction(t *testing.T, ctx context.Context, database *storagesqlite.Database,
	universeWorld *world, unitID string) {
	t.Helper()
	var completesText string
	if err := database.Read().QueryRowContext(ctx, `
		SELECT completes_at FROM production_orders WHERE planet_id = 2 AND unit_id = ?
		AND completes_at IS NOT NULL ORDER BY id LIMIT 1
	`, unitID).Scan(&completesText); err != nil {
		t.Fatal(err)
	}
	completesAt, err := time.Parse(time.RFC3339Nano, completesText)
	if err != nil {
		t.Fatal(err)
	}
	setClock(t, universeWorld.Clock, completesAt)
	if _, err := universeWorld.Events.CompleteDue(ctx, 100); err != nil {
		t.Fatal(err)
	}
}

func finishProduction(t *testing.T, ctx context.Context, database *storagesqlite.Database,
	universeWorld *world, planetID int64) {
	t.Helper()
	for cycle := 0; cycle < 20; cycle++ {
		var completesText string
		err := database.Read().QueryRowContext(ctx, `
			SELECT completes_at FROM production_orders
			WHERE planet_id = ? AND state = 'active' ORDER BY completes_at LIMIT 1
		`, planetID).Scan(&completesText)
		if err != nil {
			return
		}
		completesAt, err := time.Parse(time.RFC3339Nano, completesText)
		if err != nil {
			t.Fatal(err)
		}
		setClock(t, universeWorld.Clock, completesAt)
		if _, err := universeWorld.Events.CompleteDue(ctx, 100); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("production queue did not settle")
}

// TestAnAgeingReportCanCostTheFleet proves an artificial player acts on what it
// last saw, not on the truth, and pays for it like anybody else.
func TestAnAgeingReportCanCostTheFleet(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, admin := aiUniverse(t)
	setClock(t, universeWorld.Clock, time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC))

	if _, err := universeWorld.AI.Create(ctx, admin, appai.Request{
		Name: "Imprudent", Archetype: domainai.Raider,
		Window: domainai.Window{Start: 0, End: 0}, Interval: 5 * time.Minute,
	}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	setUnits(t, ctx, database, 2, "espionage_probe", 5)
	setUnits(t, ctx, database, 2, "light_fighter", 12)
	setResearch(t, ctx, database, 2, "espionage_technology", 3)
	setResearch(t, ctx, database, 2, "computer_technology", 3)
	setResources(t, ctx, database, 2, 200000, 200000, 200000)
	setResources(t, ctx, database, 1, 60000, 40000, 10000)

	think(t, ctx, universeWorld)
	var arrival string
	if err := database.Read().QueryRowContext(ctx,
		"SELECT returns_at FROM fleets WHERE mission = 'espionage'").Scan(&arrival); err != nil {
		t.Fatal(err)
	}
	returnsAt, err := time.Parse(time.RFC3339Nano, arrival)
	if err != nil {
		t.Fatal(err)
	}
	setClock(t, universeWorld.Clock, returnsAt.Add(time.Minute))
	if _, err := universeWorld.Events.CompleteDue(ctx, 100); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}

	// Between the look and the strike, the neighbour digs in. The report says
	// nothing of it, and the report is all the raider has.
	setUnits(t, ctx, database, 1, "rocket_launcher", 60)
	setUnits(t, ctx, database, 1, "heavy_laser", 40)
	think(t, ctx, universeWorld)
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM fleets WHERE mission = 'attack' AND owner_player_id = 2", 1)

	var attackArrival string
	if err := database.Read().QueryRowContext(ctx,
		"SELECT arrives_at FROM fleets WHERE mission = 'attack'").Scan(&attackArrival); err != nil {
		t.Fatal(err)
	}
	arrivesAt, err := time.Parse(time.RFC3339Nano, attackArrival)
	if err != nil {
		t.Fatal(err)
	}
	setClock(t, universeWorld.Clock, arrivesAt)
	if _, err := universeWorld.Events.CompleteDue(ctx, 100); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}
	assertSingleText(t, database,
		`SELECT json_extract(payload, '$.outcome') FROM game_event_log WHERE event_type = 'combat_resolved'`,
		"defender")
	assertSingleText(t, database, "SELECT state FROM fleets WHERE mission = 'attack'", "destroyed")
}

// TestFleetsaveEmptiesTheGroundBeforeTheNight proves an artificial player puts
// its ships out of reach on its last reflection of the day.
func TestFleetsaveEmptiesTheGroundBeforeTheNight(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, admin := aiUniverse(t)
	setClock(t, universeWorld.Clock, time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC))

	if _, err := universeWorld.AI.Create(ctx, admin, appai.Request{
		Name: "Prudent", Archetype: domainai.CautiousMiner,
		Window: domainai.Window{Start: 8, End: 20}, Interval: time.Hour,
	}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	// A second body, without which there is nowhere to run.
	if _, err := database.Write().ExecContext(ctx, `
		INSERT INTO planets(owner_player_id, name, galaxy, system, position, total_fields,
			minimum_temperature, maximum_temperature, created_at)
		VALUES (2, 'Colonie', 1, 4, 6, 150, 10, 50, '2042-09-10T12:00:00Z')
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Write().ExecContext(ctx,
		"INSERT INTO planet_resources(planet_id, produced_at) VALUES (3, '2042-09-10T12:00:00Z')"); err != nil {
		t.Fatal(err)
	}
	setUnits(t, ctx, database, 2, "light_fighter", 25)
	setUnits(t, ctx, database, 2, "rocket_launcher", 10)
	setResearch(t, ctx, database, 2, "computer_technology", 3)
	setResources(t, ctx, database, 2, 200000, 200000, 200000)

	// The last reflection of the day: the next one would already be at night.
	setClock(t, universeWorld.Clock, time.Date(2042, time.September, 10, 19, 30, 0, 0, time.UTC))
	think(t, ctx, universeWorld)
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM fleets WHERE mission = 'transport' AND owner_player_id = 2", 1)
	assertSingleValue(t, database, `
		SELECT quantity FROM fleet_ships s JOIN fleets f ON f.id = s.fleet_id
		WHERE f.mission = 'transport' AND s.unit_id = 'light_fighter'`, 25)
	// The turrets stay: they cannot fly and they are what defends the ground.
	assertSingleValue(t, database,
		"SELECT quantity FROM planet_units WHERE planet_id = 2 AND unit_id = 'rocket_launcher'", 10)
	assertSingleValue(t, database,
		"SELECT COUNT(*) > 0 FROM ai_decisions WHERE action LIKE 'fleetsave %' AND outcome = 'done'", 1)
}

// think forces one reflection right away: the schedule is moved to now, the
// event fires and the brain acts.
func think(t *testing.T, ctx context.Context, universeWorld *world) {
	t.Helper()
	stamp := universeWorld.Clock.Now().UTC().Format(time.RFC3339Nano)
	if _, err := universeWorld.Database.Write().ExecContext(ctx,
		"UPDATE ai_profiles SET next_think_at = ? WHERE state = 'active'", stamp); err != nil {
		t.Fatal(err)
	}
	if _, err := universeWorld.Database.Write().ExecContext(ctx,
		"UPDATE scheduled_events SET due_at = ? WHERE event_type = 'ai_think' AND state = 'pending'",
		stamp); err != nil {
		t.Fatal(err)
	}
	if _, err := universeWorld.Events.CompleteDue(ctx, 100); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}
	if _, err := universeWorld.Brain.ThinkDue(ctx, 10); err != nil {
		t.Fatalf("ThinkDue() error = %v", err)
	}
}

// TestArtificialPlayerLiftsDebrisItCanSee proves an artificial player collects
// what the map shows to everybody, with just enough recyclers for the field.
func TestArtificialPlayerLiftsDebrisItCanSee(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, admin := aiUniverse(t)
	setClock(t, universeWorld.Clock, time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC))

	if _, err := universeWorld.AI.Create(ctx, admin, appai.Request{
		Name: "Ferrailleur", Archetype: domainai.Logistician,
		Window: domainai.Window{Start: 0, End: 0}, Interval: 5 * time.Minute,
	}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	setUnits(t, ctx, database, 2, "recycler", 10)
	setResearch(t, ctx, database, 2, "computer_technology", 3)
	setResources(t, ctx, database, 2, 200000, 200000, 200000)

	// A field left by somebody else's battle, in plain sight on the map.
	home, err := universeWorld.Economy.Planet(ctx, appauth.Principal{AccountID: 1}, 1)
	if err != nil {
		t.Fatalf("Planet() error = %v", err)
	}
	if _, err := database.Write().ExecContext(ctx, `
		INSERT INTO debris_fields(galaxy, system, position, metal, crystal, created_at, updated_at)
		VALUES (?, ?, ?, 45000, 15000, '2042-09-10T12:00:00Z', '2042-09-10T12:00:00Z')
	`, home.Coordinate.Galaxy, home.Coordinate.System, home.Coordinate.Position); err != nil {
		t.Fatal(err)
	}

	think(t, ctx, universeWorld)
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM fleets WHERE mission = 'recycle' AND owner_player_id = 2", 1)
	// Sixty thousand of wreckage, twenty thousand a hold: three recyclers.
	assertSingleValue(t, database, `
		SELECT quantity FROM fleet_ships s JOIN fleets f ON f.id = s.fleet_id
		WHERE f.mission = 'recycle' AND s.unit_id = 'recycler'`, 3)
	assertSingleValue(t, database,
		"SELECT COUNT(*) > 0 FROM ai_decisions WHERE action LIKE 'recycle %' AND outcome = 'done'", 1)
}
