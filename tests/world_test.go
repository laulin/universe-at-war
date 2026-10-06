package tests

import (
	"context"
	"crypto/rand"
	"fmt"
	"testing"
	"time"

	"universeatwar/internal/ai"
	appacs "universeatwar/internal/app/acs"
	appactivity "universeatwar/internal/app/activity"
	appai "universeatwar/internal/app/ai"
	appalliance "universeatwar/internal/app/alliance"
	appauth "universeatwar/internal/app/authentication"
	appbattlesimulation "universeatwar/internal/app/battlesimulation"
	appchat "universeatwar/internal/app/chat"
	appeconomy "universeatwar/internal/app/economy"
	appfleet "universeatwar/internal/app/fleet"
	appgalaxy "universeatwar/internal/app/galaxy"
	appjumpgate "universeatwar/internal/app/jumpgate"
	appphalanx "universeatwar/internal/app/phalanx"
	appreports "universeatwar/internal/app/reports"
	appresearch "universeatwar/internal/app/research"
	appshipyard "universeatwar/internal/app/shipyard"
	appclock "universeatwar/internal/clock"
	"universeatwar/internal/domain/catalogue"
	domaineconomy "universeatwar/internal/domain/economy"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/random"
	"universeatwar/internal/domain/rules"
	"universeatwar/internal/domain/unit"
	"universeatwar/internal/domain/universe"
	storagesqlite "universeatwar/internal/storage/sqlite"
)

// world wires the same services as the serve command so that acceptance tests
// exercise the production composition instead of a parallel assembly.
type world struct {
	Database         *storagesqlite.Database
	Clock            *appclock.Fake
	Events           *storagesqlite.EventProcessor
	Activity         appactivity.Service
	Economy          appeconomy.Service
	Research         appresearch.Service
	Shipyard         appshipyard.Service
	Fleet            appfleet.Service
	Galaxy           appgalaxy.Service
	Reports          appreports.Service
	BattleSimulation appbattlesimulation.Service
	Phalanx          appphalanx.Service
	JumpGate         appjumpgate.Service
	Alliance         appalliance.Service
	Chat             appchat.Service
	ACS              appacs.Service
	AI               appai.Service
	Population       appai.Populating
	Thinking         appai.Thinking
	Teamwork         appai.Teamwork
	Brain            *ai.Brain
}

