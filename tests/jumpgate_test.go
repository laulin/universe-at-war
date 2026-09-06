package tests

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appjumpgate "universeatwar/internal/app/jumpgate"
	appclock "universeatwar/internal/clock"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/unit"
	storagesqlite "universeatwar/internal/storage/sqlite"
)

func TestJumpGateMovesShipsAndCoolsDownBothEnds(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, clock, first, second := twoMoonsWithGates(t)
	alice := appauth.Principal{AccountID: 1}

	overview, err := universeWorld.JumpGate.Overview(ctx, alice, first)
	if err != nil {
		t.Fatalf("Overview() error = %v", err)
	}
	if !overview.Ready || len(overview.Destinations) != 1 || overview.Destinations[0].MoonID != second {
		t.Fatalf("overview = %+v", overview)
	}

	transfer, err := universeWorld.JumpGate.Jump(ctx, alice, first, second,
		domainfleet.Composition{unit.LightFighter: 40}, "jump-1")
	if err != nil {
		t.Fatalf("Jump() error = %v", err)
	}
	if !transfer.ReadyAt.Equal(clock.Now().Add(time.Hour)) {
		t.Fatalf("cooldown ends at %v", transfer.ReadyAt)
	}
	assertSingleValue(t, database, "SELECT quantity FROM planet_units WHERE planet_id = ? AND unit_id = 'light_fighter'", 60, first)
	assertSingleValue(t, database, "SELECT quantity FROM planet_units WHERE planet_id = ? AND unit_id = 'light_fighter'", 40, second)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM jump_gates", 2)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM game_event_log WHERE event_type = 'jump_gate_used'", 1)

	// Both ends are cooling down, in either direction.
	if _, err := universeWorld.JumpGate.Jump(ctx, alice, first, second,
		domainfleet.Composition{unit.LightFighter: 1}, "jump-2"); !errors.Is(err, appjumpgate.ErrCoolingDown) {
		t.Fatalf("a second jump error = %v, want ErrCoolingDown", err)
	}
	if _, err := universeWorld.JumpGate.Jump(ctx, alice, second, first,
		domainfleet.Composition{unit.LightFighter: 1}, "jump-3"); !errors.Is(err, appjumpgate.ErrCoolingDown) {
		t.Fatalf("the return jump error = %v, want ErrCoolingDown", err)
	}

	clock.Advance(time.Hour)
	if _, err := universeWorld.JumpGate.Jump(ctx, alice, second, first,
		domainfleet.Composition{unit.LightFighter: 40}, "jump-4"); err != nil {
		t.Fatalf("Jump() after the cooldown error = %v", err)
	}
	assertSingleValue(t, database, "SELECT quantity FROM planet_units WHERE planet_id = ? AND unit_id = 'light_fighter'", 100, first)
	assertSingleValue(t, database, "SELECT quantity FROM planet_units WHERE planet_id = ? AND unit_id = 'light_fighter'", 0, second)
}

func TestJumpGateRefusesEveryImpossibleDestination(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, _, first, second := twoMoonsWithGates(t)
	alice := appauth.Principal{AccountID: 1}
	bob := appauth.Principal{AccountID: 2}
	composition := domainfleet.Composition{unit.LightFighter: 1}

	if _, err := universeWorld.JumpGate.Jump(ctx, alice, first, first, composition, "self"); !errors.Is(err, appjumpgate.ErrSameMoon) {
		t.Fatalf("jumping to itself error = %v", err)
	}
	if _, err := universeWorld.JumpGate.Jump(ctx, alice, first, 1, composition, "planet"); !errors.Is(err, appjumpgate.ErrNotAMoon) {
		t.Fatalf("jumping to a planet error = %v", err)
	}
	if _, err := universeWorld.JumpGate.Jump(ctx, bob, first, second, composition, "foreign"); err == nil {
		t.Fatal("a foreign gate was accepted")
	}
	if _, err := universeWorld.JumpGate.Jump(ctx, alice, first, second,
		domainfleet.Composition{unit.LightFighter: 1000}, "too-many"); !errors.Is(err, domainfleet.ErrInsufficientUnits) {
		t.Fatalf("jumping more ships than stationed error = %v", err)
	}
	if _, err := universeWorld.JumpGate.Jump(ctx, alice, first, second,
		domainfleet.Composition{unit.RocketLauncher: 1}, "defense"); !errors.Is(err, domainfleet.ErrImmobileUnit) {
		t.Fatalf("jumping a defense error = %v", err)
	}
	// A moon without a gate is not a destination.
	outpost := insertColony(t, ctx, database, 1, "Avant-poste", 1, 6, 4)
	third := insertMoon(t, ctx, database, 1, outpost, 1, 6, 4)
	if _, err := universeWorld.JumpGate.Jump(ctx, alice, first, third, composition, "no-gate"); !errors.Is(err, appjumpgate.ErrNoGate) {
		t.Fatalf("jumping to a moon without a gate error = %v", err)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM jump_gates", 0)
	assertSingleValue(t, database, "SELECT quantity FROM planet_units WHERE planet_id = ? AND unit_id = 'light_fighter'", 100, first)
}

func TestConcurrentJumpsFireTheGateOnlyOnce(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, _, first, second := twoMoonsWithGates(t)
	alice := appauth.Principal{AccountID: 1}

	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for index := range 2 {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			_, err := universeWorld.JumpGate.Jump(ctx, alice, first, second,
				domainfleet.Composition{unit.LightFighter: 60}, jumpKey(index))
			results <- err
		}(index)
	}
	close(start)
	wait.Wait()
	close(results)

	successes := 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, appjumpgate.ErrCoolingDown), errors.Is(err, domainfleet.ErrInsufficientUnits):
		default:
			t.Fatalf("unexpected concurrent error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful jumps = %d, want 1", successes)
	}
	var here, there int64
	if err := database.Read().QueryRowContext(ctx,
		"SELECT quantity FROM planet_units WHERE planet_id = ? AND unit_id = 'light_fighter'", first).Scan(&here); err != nil {
		t.Fatal(err)
	}
	if err := database.Read().QueryRowContext(ctx,
		"SELECT COALESCE(SUM(quantity), 0) FROM planet_units WHERE planet_id = ? AND unit_id = 'light_fighter'", second).Scan(&there); err != nil {
		t.Fatal(err)
	}
	if here+there != 100 {
		t.Fatalf("the jump created or destroyed ships: %d here and %d there", here, there)
	}
}

func jumpKey(index int) string {
	if index == 0 {
		return "concurrent-first"
	}
	return "concurrent-second"
}

// twoMoonsWithGates gives Alice two moons, each with a jump gate, and a
// hundred fighters on the first one.
func twoMoonsWithGates(t *testing.T) (*storagesqlite.Database, *world, *appclock.Fake, int64, int64) {
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
	colony := insertColony(t, ctx, database, 1, "Colonie", 1, 5, 4)
	first := insertMoon(t, ctx, database, 1, 1, 1, 1, 8)
	second := insertMoon(t, ctx, database, 1, colony, 1, 5, 4)
	for _, moon := range []int64{first, second} {
		setBuilding(t, ctx, database, moon, "lunar_base", 1)
		setBuilding(t, ctx, database, moon, "jump_gate", 1)
	}
	setUnits(t, ctx, database, first, "light_fighter", 100)
	setUnits(t, ctx, database, first, "rocket_launcher", 5)
	return database, universeWorld, clock, first, second
}
