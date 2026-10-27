package missile

import (
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"universeatwar/internal/domain/random"
	"universeatwar/internal/domain/rules"
	"universeatwar/internal/domain/unit"
	"universeatwar/internal/domain/universe"
)

func TestMissileFlight(t *testing.T) {
	for _, test := range []struct {
		name     string
		from, to universe.Coordinate
		impulse  int
		speed    float64
		circular bool
		want     time.Duration
		err      error
	}{
		{"same system", universe.Coordinate{Galaxy: 1, System: 100, Position: 1}, universe.Coordinate{Galaxy: 1, System: 100, Position: 15}, 1, 1, false, 30 * time.Second, nil},
		{"ten systems", universe.Coordinate{Galaxy: 1, System: 100, Position: 1}, universe.Coordinate{Galaxy: 1, System: 110, Position: 1}, 3, 1, false, 630 * time.Second, nil},
		{"accelerated", universe.Coordinate{Galaxy: 1, System: 100, Position: 1}, universe.Coordinate{Galaxy: 1, System: 110, Position: 1}, 3, 3, false, 210 * time.Second, nil},
		{"range boundary", universe.Coordinate{Galaxy: 1, System: 100, Position: 1}, universe.Coordinate{Galaxy: 1, System: 124, Position: 1}, 5, 1, false, 1470 * time.Second, nil},
		{"outside range", universe.Coordinate{Galaxy: 1, System: 100, Position: 1}, universe.Coordinate{Galaxy: 1, System: 125, Position: 1}, 5, 1, false, 0, ErrRange},
		{"other galaxy", universe.Coordinate{Galaxy: 1, System: 100, Position: 1}, universe.Coordinate{Galaxy: 2, System: 100, Position: 1}, 100, 1, true, 0, ErrRange},
		{"no impulse", universe.Coordinate{Galaxy: 1, System: 100, Position: 1}, universe.Coordinate{Galaxy: 1, System: 100, Position: 2}, 0, 1, false, 0, ErrRange},
		{"wrap", universe.Coordinate{Galaxy: 1, System: 1, Position: 1}, universe.Coordinate{Galaxy: 1, System: 499, Position: 1}, 1, 1, true, 90 * time.Second, nil},
		{"linear boundary", universe.Coordinate{Galaxy: 1, System: 1, Position: 1}, universe.Coordinate{Galaxy: 1, System: 499, Position: 1}, 1, 1, false, 0, ErrRange},
	} {
		t.Run(test.name, func(t *testing.T) {
			topology := rules.Default().Topology
			topology.SystemsPerGalaxy = 499
			topology.CircularSystems = test.circular
			got, err := Flight(test.from, test.to, test.impulse, topology, test.speed)
			if !errors.Is(err, test.err) || got != test.want {
				t.Fatalf("Flight() = %v, %v; want %v, %v", got, err, test.want, test.err)
			}
		})
	}
}

func TestMissilesIgnoreShieldsAndRespectTechnologies(t *testing.T) {
	for _, test := range []struct {
		name            string
		weapons, armour int
		expected        int64
	}{
		{"equal technologies", 0, 0, 60}, {"weapons", 10, 0, 120}, {"armour", 0, 10, 30}, {"equal advanced", 10, 10, 60},
	} {
		t.Run(test.name, func(t *testing.T) {
			inventory := unit.Inventory{unit.RocketLauncher: 200, unit.LightFighter: 100}
			result, err := Resolve(1, 0, test.weapons, test.armour, unit.RocketLauncher, inventory, unit.DefaultCatalogue(), random.NewSeeded(1))
			if err != nil || result.Losses[unit.RocketLauncher] != test.expected {
				t.Fatalf("Resolve = %+v, %v", result, err)
			}
			if inventory[unit.RocketLauncher] != 200 || result.Losses[unit.LightFighter] != 0 {
				t.Fatal("mutated inventory or hit ships")
			}
		})
	}
	result, err := Resolve(1, 0, 0, 0, unit.LargeShieldDome, unit.Inventory{unit.LargeShieldDome: 1}, unit.DefaultCatalogue(), random.NewSeeded(1))
	if err != nil || result.Losses[unit.LargeShieldDome] != 1 {
		t.Fatalf("shield dome survived: %+v, %v", result, err)
	}
}

func TestMissileInterceptionPriorityAndStockpile(t *testing.T) {
	catalogue := unit.DefaultCatalogue()
	result, err := Resolve(3, 2, 0, 0, unit.PlasmaTurret, unit.Inventory{unit.PlasmaTurret: 1, unit.RocketLauncher: 100}, catalogue, random.NewSeeded(2))
	if err != nil || result.Intercepted != 2 || result.Losses[unit.PlasmaTurret] != 1 || result.Losses[unit.RocketLauncher] != 10 {
		t.Fatalf("priority strike = %+v, %v", result, err)
	}
	result, err = Resolve(1, 10, 0, 0, "", unit.Inventory{unit.RocketLauncher: 100}, catalogue, random.NewSeeded(2))
	if err != nil || result.Intercepted != 1 || len(result.Losses) != 0 {
		t.Fatalf("interception = %+v, %v", result, err)
	}
	result, err = Resolve(1, 0, 0, 0, "", unit.Inventory{unit.InterplanetaryMissile: 20}, catalogue, random.NewSeeded(2))
	if err != nil || result.Losses[unit.InterplanetaryMissile] != 8 {
		t.Fatalf("stockpile = %+v, %v", result, err)
	}
	result, err = Resolve(1, 0, 0, 0, "", unit.Inventory{unit.LargeShieldDome: 2, unit.InterplanetaryMissile: 20}, catalogue, random.NewSeeded(2))
	if err != nil || result.Losses[unit.InterplanetaryMissile] != 0 {
		t.Fatalf("stockpile hit before remaining defenses: %+v, %v", result, err)
	}
	_, err = Resolve(math.MaxInt64, 0, 0, 0, "", unit.Inventory{}, catalogue, random.NewSeeded(2))
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("overflow accepted: %v", err)
	}
	_, err = Resolve(1, 0, 0, 0, unit.InterplanetaryMissile, unit.Inventory{}, catalogue, random.NewSeeded(2))
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("stockpile allowed as priority: %v", err)
	}
}

func TestMissileResolutionIsReproducible(t *testing.T) {
	inventory := unit.Inventory{unit.RocketLauncher: 100, unit.LightLaser: 100, unit.GaussCannon: 100, unit.PlasmaTurret: 100}
	first, err := Resolve(5, 0, 5, 5, "", inventory, unit.DefaultCatalogue(), random.NewSeeded(42))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Resolve(5, 0, 5, 5, "", inventory, unit.DefaultCatalogue(), random.NewSeeded(42))
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("nonreproducible: %+v, %+v, %v", first, second, err)
	}
}
