package ai

import (
	"reflect"
	"testing"

	"universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/random"
)

func body(levels map[string]int, energy economy.Energy, stock, capacity economy.Resources) Body {
	return Body{ID: 1, FreeFields: 100, Levels: levels, Energy: energy, Stock: stock, Capacity: capacity}
}

func TestBuildingPrioritiesFollowTheDocumentedOrder(t *testing.T) {
	full := economy.Resources{Metal: 9500, Crystal: 100, Deuterium: 0}
	room := economy.Resources{Metal: 10000, Crystal: 10000, Deuterium: 10000}
	cases := []struct {
		name  string
		body  Body
		first string
	}{
		{
			"energy comes before everything",
			body(map[string]int{"metal_mine": 4}, economy.Energy{Produced: 10, Consumed: 30}, economy.Resources{}, room),
			"solar_plant",
		},
		{
			"a full store comes before a new mine",
			body(map[string]int{"metal_mine": 4}, economy.Energy{Produced: 30}, full, room),
			"metal_storage",
		},
		{
			"the mines are dug in the shape of the empire",
			body(map[string]int{"metal_mine": 1}, economy.Energy{Produced: 30}, economy.Resources{}, room),
			"crystal_mine",
		},
		{
			"an empty planet starts with metal",
			body(map[string]int{}, economy.Energy{Produced: 30}, economy.Resources{}, room),
			"metal_mine",
		},
		{
			"installations come once the mines are deep enough",
			body(map[string]int{"metal_mine": 6, "crystal_mine": 6, "deuterium_synthesizer": 6},
				economy.Energy{Produced: 90, Consumed: 30}, economy.Resources{}, room),
			"robotics_factory",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := BuildingPriorities(testCase.body)
			if len(got) == 0 || got[0] != testCase.first {
				t.Fatalf("BuildingPriorities() = %v, want %s first", got, testCase.first)
			}
		})
	}
	// The three mines always close the list, so nothing ever blocks for good.
	tail := BuildingPriorities(body(map[string]int{"metal_mine": 1, "crystal_mine": 1, "deuterium_synthesizer": 1},
		economy.Energy{Produced: 90, Consumed: 30}, economy.Resources{}, room))
	if len(tail) != 3 {
		t.Fatalf("a quiet planet wants %v", tail)
	}
}

func TestPickTakesTheFirstAffordableAndNamesWhatItCannotPay(t *testing.T) {
	options := map[string]Option{
		"solar_plant": {ID: "solar_plant", Available: true, Affordable: false},
		"metal_mine":  {ID: "metal_mine", Available: true, Affordable: true},
		"shipyard":    {ID: "shipyard", Available: false, Affordable: true},
	}
	chosen, blocked := Pick(options, []string{"solar_plant", "shipyard", "metal_mine"})
	if chosen != "metal_mine" || blocked != "solar_plant" {
		t.Fatalf("Pick() = %q, %q", chosen, blocked)
	}
	if chosen, blocked := Pick(options, []string{"shipyard"}); chosen != "" || blocked != "" {
		t.Fatalf("a locked option was picked: %q %q", chosen, blocked)
	}
	if chosen, blocked := Pick(options, []string{"unknown"}); chosen != "" || blocked != "" {
		t.Fatalf("an unknown option was picked: %q %q", chosen, blocked)
	}
}

func TestProductionFollowsTheDefenceShareOfTheArchetype(t *testing.T) {
	// A turtle turns to the ground far more often than a raider.
	defensive, offensive := 0, 0
	for tick := uint64(0); tick < 200; tick++ {
		if ProductionPriorities(Turtle.Preferences(), random.NewSeeded(tick))[0] == "rocket_launcher" {
			defensive++
		}
		if ProductionPriorities(Raider.Preferences(), random.NewSeeded(tick))[0] == "rocket_launcher" {
			offensive++
		}
	}
	if defensive <= offensive {
		t.Fatalf("the turtle built defences %d times and the raider %d", defensive, offensive)
	}
	if offensive == 0 {
		t.Fatal("even a raider guards its planet once in a while")
	}
	// The same seed always yields the same yard order.
	if !reflect.DeepEqual(ProductionPriorities(Turtle.Preferences(), random.NewSeeded(7)),
		ProductionPriorities(Turtle.Preferences(), random.NewSeeded(7))) {
		t.Fatal("the same seed gave two different yard orders")
	}
}

func TestOrderSizeCommitsHalfAndNeverEverything(t *testing.T) {
	cases := map[int64]int64{0: 0, -3: 0, 1: 1, 2: 1, 7: 3, 40: 10, 1000: 10}
	for capacity, want := range cases {
		if got := OrderSize(capacity); got != want {
			t.Fatalf("OrderSize(%d) = %d, want %d", capacity, got, want)
		}
	}
}
