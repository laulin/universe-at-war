package tests

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appmissile "universeatwar/internal/app/missile"
	appranking "universeatwar/internal/app/ranking"
	appreports "universeatwar/internal/app/reports"
	appclock "universeatwar/internal/clock"
	"universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/missile"
	"universeatwar/internal/domain/report"
	"universeatwar/internal/domain/unit"
)

func missileWorld(t *testing.T) (*world, appmissile.Request) {
	t.Helper()
	ctx := context.Background()
	db := economyDatabase(t, ctx, 2)
	w := newWorld(t, db, appclock.NewFake(time.Date(2042, 9, 10, 11, 12, 13, 0, time.UTC)))
	for i, name := range []string{"Alice", "Bob"} {
		if _, err := w.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: int64(i + 1)}, name); err != nil {
			t.Fatal(err)
		}
	}
	setBuilding(t, ctx, db, 1, "missile_silo", 4)
	setResearch(t, ctx, db, 1, "impulse_drive", 5)
	setUnits(t, ctx, db, 1, "interplanetary_missile", 10)
	return w, appmissile.Request{Target: coordinateOf(t, 1, 1, 1), TargetKind: fleet.TargetPlanet, Count: 3, Priority: unit.RocketLauncher}
}

func TestMissileStrikePersistsInterceptsAndReportsExactlyOnce(t *testing.T) {
	w, request := missileWorld(t)
	ctx := context.Background()
	alice := appauth.Principal{AccountID: 1}
	setUnits(t, ctx, w.Database, 2, "anti_ballistic_missile", 2)
	setUnits(t, ctx, w.Database, 2, "rocket_launcher", 100)
	setUnits(t, ctx, w.Database, 2, "light_fighter", 100)
	plan, err := w.Missiles.Preview(ctx, alice, 1, request)
	if err != nil || plan.Duration != 30*time.Second {
		t.Fatalf("preview = %+v, %v", plan, err)
	}
	before, err := w.Ranking.List(ctx, alice, appranking.Total)
	if err != nil {
		t.Fatal(err)
	}
	strike, err := w.Missiles.Launch(ctx, alice, 1, request, "first-strike")
	if err != nil {
		t.Fatal(err)
	}
	after, err := w.Ranking.List(ctx, alice, appranking.Total)
	if err != nil {
		t.Fatal(err)
	}
	var beforeScore, afterScore int64
	for _, entry := range before {
		if entry.Own {
			beforeScore = entry.Points
		}
	}
	for _, entry := range after {
		if entry.Own {
			afterScore = entry.Points
		}
	}
	if beforeScore-afterScore != 75 {
		t.Fatalf("missile score loss=%d, want75", beforeScore-afterScore)
	}
	assertSingleValue(t, w.Database, "SELECT quantity FROM planet_units WHERE planet_id=1 AND unit_id='interplanetary_missile'", 7)
	assertSingleValue(t, w.Database, "SELECT COUNT(*) FROM fleets", 0)
	assertSingleValue(t, w.Database, "SELECT COUNT(*) FROM reports", 0)
	if replay, err := w.Missiles.Launch(ctx, alice, 1, request, "first-strike"); err != nil || replay.ID != strike.ID {
		t.Fatalf("replay = %+v, %v", replay, err)
	}
	changed := request
	changed.Count++
	if _, err := w.Missiles.Launch(ctx, alice, 1, changed, "first-strike"); !errors.Is(err, appmissile.ErrInvalid) {
		t.Fatalf("conflicting replay = %v", err)
	}
	activity, err := w.Activity.Snapshot(ctx, appauth.Principal{AccountID: 2})
	if err != nil || len(activity.Incoming) != 1 || !activity.Incoming[0].Missile || activity.Bodies[2].IncomingAttacks != 1 {
		t.Fatalf("warning = %+v, %v", activity, err)
	}
	// A fresh composition reuses only the durable salve and scheduled event.
	restarted := newWorld(t, w.Database, w.Clock)
	w.Clock.Set(strike.ArrivesAt)
	if _, err := restarted.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatal(err)
	}
	assertSingleValue(t, w.Database, "SELECT quantity FROM planet_units WHERE planet_id=2 AND unit_id='anti_ballistic_missile'", 0)
	assertSingleValue(t, w.Database, "SELECT quantity FROM planet_units WHERE planet_id=2 AND unit_id='rocket_launcher'", 40)
	assertSingleValue(t, w.Database, "SELECT quantity FROM planet_units WHERE planet_id=2 AND unit_id='light_fighter'", 100)
	assertSingleValue(t, w.Database, "SELECT COUNT(*) FROM debris_fields", 0)
	assertSingleValue(t, w.Database, "SELECT COUNT(*) FROM reports", 2)
	assertSingleValue(t, w.Database, "SELECT COUNT(*) FROM missile_strikes WHERE state='completed'", 1)
	if _, err := restarted.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatal(err)
	}
	assertSingleValue(t, w.Database, "SELECT COUNT(*) FROM reports", 2)
	for _, recipient := range []int64{1, 2} {
		principal := appauth.Principal{AccountID: recipient}
		summaries, err := w.Reports.List(ctx, principal, appreports.Filter{Page: 1})
		if err != nil || len(summaries) != 1 {
			t.Fatalf("summaries: %+v %v", summaries, err)
		}
		detail, err := w.Reports.Get(ctx, principal, summaries[0].ID)
		if err != nil {
			t.Fatal(err)
		}
		payload, ok := detail.Payload.(report.MissilePayload)
		if !ok || payload.Sent != 3 || payload.Intercepted != 2 || payload.Losses["rocket_launcher"] != 60 {
			t.Fatalf("payload = %+v", detail.Payload)
		}
	}
	if unread, err := w.Reports.UnreadHostile(ctx, appauth.Principal{AccountID: 2}); err != nil || unread != 1 {
		t.Fatalf("hostile count = %d %v", unread, err)
	}
	if err := w.Database.CheckIntegrity(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestMissileStrikeOnMoonUsesParentInterceptors(t *testing.T) {
	w, request := missileWorld(t)
	ctx := context.Background()
	moonID := insertMoon(t, ctx, w.Database, 2, 2, 1, 1, 1)
	setUnits(t, ctx, w.Database, 2, "anti_ballistic_missile", 1)
	setUnits(t, ctx, w.Database, 2, "rocket_launcher", 100)
	setUnits(t, ctx, w.Database, moonID, "rocket_launcher", 100)
	request.TargetKind = fleet.TargetMoon
	request.Count = 2
	strike, err := w.Missiles.Launch(ctx, appauth.Principal{AccountID: 1}, 1, request, "moon-strike")
	if err != nil {
		t.Fatal(err)
	}
	view, err := w.Galaxy.System(ctx, appauth.Principal{AccountID: 1}, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if view.Rows[0].PlanetID != 2 || view.Rows[0].MoonID != moonID {
		t.Fatalf("planet and moon collapsed: %+v", view.Rows[0])
	}
	w.Clock.Set(strike.ArrivesAt)
	if _, err := w.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatal(err)
	}
	assertSingleValue(t, w.Database, "SELECT quantity FROM planet_units WHERE planet_id=2 AND unit_id='anti_ballistic_missile'", 0)
	assertSingleValue(t, w.Database, "SELECT quantity FROM planet_units WHERE planet_id=2 AND unit_id='rocket_launcher'", 100)
	assertSingleValue(t, w.Database, fmt.Sprintf("SELECT quantity FROM planet_units WHERE planet_id=%d AND unit_id='rocket_launcher'", moonID), 40)
}

func TestMissileLaunchRejectsInvalidRequestsWithoutSpending(t *testing.T) {
	w, base := missileWorld(t)
	ctx := context.Background()
	for _, test := range []struct {
		name   string
		change func(*appmissile.Request)
		want   error
	}{
		{"zero", func(r *appmissile.Request) { r.Count = 0 }, appmissile.ErrInvalid},
		{"negative", func(r *appmissile.Request) { r.Count = -1 }, appmissile.ErrInvalid},
		{"stock", func(r *appmissile.Request) { r.Count = 11 }, appmissile.ErrStock},
		{"far", func(r *appmissile.Request) { r.Target.System = 26 }, missile.ErrRange},
		{"galaxy", func(r *appmissile.Request) { r.Target.Galaxy = 2 }, missile.ErrRange},
		{"self", func(r *appmissile.Request) { r.Target.Position = 8 }, appmissile.ErrTarget},
		{"empty", func(r *appmissile.Request) { r.Target.Position = 2 }, appmissile.ErrTarget},
		{"no moon", func(r *appmissile.Request) { r.TargetKind = fleet.TargetMoon }, appmissile.ErrTarget},
		{"ship priority", func(r *appmissile.Request) { r.Priority = unit.LightFighter }, appmissile.ErrInvalid},
		{"missile priority", func(r *appmissile.Request) { r.Priority = unit.InterplanetaryMissile }, appmissile.ErrInvalid},
		{"debris", func(r *appmissile.Request) { r.TargetKind = fleet.TargetDebris }, appmissile.ErrTarget},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := base
			test.change(&request)
			if _, err := w.Missiles.Launch(ctx, appauth.Principal{AccountID: 1}, 1, request, test.name); !errors.Is(err, test.want) {
				t.Fatalf("got %v, want %v", err, test.want)
			}
		})
	}
	if _, err := w.Missiles.Launch(ctx, appauth.Principal{AccountID: 2}, 1, base, "foreign"); err == nil {
		t.Fatal("foreign origin accepted")
	}
	if _, err := w.Missiles.Launch(ctx, appauth.Principal{}, 1, base, "unauthenticated"); !errors.Is(err, appmissile.ErrForbidden) {
		t.Fatalf("unauthenticated: %v", err)
	}
	setBuilding(t, ctx, w.Database, 1, "missile_silo", 3)
	if _, err := w.Missiles.Launch(ctx, appauth.Principal{AccountID: 1}, 1, base, "silo"); !errors.Is(err, appmissile.ErrOrigin) {
		t.Fatalf("silo: %v", err)
	}
	assertSingleValue(t, w.Database, "SELECT quantity FROM planet_units WHERE planet_id=1 AND unit_id='interplanetary_missile'", 10)
	assertSingleValue(t, w.Database, "SELECT COUNT(*) FROM missile_strikes", 0)
}

