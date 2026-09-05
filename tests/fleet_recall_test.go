package tests

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appfleet "universeatwar/internal/app/fleet"
	appclock "universeatwar/internal/clock"
	domaineconomy "universeatwar/internal/domain/economy"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/unit"
	storagesqlite "universeatwar/internal/storage/sqlite"
)

func TestRecallBeforeArrivalBringsEverythingBack(t *testing.T) {
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC))
	database, universe, alice, launched := launchedTransport(t, ctx, clock)

	clock.Advance(100 * time.Second)
	recalled, err := universe.Fleet.Recall(ctx, alice, launched.ID, "recall-1")
	if err != nil {
		t.Fatalf("Recall() error = %v", err)
	}
	if recalled.State != domainfleet.Recalled || recalled.ReturnsAt == nil {
		t.Fatalf("recalled fleet = %+v", recalled)
	}
	if want := clock.Now().Add(100 * time.Second); !recalled.ReturnsAt.Equal(want) {
		t.Fatalf("return time = %v, want %v", recalled.ReturnsAt, want)
	}
	assertSingleText(t, database, "SELECT state FROM scheduled_events WHERE event_type = 'fleet_arrived'", "cancelled")
	assertSingleValue(t, database, "SELECT COUNT(*) FROM scheduled_events WHERE event_type = 'fleet_returned' AND state = 'pending'", 1)

	replay, err := universe.Fleet.Recall(ctx, alice, launched.ID, "recall-1")
	if err != nil || replay.ID != recalled.ID {
		t.Fatalf("idempotent recall = %+v %v", replay, err)
	}

	clock.Set(*recalled.ReturnsAt)
	if _, err := universe.Events.CompleteDue(ctx, 10); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}
	assertSingleValue(t, database, "SELECT quantity FROM planet_units WHERE planet_id = 1 AND unit_id = 'small_cargo'", 2)
	assertSingleText(t, database, "SELECT state FROM fleets WHERE id = 1", "completed")
	assertSingleValue(t, database, "SELECT COUNT(*) FROM planet_resources WHERE planet_id = 2 AND metal > 600", 0)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM game_event_log WHERE event_type = 'fleet_recalled'", 1)
}

func TestRecallAfterArrivalIsRefused(t *testing.T) {
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC))
	database, universe, alice, launched := launchedTransport(t, ctx, clock)

	clock.Set(launched.ArrivesAt)
	if _, err := universe.Events.CompleteDue(ctx, 10); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}
	if _, err := universe.Fleet.Recall(ctx, alice, launched.ID, "late"); !errors.Is(err, appfleet.ErrNotRecallable) {
		t.Fatalf("Recall() after arrival error = %v, want ErrNotRecallable", err)
	}
	assertSingleText(t, database, "SELECT state FROM fleets WHERE id = 1", "returning")
	assertSingleValue(t, database, "SELECT COUNT(*) FROM fleet_transitions WHERE to_state = 'recalled'", 0)
}

func TestRecallOfAnotherAccountIsNotFound(t *testing.T) {
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC))
	database, universe, _, launched := launchedTransport(t, ctx, clock)

	if _, err := universe.Fleet.Recall(ctx, appauth.Principal{AccountID: 2}, launched.ID, "steal"); !errors.Is(err, appfleet.ErrNotFound) {
		t.Fatalf("Recall() by another account error = %v, want ErrNotFound", err)
	}
	assertSingleText(t, database, "SELECT state FROM fleets WHERE id = 1", "outbound")
}

func TestRecallRacingWithArrivalLeavesOneTransition(t *testing.T) {
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC))
	database, universe, alice, launched := launchedTransport(t, ctx, clock)

	clock.Set(launched.ArrivesAt)
	start := make(chan struct{})
	var wait sync.WaitGroup
	var recallErr error
	wait.Add(2)
	go func() {
		defer wait.Done()
		<-start
		_, recallErr = universe.Fleet.Recall(ctx, alice, launched.ID, "race")
	}()
	go func() {
		defer wait.Done()
		<-start
		if _, err := universe.Events.CompleteDue(ctx, 10); err != nil {
			t.Errorf("CompleteDue() error = %v", err)
		}
	}()
	close(start)
	wait.Wait()

	if recallErr != nil && !errors.Is(recallErr, appfleet.ErrNotRecallable) {
		t.Fatalf("Recall() during arrival error = %v", recallErr)
	}
	// Exactly one transition left the outbound state, whoever won the race.
	assertSingleValue(t, database, "SELECT COUNT(*) FROM fleet_transitions WHERE from_state = 'outbound'", 1)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM scheduled_events WHERE event_type = 'fleet_returned'", 1)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM fleets WHERE state = 'outbound'", 0)
}

// launchedTransport prepares one player with a transport already under way.
func launchedTransport(t *testing.T, ctx context.Context, clock *appclock.Fake) (*storagesqlite.Database, *world, appauth.Principal, appfleet.Fleet) {
	t.Helper()
	database := economyDatabase(t, ctx, 2)
	universe := newWorld(t, database, clock)
	alice := appauth.Principal{AccountID: 1}

	home, err := universe.Economy.CreateEmpire(ctx, alice, "Alice")
	if err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	target, err := universe.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: 2}, "Bob")
	if err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	setUnits(t, ctx, database, home.ID, "small_cargo", 2)
	setResources(t, ctx, database, home.ID, 1000, 500, 200)

	launched, err := universe.Fleet.Launch(ctx, alice, home.ID, appfleet.LaunchRequest{
		Target: target.Coordinate, TargetKind: domainfleet.TargetPlanet, Mission: domainfleet.MissionTransport,
		Composition: domainfleet.Composition{unit.SmallCargo: 2},
		Cargo:       domaineconomy.Resources{Metal: 100}, Percent: 10,
	}, "launch")
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	return database, universe, alice, launched
}
