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

func TestResearchPlanMovesThroughUnlockMilestones(t *testing.T) {
	options := map[string]Option{
		"energy_technology":    {ID: "energy_technology", Level: 0, Available: true, Affordable: true},
		"laser_technology":     {ID: "laser_technology", Level: 0, Available: false, Affordable: true},
		"combustion_drive":     {ID: "combustion_drive", Level: 0, Available: false, Affordable: true},
		"espionage_technology": {ID: "espionage_technology", Level: 0, Available: false, Affordable: true},
		"computer_technology":  {ID: "computer_technology", Level: 0, Available: true, Affordable: true},
		"weapons_technology":   {ID: "weapons_technology", Level: 0},
		"shielding_technology": {ID: "shielding_technology", Level: 0},
		"armour_technology":    {ID: "armour_technology", Level: 0},
	}
	chosen, _ := Pick(options, PlannedResearchPriorities(options, Raider.Preferences()))
	if chosen != "energy_technology" {
		t.Fatalf("a new empire chose %q, want energy_technology", chosen)
	}
	energy := options["energy_technology"]
	energy.Level = 2
	options["energy_technology"] = energy
	laser := options["laser_technology"]
	laser.Available = true
	options["laser_technology"] = laser
	chosen, _ = Pick(options, PlannedResearchPriorities(options, Raider.Preferences()))
	if chosen != "laser_technology" {
		t.Fatalf("an empire kept repeating its first technology and chose %q", chosen)
	}
}

func TestArchetypeResearchFocusHasFiniteMilestones(t *testing.T) {
	options := map[string]Option{
		"combustion_drive":     {ID: "combustion_drive", Level: 6, Available: true, Affordable: true},
		"impulse_drive":        {ID: "impulse_drive", Level: 5, Available: true, Affordable: true},
		"espionage_technology": {ID: "espionage_technology", Level: 8, Available: true, Affordable: true},
		"weapons_technology":   {ID: "weapons_technology", Level: 8, Available: true, Affordable: true},
		"computer_technology":  {ID: "computer_technology", Level: 8, Available: true, Affordable: true},
		"astrophysics":         {ID: "astrophysics", Level: 0, Available: true, Affordable: true},
	}
	profile := Profile{Archetype: Raider}
	wanted := TunedResearchPriorities(options, profile)
	if len(wanted) == 0 || wanted[0] == "combustion_drive" {
		t.Fatalf("a mature raider remained stuck on its first focus: %v", wanted)
	}
	for _, id := range wanted {
		if id == "astrophysics" {
			return
		}
	}
	t.Fatalf("the common expansion line disappeared behind the archetype: %v", wanted)
}

func TestArchetypeFacilitiesStopAtTheirTarget(t *testing.T) {
	settled := body(map[string]int{
		"metal_mine": 12, "crystal_mine": 8, "deuterium_synthesizer": 5,
		"shipyard": 3, "research_lab": 3, "robotics_factory": 2,
	}, economy.Energy{Produced: 200, Consumed: 100}, economy.Resources{}, economy.Resources{})
	wanted := TunedBuildingPriorities(settled, Raider)
	if len(wanted) == 0 || wanted[0] == "shipyard" {
		t.Fatalf("a raider kept raising a completed facility instead of its economy: %v", wanted)
	}
}

func TestTunedProductionBuildsUtilityAndAdvancedUnits(t *testing.T) {
	tuning := Raider.Tuning()
	tuning.DefenceShare = 0
	options := map[string]Option{
		"espionage_probe": {ID: "espionage_probe", Owned: 0},
		"battlecruiser":   {ID: "battlecruiser"},
		"destroyer":       {ID: "destroyer"},
		"bomber":          {ID: "bomber"},
		"battleship":      {ID: "battleship"},
		"cruiser":         {ID: "cruiser"},
		"heavy_fighter":   {ID: "heavy_fighter"},
		"light_fighter":   {ID: "light_fighter"},
	}
	wanted := TunedProductionPriorities(tuning, options, random.NewScript([]int{2}, nil))
	if wanted[0] != "espionage_probe" {
		t.Fatalf("a raider without probes wants %v", wanted)
	}
	probe := options["espionage_probe"]
	probe.Owned = tuning.Probes * 2
	options["espionage_probe"] = probe
	wanted = TunedProductionPriorities(tuning, options, random.NewScript([]int{2}, nil))
	if wanted[0] == "small_cargo" || wanted[0] == "light_fighter" {
		t.Fatalf("a mature yard still starts from the old narrow roster: %v", wanted)
	}
	found := map[string]bool{}
	for _, id := range wanted {
		found[id] = true
	}
	for _, id := range []string{"cruiser", "battleship", "bomber", "destroyer", "battlecruiser"} {
		if !found[id] {
			t.Fatalf("the production plan never considers %s: %v", id, wanted)
		}
	}
}
