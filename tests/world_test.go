package tests

import (
	"context"
	"crypto/rand"
	"path/filepath"
	"testing"

	appeconomy "universeatwar/internal/app/economy"
	appfleet "universeatwar/internal/app/fleet"
	appresearch "universeatwar/internal/app/research"
	appshipyard "universeatwar/internal/app/shipyard"
	appclock "universeatwar/internal/clock"
	"universeatwar/internal/domain/catalogue"
	"universeatwar/internal/domain/universe"
	"universeatwar/internal/random"
	storagesqlite "universeatwar/internal/storage/sqlite"
)

// world wires the same services as the serve command so that acceptance tests
// exercise the production composition instead of a parallel assembly.
type world struct {
	Database *storagesqlite.Database
	Clock    *appclock.Fake
	Events   *storagesqlite.EventProcessor
	Economy  appeconomy.Service
	Research appresearch.Service
	Shipyard appshipyard.Service
	Fleet    appfleet.Service
}

func newWorld(t *testing.T, database *storagesqlite.Database, clock *appclock.Fake) *world {
	t.Helper()
	catalogues := catalogue.Default()
	economyRepository := storagesqlite.NewEconomyRepository(database.Write(), catalogues.Buildings)
	researchRepository := storagesqlite.NewResearchRepository(database.Write(), catalogues)
	shipyardRepository := storagesqlite.NewShipyardRepository(database.Write(), catalogues)
	fleetRepository := storagesqlite.NewFleetRepository(database.Write(), catalogues)
	events := storagesqlite.NewEventProcessor(database.Write(), clock)
	economyRepository.RegisterHandlers(events)
	researchRepository.RegisterHandlers(events)
	shipyardRepository.RegisterHandlers(events)
	fleetRepository.RegisterHandlers(events)
	return &world{
		Database: database,
		Clock:    clock,
		Events:   events,
		Economy: appeconomy.Service{
			Clock:      clock,
			Repository: economyRepository,
			Catalogue:  catalogues.Buildings,
			Completer:  events,
		},
		Research: appresearch.Service{
			Clock:      clock,
			Repository: researchRepository,
			Catalogues: catalogues,
			Completer:  events,
		},
		Shipyard: appshipyard.Service{
			Clock:      clock,
			Repository: shipyardRepository,
			Catalogues: catalogues,
			Completer:  events,
		},
		Fleet: appfleet.Service{
			Clock:      clock,
			Repository: fleetRepository,
			Catalogues: catalogues,
			Seeds:      random.NewSeedGenerator(rand.Reader),
			Completer:  events,
		},
	}
}

func setBuilding(t *testing.T, ctx context.Context, database *storagesqlite.Database, planetID int64, id string, level int) {
	t.Helper()
	if _, err := database.Write().ExecContext(ctx, `
		INSERT INTO planet_buildings(planet_id, building_id, level) VALUES (?, ?, ?)
		ON CONFLICT(planet_id, building_id) DO UPDATE SET level = excluded.level
	`, planetID, id, level); err != nil {
		t.Fatalf("set building %s: %v", id, err)
	}
}

func setResearch(t *testing.T, ctx context.Context, database *storagesqlite.Database, playerID int64, id string, level int) {
	t.Helper()
	if _, err := database.Write().ExecContext(ctx, `
		INSERT INTO player_research(player_id, research_id, level) VALUES (?, ?, ?)
		ON CONFLICT(player_id, research_id) DO UPDATE SET level = excluded.level
	`, playerID, id, level); err != nil {
		t.Fatalf("set research %s: %v", id, err)
	}
}

func setUnits(t *testing.T, ctx context.Context, database *storagesqlite.Database, planetID int64, id string, quantity int64) {
	t.Helper()
	if _, err := database.Write().ExecContext(ctx, `
		INSERT INTO planet_units(planet_id, unit_id, quantity) VALUES (?, ?, ?)
		ON CONFLICT(planet_id, unit_id) DO UPDATE SET quantity = excluded.quantity
	`, planetID, id, quantity); err != nil {
		t.Fatalf("set units %s: %v", id, err)
	}
}

func setResources(t *testing.T, ctx context.Context, database *storagesqlite.Database, planetID int64, metal, crystal, deuterium int64) {
	t.Helper()
	if _, err := database.Write().ExecContext(ctx, `
		UPDATE planet_resources SET metal = ?, crystal = ?, deuterium = ? WHERE planet_id = ?
	`, metal, crystal, deuterium, planetID); err != nil {
		t.Fatalf("set resources: %v", err)
	}
}

// benchmarkDatabase prepares an empty universe for a benchmark.
func benchmarkDatabase(b *testing.B, ctx context.Context) *storagesqlite.Database {
	b.Helper()
	database, err := storagesqlite.Open(ctx, filepath.Join(b.TempDir(), "benchmark.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(ctx); err != nil {
		b.Fatal(err)
	}
	return database
}

func assertSingleText(t *testing.T, database *storagesqlite.Database, query string, want string) {
	t.Helper()
	var got string
	if err := database.Read().QueryRow(query).Scan(&got); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	if got != want {
		t.Fatalf("query %q = %q, want %q", query, got, want)
	}
}

func coordinateOf(t *testing.T, galaxy, system, position int) universe.Coordinate {
	t.Helper()
	return universe.Coordinate{Galaxy: galaxy, System: system, Position: position}
}