func TestConcurrentMissileLaunchesCannotOverspend(t *testing.T) {
	w, request := missileWorld(t)
	request.Count = 10
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := w.Missiles.Launch(context.Background(), appauth.Principal{AccountID: 1}, 1, request, fmt.Sprintf("concurrent-%d", i))
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, appmissile.ErrStock) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatalf("successes=%d", successes)
	}
	assertSingleValue(t, w.Database, "SELECT quantity FROM planet_units WHERE planet_id=1 AND unit_id='interplanetary_missile'", 0)
	assertSingleValue(t, w.Database, "SELECT COUNT(*) FROM missile_strikes", 1)
}

func TestMissileStrikeAbortsIfTargetDisappears(t *testing.T) {
	w, request := missileWorld(t)
	ctx := context.Background()
	strike, err := w.Missiles.Launch(ctx, appauth.Principal{AccountID: 1}, 1, request, "gone")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Database.Write().ExecContext(ctx, "DELETE FROM planets WHERE id=2"); err != nil {
		t.Fatal(err)
	}
	w.Clock.Set(strike.ArrivesAt)
	if _, err := w.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatal(err)
	}
	assertSingleValue(t, w.Database, "SELECT COUNT(*) FROM missile_strikes WHERE state='aborted'", 1)
	assertSingleValue(t, w.Database, "SELECT quantity FROM planet_units WHERE planet_id=1 AND unit_id='interplanetary_missile'", 7)
	assertSingleValue(t, w.Database, "SELECT COUNT(*) FROM reports WHERE kind='missile_attack' AND json_extract(payload,'$.aborted')=1", 1)
}

