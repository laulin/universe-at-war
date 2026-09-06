package expedition

import (
	"testing"

	"universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/random"
	"universeatwar/internal/domain/rules"
	"universeatwar/internal/domain/unit"
)

func TestOutcomeTableFollowsItsWeights(t *testing.T) {
	settings := rules.Default().Expedition
	counts := map[Outcome]int{}
	for seed := uint64(0); seed < 4000; seed++ {
		outcome, err := Draw(settings, random.NewSeeded(seed))
		if err != nil {
			t.Fatal(err)
		}
		if !outcome.Valid() {
			t.Fatalf("Draw() = %q", outcome)
		}
		counts[outcome]++
	}
	// The common outcomes really are the common ones.
	if counts[Resources] <= counts[Rare] || counts[Nothing] <= counts[Aliens] {
		t.Fatalf("the table does not follow its weights: %v", counts)
	}
	// Every outcome with a weight shows up over four thousand draws.
	for _, outcome := range []Outcome{Resources, Nothing, Ships, Delay, Pirates, Aliens, Losses, Rare} {
		if counts[outcome] == 0 {
			t.Fatalf("%s never came up: %v", outcome, counts)
		}
	}

	// A weight of zero removes an outcome from the table entirely.
	quiet := settings
	quiet.Weights = rules.ExpeditionOutcomes{Nothing: 1}
	for seed := uint64(0); seed < 50; seed++ {
		outcome, err := Draw(quiet, random.NewSeeded(seed))
		if err != nil || outcome != Nothing {
			t.Fatalf("Draw(quiet) = %q %v", outcome, err)
		}
	}
	empty := settings
	empty.Weights = rules.ExpeditionOutcomes{}
	if _, err := Draw(empty, random.NewSeeded(1)); err != ErrEmptyTable {
		t.Fatalf("an empty table error = %v", err)
	}
}

func TestTheSameSeedGivesTheSameExpedition(t *testing.T) {
	settings := rules.Default().Expedition
	for seed := uint64(0); seed < 100; seed++ {
		first, _ := Draw(settings, random.NewSeeded(seed))
		second, _ := Draw(settings, random.NewSeeded(seed))
		if first != second {
			t.Fatalf("seed %d gave %q then %q", seed, first, second)
		}
	}
}

func TestFindNeverExceedsTheHold(t *testing.T) {
	cases := []struct {
		capacity int64
		factor   float64
		want     economy.Resources
	}{
		{12000, .5, economy.Resources{Metal: 3000, Crystal: 2000, Deuterium: 1000}},
		{12000, 2, economy.Resources{Metal: 6000, Crystal: 4000, Deuterium: 2000}},
		{0, .5, economy.Resources{}},
		{12000, 0, economy.Resources{}},
	}
	for _, testCase := range cases {
		found := Find(testCase.capacity, testCase.factor)
		if found != testCase.want {
			t.Fatalf("Find(%d, %v) = %+v, want %+v", testCase.capacity, testCase.factor, found, testCase.want)
		}
		if found.Metal+found.Crystal+found.Deuterium > testCase.capacity {
			t.Fatalf("Find(%d) carries more than the hold", testCase.capacity)
		}
	}
}

func TestSalvageBringsBackAtLeastOneShipAndScales(t *testing.T) {
	cost := economy.Resources{Metal: 2000, Crystal: 2000}
	if got := Salvage(40000, .2, cost); got != 2 {
		t.Fatalf("Salvage() = %d, want 2", got)
	}
	if got := Salvage(1000, .2, cost); got != 1 {
		t.Fatalf("a small hold still brings one ship back: %d", got)
	}
	if got := Salvage(0, .2, cost); got != 0 {
		t.Fatalf("an empty hold brought %d ships", got)
	}
	if got := Salvage(40000, .2, economy.Resources{}); got != 0 {
		t.Fatalf("a free ship was salvaged: %d", got)
	}
}

func TestLosingShipsNeverEmptiesTheFleet(t *testing.T) {
	composition := map[unit.ID]int64{unit.LightFighter: 10, unit.SmallCargo: 4}
	losses := Lose(composition, .25)
	if losses[unit.LightFighter] != 2 || losses[unit.SmallCargo] != 1 {
		t.Fatalf("Lose() = %v", losses)
	}
	// However harsh the share, something always comes home.
	lone := map[unit.ID]int64{unit.LightFighter: 1}
	if len(Lose(lone, 1)) != 0 {
		t.Fatal("the last ship of a fleet was lost")
	}
	whole := map[unit.ID]int64{unit.LightFighter: 4}
	survivors := whole[unit.LightFighter] - Lose(whole, 1)[unit.LightFighter]
	if survivors < 1 {
		t.Fatal("a total loss left nobody to come home")
	}
	if len(Lose(composition, 0)) != 0 {
		t.Fatal("a share of nothing still lost ships")
	}
}

func TestAmbushScalesWithWhatCameLooking(t *testing.T) {
	small := Ambush(10_000, Pirates, random.NewSeeded(1))
	large := Ambush(1_000_000, Pirates, random.NewSeeded(1))
	if total(small) >= total(large) {
		t.Fatalf("a large fleet met %d and a small one %d", total(large), total(small))
	}
	aliens := Ambush(1_000_000, Aliens, random.NewSeeded(1))
	if total(aliens) <= 0 || aliens[unit.Cruiser] == 0 {
		t.Fatalf("the aliens fielded %v", aliens)
	}
	if Ambush(1_000_000, Nothing, random.NewSeeded(1)) != nil {
		t.Fatal("a quiet outcome still ambushed the fleet")
	}
	if Ambush(0, Pirates, random.NewSeeded(1)) != nil {
		t.Fatal("an empty fleet was ambushed")
	}
}

func total(composition map[unit.ID]int64) int64 {
	var count int64
	for _, quantity := range composition {
		count += quantity
	}
	return count
}