func newWorld(t testing.TB, database *storagesqlite.Database, clock *appclock.Fake) *world {
	t.Helper()
	catalogues := catalogue.Default()
	economyRepository := storagesqlite.NewEconomyRepository(database.Write(), catalogues.Buildings)
	researchRepository := storagesqlite.NewResearchRepository(database.Write(), catalogues)
	shipyardRepository := storagesqlite.NewShipyardRepository(database.Write(), catalogues)
	fleetRepository := storagesqlite.NewFleetRepository(database.Write(), catalogues)
	acsRepository := storagesqlite.NewACSRepository(database.Write(), catalogues, fleetRepository)
	aiRepository := storagesqlite.NewAIRepository(database.Write())
	events := storagesqlite.NewEventProcessor(database.Write(), clock)
	economyRepository.RegisterHandlers(events)
	researchRepository.RegisterHandlers(events)
	shipyardRepository.RegisterHandlers(events)
	fleetRepository.RegisterHandlers(events)
	acsRepository.RegisterHandlers(events)
	aiRepository.RegisterHandlers(events)
	economy := appeconomy.Service{
		Clock:      clock,
		Repository: economyRepository,
		Catalogue:  catalogues.Buildings,
		Completer:  events,
	}
	research := appresearch.Service{
		Clock:      clock,
		Repository: researchRepository,
		Catalogues: catalogues,
		Completer:  events,
	}
	shipyard := appshipyard.Service{
		Clock:      clock,
		Repository: shipyardRepository,
		Catalogues: catalogues,
		Completer:  events,
	}
	fleetService := appfleet.Service{
		Clock:      clock,
		Repository: fleetRepository,
		Catalogues: catalogues,
		Seeds:      random.NewSeedGenerator(rand.Reader),
		Completer:  events,
	}
	activityService := appactivity.Service{
		Repository: storagesqlite.NewActivityRepository(database.Read()),
		Completer:  events,
	}
	galaxyService := appgalaxy.Service{Repository: storagesqlite.NewGalaxyRepository(database.Read())}
	reportsService := appreports.Service{
		Clock:      clock,
		Repository: storagesqlite.NewReportsRepository(database.Read(), database.Write()),
		Completer:  events,
	}
	battleSimulation := appbattlesimulation.Service{Reports: reportsService, Economy: economy, Catalogues: catalogues}
	alliance := appalliance.Service{Clock: clock, Repository: storagesqlite.NewAllianceRepository(database.Write())}
	chat := appchat.Service{
		Clock: clock, Repository: storagesqlite.NewChatRepository(database.Read(), database.Write()),
		Typing: &appchat.TypingTracker{},
	}
	operations := appacs.Service{
		Clock:      clock,
		Repository: acsRepository,
		Seeds:      random.NewSeedGenerator(rand.Reader),
		Completer:  events,
	}
	thinking := appai.Thinking{Clock: clock, Thought: aiRepository}
	artificials := appai.Service{
		Clock:      clock,
		Repository: aiRepository,
		Census:     aiRepository,
		Empires:    economy,
		Alliances:  alliance,
		Seeds:      random.NewSeedGenerator(rand.Reader),
		Completer:  events,
	}
	return &world{
		Database:         database,
		Clock:            clock,
		Events:           events,
		Activity:         activityService,
		Economy:          economy,
		Research:         research,
		Shipyard:         shipyard,
		Fleet:            fleetService,
		Galaxy:           galaxyService,
		Reports:          reportsService,
		BattleSimulation: battleSimulation,
		Phalanx:          appphalanx.Service{Clock: clock, Repository: storagesqlite.NewPhalanxRepository(database.Write(), catalogues), Completer: events},
		JumpGate:         appjumpgate.Service{Clock: clock, Repository: storagesqlite.NewJumpGateRepository(database.Write(), catalogues), Completer: events},
		Alliance:         alliance,
		Chat:             chat,
		ACS:              operations,
		AI:               artificials,
		Population:       appai.Populating{Clock: clock, Service: artificials, Census: aiRepository},
		Thinking:         thinking,
		Teamwork:         appai.Teamwork{Shared: aiRepository},
		Brain: &ai.Brain{
			Clock: clock, Thinking: thinking, Economy: economy, Research: research, Shipyard: shipyard,
			Fleet: fleetService, Reports: reportsService, Galaxy: galaxyService,
			Teamwork: appai.Teamwork{Shared: aiRepository}, Diplomacy: alliance, Operations: operations,
			Catalogues: catalogues,
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
	database := freshDatabase(b, ctx, "benchmark.db")
	return database
}

func assertSingleText(t *testing.T, database *storagesqlite.Database, query string, want string, arguments ...any) {
	t.Helper()
	var got string
	if err := database.Read().QueryRow(query, arguments...).Scan(&got); err != nil {
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

// newWorldFor rebuilds the services over an existing database, for tests that
// need a second point of view on the same universe.
func newWorldFor(t *testing.T, database *storagesqlite.Database) *world {
	t.Helper()
	return newWorld(t, database, appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC)))
}

// benchmarkUniverse prepares a running universe with two accounts.
func benchmarkUniverse(b *testing.B, ctx context.Context) *storagesqlite.Database {
	b.Helper()
	database := benchmarkDatabase(b, ctx)
	now := "2042-09-10T11:12:13Z"
	for id := 1; id <= 2; id++ {
		if _, err := database.Write().ExecContext(ctx,
			"INSERT INTO accounts(id, username, username_normalized, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
			id, fmt.Sprintf("player%d", id), fmt.Sprintf("player%d", id), now, now); err != nil {
			b.Fatal(err)
		}
	}
	document, err := rules.Encode(rules.Default())
	if err != nil {
		b.Fatal(err)
	}
	if _, err := database.Write().ExecContext(ctx,
		"INSERT INTO ruleset_versions(version, status, document, checksum, author_account_id, effective_at, created_at) VALUES (1, 'active', ?, 'bench', 1, ?, ?)",
		string(document), now, now); err != nil {
		b.Fatal(err)
	}
	return database
}

func newBenchmarkWorld(b *testing.B, database *storagesqlite.Database, clock *appclock.Fake) *world {
	b.Helper()
	catalogues := catalogue.Default()
	economyRepository := storagesqlite.NewEconomyRepository(database.Write(), catalogues.Buildings)
	fleetRepository := storagesqlite.NewFleetRepository(database.Write(), catalogues)
	events := storagesqlite.NewEventProcessor(database.Write(), clock)
	economyRepository.RegisterHandlers(events)
	fleetRepository.RegisterHandlers(events)
	return &world{
		Database: database,
		Clock:    clock,
		Events:   events,
		Economy:  appeconomy.Service{Clock: clock, Repository: economyRepository, Catalogue: catalogues.Buildings, Completer: events},
		Fleet: appfleet.Service{
			Clock: clock, Repository: fleetRepository, Catalogues: catalogues,
			Seeds: random.NewSeedGenerator(rand.Reader), Completer: events,
		},
	}
}

// insertMoon puts a moon in orbit of an existing planet.
func insertMoon(t *testing.T, ctx context.Context, database *storagesqlite.Database, playerID, parentID int64, galaxy, system, position int) int64 {
	t.Helper()
	result, err := database.Write().ExecContext(ctx, `
		INSERT INTO planets(owner_player_id, kind, parent_planet_id, name, galaxy, system, position,
			total_fields, minimum_temperature, maximum_temperature, created_at)
		VALUES (?, 'moon', ?, 'Lune', ?, ?, ?, 10, 10, 50, '2042-09-10T11:12:13Z')
	`, playerID, parentID, galaxy, system, position)
	if err != nil {
		t.Fatalf("insert moon: %v", err)
	}
	moonID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Write().ExecContext(ctx,
		"INSERT INTO planet_resources(planet_id, metal, crystal, deuterium, produced_at) VALUES (?, 0, 0, 0, '2042-09-10T11:12:13Z')",
		moonID); err != nil {
		t.Fatalf("insert moon resources: %v", err)
	}
	return moonID
}

// launchTransportTowards sends a small transport, which the phalanx tests need
// something to detect.
func launchTransportTowards(t *testing.T, ctx context.Context, universeWorld *world, fromPlanetID int64, target universe.Coordinate) {
	t.Helper()
	if _, err := universeWorld.Fleet.Launch(ctx, appauth.Principal{AccountID: 2}, fromPlanetID, appfleet.LaunchRequest{
		Target: target, TargetKind: domainfleet.TargetPlanet, Mission: domainfleet.MissionTransport,
		Composition: domainfleet.Composition{unit.SmallCargo: 4},
		Cargo:       domaineconomy.Resources{Metal: 100}, Percent: 10,
	}, "phalanx-target"); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
}

// setClock moves the fake clock forward and refuses to hide a rejected move,
// which would otherwise silently run a test at the wrong hour.
func setClock(t *testing.T, clock *appclock.Fake, at time.Time) {
	t.Helper()
	if err := clock.Set(at); err != nil {
		t.Fatalf("set clock to %v: %v", at, err)
	}
}