func TestMissileImpactFailureRollsBackAndCanBeRetried(t *testing.T) {
	w, request := missileWorld(t)
	ctx := context.Background()
	setUnits(t, ctx, w.Database, 2, "anti_ballistic_missile", 1)
	setUnits(t, ctx, w.Database, 2, "rocket_launcher", 100)
	strike, err := w.Missiles.Launch(ctx, appauth.Principal{AccountID: 1}, 1, request, "rollback")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Database.Write().ExecContext(ctx, `CREATE TRIGGER reject_missile_report BEFORE INSERT ON reports BEGIN SELECT RAISE(ABORT, 'test report failure'); END`); err != nil {
		t.Fatal(err)
	}
	w.Clock.Set(strike.ArrivesAt)
	w.Events.MaxAttempts = 1
	if _, err := w.Events.CompleteDue(ctx, 1); err != nil {
		t.Fatal(err)
	}
	assertSingleValue(t, w.Database, "SELECT quantity FROM planet_units WHERE planet_id=2 AND unit_id='anti_ballistic_missile'", 1)
	assertSingleValue(t, w.Database, "SELECT quantity FROM planet_units WHERE planet_id=2 AND unit_id='rocket_launcher'", 100)
	assertSingleValue(t, w.Database, "SELECT COUNT(*) FROM reports", 0)
	assertSingleValue(t, w.Database, "SELECT COUNT(*) FROM missile_strikes WHERE state='outbound'", 1)
	if _, err := w.Database.Write().ExecContext(ctx, "DROP TRIGGER reject_missile_report"); err != nil {
		t.Fatal(err)
	}
	w.Events.MaxAttempts = 5
	if _, err := w.Events.CompleteDue(ctx, 1); err != nil {
		t.Fatal(err)
	}
	assertSingleValue(t, w.Database, "SELECT COUNT(*) FROM missile_strikes WHERE state='completed'", 1)
	assertSingleValue(t, w.Database, "SELECT quantity FROM planet_units WHERE planet_id=2 AND unit_id='rocket_launcher'", 0)
	assertSingleValue(t, w.Database, "SELECT COUNT(*) FROM reports", 2)
}

