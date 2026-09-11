package combat

import (
	"errors"
	"reflect"
	"testing"

	"universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/random"
	"universeatwar/internal/domain/rules"
	"universeatwar/internal/domain/unit"
)

func plainInput(attackers, defenders map[unit.ID]int64) Input {
	configured := rules.Default()
	return Input{
		Attackers:   []Party{{PlayerID: 1, Units: attackers, Technologies: Factors{Weapons: 1, Shield: 1, Armour: 1}}},
		Defenders:   []Party{{PlayerID: 2, Units: defenders, Technologies: Factors{Weapons: 1, Shield: 1, Armour: 1}}},
		Rules:       configured.Combat,
		Multipliers: CostMultipliers{Ship: 1, Defense: 1},
		Catalogue:   unit.DefaultCatalogue(),
	}
}

func TestOneFighterAgainstOneRocketLauncher(t *testing.T) {
	input := plainInput(
		map[unit.ID]int64{unit.LightFighter: 1},
		map[unit.ID]int64{unit.RocketLauncher: 1},
	)
	// Round two damages the fighter below seventy percent, round three damages
	// both. Attackers draw first, then defenders, then the rebuild.
	source := random.NewScript(nil, []float64{.10, .40, .60, .99})

	result, err := Resolve(input, source)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if result.Outcome != AttackerWins {
		t.Fatalf("outcome = %q, want the attacker to win", result.Outcome)
	}
	if len(result.Rounds) != 3 {
		t.Fatalf("rounds = %d, want 3", len(result.Rounds))
	}
	if result.Rounds[0].Attackers.Damage != 50 || result.Rounds[0].Attackers.Absorbed != 20 {
		t.Fatalf("round one attacker stats = %+v", result.Rounds[0].Attackers)
	}
	if result.Rounds[0].Defenders.Damage != 80 || result.Rounds[0].Defenders.Absorbed != 10 {
		t.Fatalf("round one defender stats = %+v", result.Rounds[0].Defenders)
	}
	if result.Attackers[0].Survivors[unit.LightFighter] != 1 {
		t.Fatalf("the fighter did not survive: %+v", result.Attackers[0])
	}
	if result.Defenders[0].Losses[unit.RocketLauncher] != 1 {
		t.Fatalf("the launcher was not destroyed: %+v", result.Defenders[0])
	}
	if result.Debris != (economy.Resources{}) {
		t.Fatalf("debris = %+v, want none while defenses leave no wreckage", result.Debris)
	}
	if result.ShipDebris != (economy.Resources{}) || result.DefenseDebris != (economy.Resources{}) {
		t.Fatalf("debris breakdown = ships %+v, defenses %+v", result.ShipDebris, result.DefenseDebris)
	}
	if result.Defenders[0].Rebuilt[unit.RocketLauncher] != 0 {
		t.Fatalf("the launcher was rebuilt despite a failed draw: %+v", result.Defenders[0])
	}
	if result.MoonChance != 0 {
		t.Fatalf("moon chance = %v, want none", result.MoonChance)
	}
}

func TestTheSameDuelLostByTheAttackerLeavesShipDebris(t *testing.T) {
	input := plainInput(
		map[unit.ID]int64{unit.LightFighter: 1},
		map[unit.ID]int64{unit.RocketLauncher: 1},
	)
	// The fighter draws first in round three and loses its check.
	source := random.NewScript(nil, []float64{.10, .60, .40})

	result, err := Resolve(input, source)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if result.Outcome != DefenderWins {
		t.Fatalf("outcome = %q, want the defender to win", result.Outcome)
	}
	if result.Attackers[0].Losses[unit.LightFighter] != 1 {
		t.Fatalf("the fighter survived: %+v", result.Attackers[0])
	}
	if result.Debris != (economy.Resources{Metal: 900, Crystal: 300}) {
		t.Fatalf("debris = %+v, want 900 metal and 300 crystal", result.Debris)
	}
	if result.ShipDebris != result.Debris || result.DefenseDebris != (economy.Resources{}) {
		t.Fatalf("debris breakdown = ships %+v, defenses %+v", result.ShipDebris, result.DefenseDebris)
	}
}

