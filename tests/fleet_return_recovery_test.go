package tests

import (
	"context"
	"fmt"
	"testing"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appfleet "universeatwar/internal/app/fleet"
	appclock "universeatwar/internal/clock"
	domaineconomy "universeatwar/internal/domain/economy"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/unit"
)

// This is the persisted failure reported by KaLiMH3R0: recycling succeeded,
// but the return exhausted its attempts after the origin was settled further.
func TestLateSettlementMigrationRecoversAnExhaustedRecyclingReturnExactlyOnce(t *testing.T) {
	ctx := context.Background()
	db := economyDatabase(t, ctx, 1)
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC))
	w := newWorld(t, db, clock)
	owner := appauth.Principal{AccountID: 1}
	home, err := w.Economy.CreateEmpire(ctx, owner, "Alice")
	if err != nil {
		t.Fatal(err)
	}
	setUnits(t, ctx, db, home.ID, "recycler", 2)
	setResources(t, ctx, db, home.ID, 0, 0, 5000)
	if _, err := db.Write().ExecContext(ctx, `
		INSERT INTO debris_fields(galaxy, system, position, metal, crystal, created_at, updated_at)
		VALUES (1, 1, 5, 3000, 1000, ?, ?)
	`, clock.Now().Format(time.RFC3339Nano), clock.Now().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	flight, err := w.Fleet.Launch(ctx, owner, home.ID, appfleet.LaunchRequest{
		Target: coordinateOf(t, 1, 1, 5), TargetKind: domainfleet.TargetDebris, Mission: domainfleet.MissionRecycle,
		Composition: domainfleet.Composition{unit.Recycler: 2}, Percent: 100,
	}, "recycle")
	if err != nil {
		t.Fatal(err)
	}
	setClock(t, clock, flight.ArrivesAt)
	if _, err := w.Events.CompleteDue(ctx, 10); err != nil {
		t.Fatal(err)
	}
	assertSingleText(t, db, "SELECT state FROM fleets WHERE id = ?", "returning", flight.ID)
	assertSingleValue(t, db, "SELECT metal FROM fleet_cargo WHERE fleet_id = ?", 3000, flight.ID)
	assertSingleValue(t, db, "SELECT crystal FROM fleet_cargo WHERE fleet_id = ?", 1000, flight.ID)

	setClock(t, clock, flight.ReturnsAt.Add(2*time.Second))
	// Bypass the service completer to reproduce a visit winning the race.
	before, err := w.Economy.Repository.Planet(ctx, owner.AccountID, home.ID, clock.Now(), w.Economy.Catalogue)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Write().ExecContext(ctx, `
		UPDATE scheduled_events SET attempts = 5, last_error = 'economy: production time cannot move backwards'
		WHERE idempotency_key = ?
	`, fmt.Sprintf("fleet-return:%d", flight.ID)); err != nil {
		t.Fatal(err)
	}
	if completed, err := w.Events.CompleteDue(ctx, 10); err != nil || completed != 0 {
		t.Fatalf("exhausted return processed = %d, error = %v", completed, err)
	}
	stuck, err := w.Fleet.Overview(ctx, owner, home.ID)
	if err != nil || stuck.Used != 1 || stuck.Stationed[unit.Recycler] != 0 {
		t.Fatalf("stuck overview = %+v, error = %v", stuck, err)
	}

	// Migration 24 changes only data. Forget its record to exercise the normal
	// startup migration over the stranded rows, without undoing any schema.
	if _, err := db.Write().ExecContext(ctx, "DELETE FROM schema_migrations WHERE version = 24"); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	assertSingleValue(t, db, "SELECT attempts FROM scheduled_events WHERE event_type = 'fleet_returned'", 0)
	assertSingleValue(t, db, "SELECT COUNT(*) FROM scheduled_events WHERE last_error IS NOT NULL", 0)
	if completed, err := w.Events.CompleteDue(ctx, 10); err != nil || completed != 1 {
		t.Fatalf("recovered return processed = %d, error = %v", completed, err)
	}
	assertSingleText(t, db, "SELECT state FROM fleets WHERE id = ?", "completed", flight.ID)
	assertSingleText(t, db, "SELECT state FROM scheduled_events WHERE event_type = 'fleet_returned'", "completed")
	assertSingleValue(t, db, "SELECT metal FROM planet_resources WHERE planet_id = ?", int(before.Stock.Metal+3000), home.ID)
	assertSingleValue(t, db, "SELECT crystal FROM planet_resources WHERE planet_id = ?", int(before.Stock.Crystal+1000), home.ID)
	assertSingleValue(t, db, "SELECT metal + crystal + deuterium FROM fleet_cargo WHERE fleet_id = ?", 0, flight.ID)
	assertSingleValue(t, db, "SELECT COUNT(*) FROM fleet_ships WHERE fleet_id = ?", 0, flight.ID)

	// Neither a repeated startup nor redelivery may credit the return twice.
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	replayEvent(t, ctx, db, "fleet_returned")
	if _, err := w.Events.CompleteDue(ctx, 10); err != nil {
		t.Fatal(err)
	}
	landed, err := w.Fleet.Overview(ctx, owner, home.ID)
	if err != nil || landed.Used != 0 || len(landed.Fleets) != 0 || landed.Stationed[unit.Recycler] != 2 {
		t.Fatalf("landed overview = %+v, error = %v", landed, err)
	}
	assertSingleValue(t, db, "SELECT metal FROM planet_resources WHERE planet_id = ?", int(before.Stock.Metal+3000), home.ID)
	assertSingleValue(t, db, "SELECT crystal FROM planet_resources WHERE planet_id = ?", int(before.Stock.Crystal+1000), home.ID)
	assertSingleValue(t, db, "SELECT COUNT(*) FROM game_event_log WHERE event_type = 'fleet_returned'", 1)
}

