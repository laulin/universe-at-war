package tests

import (
	"context"
	"testing"
	"time"

	appai "universeatwar/internal/app/ai"
	appauth "universeatwar/internal/app/authentication"
	domainai "universeatwar/internal/domain/ai"
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