func TestDefenseDebrisIsSeparatedFromShipDebris(t *testing.T) {
	input := plainInput(
		map[unit.ID]int64{unit.LightFighter: 1},
		map[unit.ID]int64{unit.RocketLauncher: 1},
	)
	input.Rules.DefensesToDebris = .3
	input.Rules.DefenseRebuildChance = 0
	result, err := Resolve(input, random.NewScript(nil, []float64{.10, .40, .60}))
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if result.ShipDebris != (economy.Resources{}) {
		t.Fatalf("ship debris = %+v, want none", result.ShipDebris)
	}
	want := economy.Resources{Metal: 600}
	if result.DefenseDebris != want || result.Debris != want {
		t.Fatalf("debris = total %+v, ships %+v, defenses %+v, want defense %+v",
			result.Debris, result.ShipDebris, result.DefenseDebris, want)
	}
}

func TestABattleIsReproducibleFromItsSeed(t *testing.T) {
	input := plainInput(
		map[unit.ID]int64{unit.Cruiser: 20, unit.LightFighter: 60},
		map[unit.ID]int64{unit.RocketLauncher: 40, unit.HeavyLaser: 10, unit.LightFighter: 15},
	)
	first, err := Resolve(input, random.NewSeeded(42))
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	second, err := Resolve(input, random.NewSeeded(42))
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("the same seed produced two different battles")
	}
	other, err := Resolve(input, random.NewSeeded(43))
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if reflect.DeepEqual(first, other) {
		t.Fatal("two different seeds produced the very same battle")
	}
}

func TestUndefendedPlanetFallsWithoutARound(t *testing.T) {
	input := plainInput(map[unit.ID]int64{unit.LightFighter: 1}, map[unit.ID]int64{})
	result, err := Resolve(input, random.NewScript(nil, nil))
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if result.Outcome != AttackerWins || len(result.Rounds) != 0 {
		t.Fatalf("result = %+v", result)
	}
}