func TestLateFleetReturnsLandOnTheirOriginBody(t *testing.T) {
	for _, moon := range []bool{false, true} {
		for _, recall := range []bool{false, true} {
			t.Run(fmt.Sprintf("moon=%t/recall=%t", moon, recall), func(t *testing.T) {
				ctx := context.Background()
				db := economyDatabase(t, ctx, 1)
				clock := appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC))
				w := newWorld(t, db, clock)
				owner := appauth.Principal{AccountID: 1}
				home, err := w.Economy.CreateEmpire(ctx, owner, "Alice")
				if err != nil {
					t.Fatal(err)
				}
				originID := home.ID
				if moon {
					originID = insertMoon(t, ctx, db, 1, home.ID, home.Coordinate.Galaxy, home.Coordinate.System, home.Coordinate.Position)
				}
				colonyID := insertColony(t, ctx, db, 1, "Colonie", 1, 2, 4)
				setUnits(t, ctx, db, originID, "small_cargo", 2)
				setResources(t, ctx, db, originID, 1000, 500, 5000)
				flight, err := w.Fleet.Launch(ctx, owner, originID, appfleet.LaunchRequest{
					Target: coordinateOf(t, 1, 2, 4), TargetKind: domainfleet.TargetPlanet, Mission: domainfleet.MissionTransport,
					Composition: domainfleet.Composition{unit.SmallCargo: 2}, Cargo: domaineconomy.Resources{Metal: 100}, Percent: 100,
				}, "transport")
				if err != nil {
					t.Fatal(err)
				}
				if recall {
					setClock(t, clock, flight.DepartedAt.Add(time.Second))
					flight, err = w.Fleet.Recall(ctx, owner, flight.ID, "recall")
					if err != nil {
						t.Fatal(err)
					}
				} else {
					setClock(t, clock, flight.ArrivesAt)
					if _, err := w.Events.CompleteDue(ctx, 10); err != nil {
						t.Fatal(err)
					}
				}
				setClock(t, clock, flight.ReturnsAt.Add(2*time.Second))
				before, err := w.Economy.Repository.Planet(ctx, owner.AccountID, originID, clock.Now(), w.Economy.Catalogue)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := w.Events.CompleteDue(ctx, 10); err != nil {
					t.Fatal(err)
				}
				assertSingleText(t, db, "SELECT state FROM fleets WHERE id = ?", "completed", flight.ID)
				assertSingleValue(t, db, "SELECT quantity FROM planet_units WHERE planet_id = ? AND unit_id = 'small_cargo'", 2, originID)
				assertSingleValue(t, db, "SELECT COUNT(*) FROM scheduled_events WHERE last_error IS NOT NULL", 0)
				if recall {
					assertSingleValue(t, db, "SELECT metal FROM planet_resources WHERE planet_id = ?", int(before.Stock.Metal+100), originID)
				}
				assertSingleValue(t, db, "SELECT COUNT(*) FROM planet_units WHERE planet_id = ? AND unit_id = 'small_cargo' AND quantity > 0", 0, colonyID)
				if moon {
					assertSingleValue(t, db, "SELECT COUNT(*) FROM planet_units WHERE planet_id = ? AND unit_id = 'small_cargo' AND quantity > 0", 0, home.ID)
				}
			})
		}
	}
}
