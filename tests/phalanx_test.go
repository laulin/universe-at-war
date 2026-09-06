package tests

import (
	"context"
	"errors"
	"testing"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appfleet "universeatwar/internal/app/fleet"
	appclock "universeatwar/internal/clock"
	domaineconomy "universeatwar/internal/domain/economy"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/phalanx"
	"universeatwar/internal/domain/unit"
	"universeatwar/internal/domain/universe"
	storagesqlite "universeatwar/internal/storage/sqlite"
)

func TestPhalanxTimesFleetsWithoutRevealingThem(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, clock, moonID := phalanxUniverse(t)
	bob := appauth.Principal{AccountID: 2}
	alice := appauth.Principal{AccountID: 1}

	// Alice sends a transport and a spy flight at Bob's planet.
	target := coordinateOf(t, 1, 1, 1)
	transport, err := universeWorld.Fleet.Launch(ctx, alice, 1, appfleet.LaunchRequest{
		Target: target, TargetKind: domainfleet.TargetPlanet, Mission: domainfleet.MissionTransport,
		Composition: domainfleet.Composition{unit.SmallCargo: 3}, Cargo: domaineconomy.Resources{Metal: 100}, Percent: 10,
	}, "transport")
	if err != nil {
		t.Fatalf("Launch(transport) error = %v", err)
	}
	if _, err := universeWorld.Fleet.Launch(ctx, alice, 1, appfleet.LaunchRequest{
		Target: target, TargetKind: domainfleet.TargetPlanet, Mission: domainfleet.MissionEspionage,
		Composition: domainfleet.Composition{unit.EspionageProbe: 2}, Percent: 10,
	}, "spy"); err != nil {
		t.Fatalf("Launch(espionage) error = %v", err)
	}

	scan, err := universeWorld.Phalanx.Scan(ctx, bob, moonID, target)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(scan.Sightings) != 1 {
		t.Fatalf("the sweep saw %d missions, want only the transport", len(scan.Sightings))
	}
	sighting := scan.Sightings[0]
	if sighting.Mission != domainfleet.MissionTransport {
		t.Fatalf("the sweep saw a %q mission", sighting.Mission)
	}
	if sighting.Ships != 3 || !sighting.ArrivesAt.Equal(transport.ArrivesAt) {
		t.Fatalf("sighting = %+v", sighting)
	}
	if sighting.ReturnsAt == nil || !sighting.ReturnsAt.Equal(*transport.ReturnsAt) {
		t.Fatalf("the sweep did not time the return: %+v", sighting)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM game_event_log WHERE event_type = 'phalanx_scanned'", 1)
	// The sweep is paid for, whatever it finds.
	assertSingleValue(t, database, "SELECT deuterium FROM planet_resources WHERE planet_id = ?", 5000, moonID)
	_ = clock
}

func TestPhalanxRefusesWhatItCannotReach(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, _, moonID := phalanxUniverse(t)
	bob := appauth.Principal{AccountID: 2}

	if _, err := universeWorld.Phalanx.Scan(ctx, bob, moonID, universe.Coordinate{Galaxy: 1, System: 40, Position: 1}); !errors.Is(err, phalanx.ErrOutOfRange) {
		t.Fatalf("scanning a distant system error = %v, want ErrOutOfRange", err)
	}
	if _, err := universeWorld.Phalanx.Scan(ctx, bob, moonID, universe.Coordinate{Galaxy: 2, System: 1, Position: 1}); !errors.Is(err, phalanx.ErrOtherGalaxy) {
		t.Fatalf("scanning another galaxy error = %v, want ErrOtherGalaxy", err)
	}
	// Scanning a planet is not scanning from one.
	if _, err := universeWorld.Phalanx.Scan(ctx, bob, 2, coordinateOf(t, 1, 1, 1)); err == nil {
		t.Fatal("a planet was accepted as a phalanx")
	}
	// Somebody else's moon is out of reach.
	if _, err := universeWorld.Phalanx.Scan(ctx, appauth.Principal{AccountID: 1}, moonID, coordinateOf(t, 1, 1, 1)); err == nil {
		t.Fatal("a foreign moon was accepted")
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM game_event_log WHERE event_type = 'phalanx_scanned'", 0)
	assertSingleValue(t, database, "SELECT deuterium FROM planet_resources WHERE planet_id = ?", 10000, moonID)
}

func TestPhalanxRefusesToScanWithoutDeuterium(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, _, moonID := phalanxUniverse(t)
	setResources(t, ctx, database, moonID, 0, 0, 100)

	if _, err := universeWorld.Phalanx.Scan(ctx, appauth.Principal{AccountID: 2}, moonID, coordinateOf(t, 1, 1, 1)); !errors.Is(err, domaineconomy.ErrInsufficientResources) {
		t.Fatalf("scanning without deuterium error = %v", err)
	}
	assertSingleValue(t, database, "SELECT deuterium FROM planet_resources WHERE planet_id = ?", 100, moonID)
}

// phalanxUniverse gives Bob a moon with a level two phalanx, in range of his own
// system and of a few neighbours.
func phalanxUniverse(t *testing.T) (*storagesqlite.Database, *world, *appclock.Fake, int64) {
	t.Helper()
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC))
	database := economyDatabase(t, ctx, 2)
	universeWorld := newWorld(t, database, clock)
	if _, err := universeWorld.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: 1}, "Alice"); err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	if _, err := universeWorld.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: 2}, "Bob"); err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	moonID := insertMoon(t, ctx, database, 2, 2, 1, 1, 1)
	setBuilding(t, ctx, database, moonID, "lunar_base", 1)
	setBuilding(t, ctx, database, moonID, "sensor_phalanx", 2)
	setResources(t, ctx, database, moonID, 0, 0, 10000)
	setUnits(t, ctx, database, 1, "small_cargo", 3)
	setUnits(t, ctx, database, 1, "espionage_probe", 2)
	setResearch(t, ctx, database, 1, "computer_technology", 3)
	setResources(t, ctx, database, 1, 5000, 5000, 5000)
	return database, universeWorld, clock, moonID
}