func TestResolveRefusesImpossibleBattles(t *testing.T) {
	tests := []struct {
		name  string
		input func() Input
		want  error
	}{
		{name: "no attacker", input: func() Input {
			return plainInput(map[unit.ID]int64{}, map[unit.ID]int64{unit.RocketLauncher: 1})
		}, want: ErrNoAttacker},
		{name: "defense on the attacking side", input: func() Input {
			return plainInput(map[unit.ID]int64{unit.RocketLauncher: 1}, map[unit.ID]int64{})
		}, want: ErrForbiddenUnit},
		{name: "missiles never fight", input: func() Input {
			return plainInput(map[unit.ID]int64{unit.LightFighter: 1}, map[unit.ID]int64{unit.InterplanetaryMissile: 1})
		}, want: ErrForbiddenUnit},
		{name: "unknown unit", input: func() Input {
			return plainInput(map[unit.ID]int64{"star_destroyer": 1}, map[unit.ID]int64{})
		}, want: ErrInvalidUnits},
		{name: "negative quantity", input: func() Input {
			return plainInput(map[unit.ID]int64{unit.LightFighter: -1}, map[unit.ID]int64{})
		}, want: ErrInvalidUnits},
		{name: "no round", input: func() Input {
			input := plainInput(map[unit.ID]int64{unit.LightFighter: 1}, map[unit.ID]int64{})
			input.Rules.MaximumRounds = 0
			return input
		}, want: ErrInvalidSettings},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Resolve(test.input(), random.NewSeeded(1)); !errors.Is(err, test.want) {
				t.Fatalf("Resolve() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestShieldsBounceShotsTheyOutclass(t *testing.T) {
	input := plainInput(
		map[unit.ID]int64{unit.LightFighter: 1},
		map[unit.ID]int64{unit.LargeShieldDome: 1},
	)
	input.Rules.MaximumRounds = 1
	result, err := Resolve(input, random.NewSeeded(7))
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if result.Rounds[0].Attackers.Damage != 0 {
		t.Fatalf("a fifty damage shot hurt a ten thousand shield: %+v", result.Rounds[0].Attackers)
	}
}

func TestRebuiltDefensesCountAsSurvivors(t *testing.T) {
	input := plainInput(
		map[unit.ID]int64{unit.Battleship: 30},
		map[unit.ID]int64{unit.RocketLauncher: 10},
	)
	input.Rules.DefenseRebuildChance = 1
	result, err := Resolve(input, random.NewSeeded(3))
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	defender := result.Defenders[0]
	if defender.Rebuilt[unit.RocketLauncher] == 0 {
		t.Fatalf("nothing was rebuilt with a certain rebuild: %+v", defender)
	}
	if defender.Survivors[unit.RocketLauncher] != defender.Initial[unit.RocketLauncher] {
		t.Fatalf("a certain rebuild lost launchers: %+v", defender)
	}
	if len(defender.Losses) != 0 {
		t.Fatalf("rebuilt launchers are still counted as losses: %+v", defender)
	}
	if result.Debris.Metal != 0 && input.Rules.DefensesToDebris == 0 {
		t.Fatalf("rebuilt defenses produced debris: %+v", result.Debris)
	}
}

func TestPillageSpreadsOverTheHold(t *testing.T) {
	tests := []struct {
		name     string
		stock    economy.Resources
		capacity int64
		ratio    float64
		want     economy.Resources
	}{
		{name: "empty hold", stock: economy.Resources{Metal: 1000}, capacity: 0, ratio: .5},
		{name: "empty planet", capacity: 1000, ratio: .5},
		{name: "hold larger than the loot", stock: economy.Resources{Metal: 1000, Crystal: 100, Deuterium: 10}, capacity: 100000, ratio: .5,
			want: economy.Resources{Metal: 500, Crystal: 50, Deuterium: 5}},
		{name: "hold smaller than the loot", stock: economy.Resources{Metal: 10000, Crystal: 5000, Deuterium: 1000}, capacity: 6000, ratio: .5,
			want: economy.Resources{Metal: 3000, Crystal: 2500, Deuterium: 500}},
		{name: "only metal to take", stock: economy.Resources{Metal: 10000}, capacity: 1000, ratio: .5,
			want: economy.Resources{Metal: 1000}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Pillage(test.stock, test.capacity, test.ratio)
			if got != test.want {
				t.Fatalf("Pillage() = %+v, want %+v", got, test.want)
			}
			total := got.Metal + got.Crystal + got.Deuterium
			if total > test.capacity {
				t.Fatalf("the loot %d exceeds the hold %d", total, test.capacity)
			}
		})
	}
}

func FuzzResolveKeepsItsInvariants(f *testing.F) {
	f.Add(uint64(1), int64(10), int64(5), int64(20))
	f.Fuzz(func(t *testing.T, seed uint64, cruisers, fighters, launchers int64) {
		bound := func(value int64) int64 {
			if value < 0 {
				value = -value
			}
			return value % 200
		}
		attackers := map[unit.ID]int64{unit.Cruiser: bound(cruisers), unit.LightFighter: bound(fighters)}
		defenders := map[unit.ID]int64{unit.RocketLauncher: bound(launchers)}
		input := plainInput(attackers, defenders)
		result, err := Resolve(input, random.NewSeeded(seed))
		if errors.Is(err, ErrNoAttacker) {
			return
		}
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
		if len(result.Rounds) > input.Rules.MaximumRounds {
			t.Fatalf("rounds = %d", len(result.Rounds))
		}
		for _, camp := range [][]PartyResult{result.Attackers, result.Defenders} {
			for _, party := range camp {
				for id, initial := range party.Initial {
					survivors := party.Survivors[id]
					losses := party.Losses[id]
					if survivors < 0 || losses < 0 || survivors > initial {
						t.Fatalf("party %+v is inconsistent for %s", party, id)
					}
					if survivors+losses != initial {
						t.Fatalf("%s: %d survivors and %d losses out of %d", id, survivors, losses, initial)
					}
				}
			}
		}
		if result.Debris.Metal < 0 || result.Debris.Crystal < 0 || result.Debris.Deuterium != 0 {
			t.Fatalf("debris = %+v", result.Debris)
		}
		if result.Debris != result.ShipDebris.Plus(result.DefenseDebris) {
			t.Fatalf("debris breakdown does not add up: %+v", result)
		}
		if result.MoonChance < 0 || result.MoonChance > input.Rules.MaximumMoonChance {
			t.Fatalf("moon chance = %v", result.MoonChance)
		}
		if result.Outcome == AttackerWins {
			for _, party := range result.Defenders {
				for id, survivors := range party.Survivors {
					if survivors > 0 && !isDefense(input.Catalogue, id) {
						t.Fatalf("the attacker won while %s survived", id)
					}
				}
			}
		}
	})
}

func isDefense(catalogue unit.Catalogue, id unit.ID) bool {
	definition, known := catalogue.Definition(id)
	return known && definition.Family == unit.Defense
}

func BenchmarkResolveLargeBattle(b *testing.B) {
	input := plainInput(
		map[unit.ID]int64{unit.LightFighter: 100_000},
		map[unit.ID]int64{unit.RocketLauncher: 100_000},
	)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Resolve(input, random.NewSeeded(11)); err != nil {
			b.Fatal(err)
		}
	}
}