func TestMissileFlightIsFrozenAndDisableDoesNotCancelAnExistingStrike(t *testing.T) {
	w, request := missileWorld(t)
	ctx := context.Background()
	request.Count = 1
	setUnits(t, ctx, w.Database, 2, "rocket_launcher", 200)
	strike, err := w.Missiles.Launch(ctx, appauth.Principal{AccountID: 1}, 1, request, "frozen")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Database.Write().ExecContext(ctx, `UPDATE ruleset_versions SET document=json_set(document, '$.combat.missiles_enabled', json('false'), '$.time.hostile_fleet_speed', 10) WHERE status='active'`); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Missiles.Launch(ctx, appauth.Principal{AccountID: 1}, 1, request, "disabled"); !errors.Is(err, appmissile.ErrDisabled) {
		t.Fatalf("disabled launch=%v", err)
	}
	w.Clock.Advance(3 * time.Second)
	if _, err := w.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatal(err)
	}
	assertSingleValue(t, w.Database, "SELECT COUNT(*) FROM reports", 0)
	// Technology is read at impact, while range and arrival remain fixed.
	setResearch(t, ctx, w.Database, 1, "weapons_technology", 10)
	w.Clock.Set(strike.ArrivesAt)
	if _, err := w.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatal(err)
	}
	assertSingleValue(t, w.Database, "SELECT quantity FROM planet_units WHERE planet_id=2 AND unit_id='rocket_launcher'", 80)
}

func TestInterceptorsBuiltDuringFlightDefendAtImpact(t *testing.T) {
	w, request := missileWorld(t)
	ctx := context.Background()
	if _, err := w.Database.Write().ExecContext(ctx, `UPDATE ruleset_versions SET document=json_set(document, '$.time.defense_speed', 1000) WHERE status='active'`); err != nil {
		t.Fatal(err)
	}
	setBuilding(t, ctx, w.Database, 2, "shipyard", 1)
	setBuilding(t, ctx, w.Database, 2, "missile_silo", 2)
	setResources(t, ctx, w.Database, 2, 8000, 0, 2000)
	setUnits(t, ctx, w.Database, 2, "rocket_launcher", 100)
	request.Count = 1
	strike, err := w.Missiles.Launch(ctx, appauth.Principal{AccountID: 1}, 1, request, "build-interceptor")
	if err != nil {
		t.Fatal(err)
	}
	order, err := w.Shipyard.Order(ctx, appauth.Principal{AccountID: 2}, 2, unit.AntiBallisticMissile, 1, "interceptor")
	if err != nil {
		t.Fatal(err)
	}
	if !order.CompletesAt.Before(strike.ArrivesAt) {
		t.Fatalf("interceptor not ready in time: %+v", order)
	}
	w.Clock.Set(strike.ArrivesAt)
	if _, err := w.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatal(err)
	}
	assertSingleValue(t, w.Database, "SELECT quantity FROM planet_units WHERE planet_id=2 AND unit_id='rocket_launcher'", 100)
	assertSingleValue(t, w.Database, "SELECT quantity FROM planet_units WHERE planet_id=2 AND unit_id='anti_ballistic_missile'", 0)
	assertSingleValue(t, w.Database, "SELECT COUNT(*) FROM reports WHERE kind='missile_attack' AND json_extract(payload,'$.intercepted')=1 AND json_extract(payload,'$.remaining') IS NULL", 1)
}

func TestMissileLaunchFreesSiloSpaceImmediately(t *testing.T) {
	w, request := missileWorld(t)
	ctx := context.Background()
	alice := appauth.Principal{AccountID: 1}
	setUnits(t, ctx, w.Database, 1, "interplanetary_missile", 20)
	setBuilding(t, ctx, w.Database, 1, "shipyard", 1)
	setBuilding(t, ctx, w.Database, 1, "metal_storage", 1)
	setResources(t, ctx, w.Database, 1, 15000, 3000, 10000)
	if _, err := w.Shipyard.Order(ctx, alice, 1, unit.InterplanetaryMissile, 1, "full-silo"); !errors.Is(err, unit.ErrSiloCapacity) {
		t.Fatalf("full silo=%v", err)
	}
	request.Count = 1
	if _, err := w.Missiles.Launch(ctx, alice, 1, request, "free-space"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Shipyard.Order(ctx, alice, 1, unit.InterplanetaryMissile, 1, "new-missile"); err != nil {
		t.Fatal(err)
	}
}

func TestSimultaneousMissileSalvesConsumeInterceptorsInEventOrder(t *testing.T) {
	w, request := missileWorld(t)
	ctx := context.Background()
	request.Count = 1
	setUnits(t, ctx, w.Database, 2, "anti_ballistic_missile", 1)
	setUnits(t, ctx, w.Database, 2, "rocket_launcher", 100)
	first, err := w.Missiles.Launch(ctx, appauth.Principal{AccountID: 1}, 1, request, "simultaneous-first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := w.Missiles.Launch(ctx, appauth.Principal{AccountID: 1}, 1, request, "simultaneous-second")
	if err != nil {
		t.Fatal(err)
	}
	if !first.ArrivesAt.Equal(second.ArrivesAt) {
		t.Fatal("salves do not arrive together")
	}
	w.Clock.Set(first.ArrivesAt)
	if _, err := w.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatal(err)
	}
	assertSingleValue(t, w.Database, "SELECT quantity FROM planet_units WHERE planet_id=2 AND unit_id='rocket_launcher'", 40)
	assertSingleValue(t, w.Database, "SELECT json_extract(payload,'$.intercepted') FROM reports WHERE kind='missile_attack' ORDER BY id LIMIT 1", 1)
	assertSingleValue(t, w.Database, "SELECT json_extract(payload,'$.intercepted') FROM reports WHERE kind='missile_attack' ORDER BY id DESC LIMIT 1", 0)
}

func TestMissileStrikeDoesNotHitAReplacementColonyWithTheSameID(t *testing.T) {
	w, request := missileWorld(t)
	ctx := context.Background()
	strike, err := w.Missiles.Launch(ctx, appauth.Principal{AccountID: 1}, 1, request, "replacement")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Database.Write().ExecContext(ctx, "DELETE FROM planets WHERE id=2"); err != nil {
		t.Fatal(err)
	}
	replacement := insertPlanet(t, ctx, w.Database, 2, "Nouvelle colonie", 1, 1, 1)
	if replacement != 2 {
		t.Fatalf("expected SQLite to reuse id2, got %d", replacement)
	}
	if _, err := w.Database.Write().ExecContext(ctx, "UPDATE planets SET created_at='2042-09-10T11:12:14Z' WHERE id=2"); err != nil {
		t.Fatal(err)
	}
	setUnits(t, ctx, w.Database, 2, "rocket_launcher", 100)
	activity, err := w.Activity.Snapshot(ctx, appauth.Principal{AccountID: 2})
	if err != nil || len(activity.Incoming) != 0 {
		t.Fatalf("old strike announced against replacement: %+v %v", activity, err)
	}
	w.Clock.Set(strike.ArrivesAt)
	if _, err := w.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatal(err)
	}
	assertSingleValue(t, w.Database, "SELECT quantity FROM planet_units WHERE planet_id=2 AND unit_id='rocket_launcher'", 100)
	assertSingleValue(t, w.Database, "SELECT COUNT(*) FROM missile_strikes WHERE state='aborted'", 1)
}
