package ai

import (
	"sort"

	"universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/random"
	"universeatwar/internal/domain/universe"
)

// StorageAlert is the share of a store above which an artificial player worries
// about wasting what it produces.
const StorageAlert = .9

// Facility thresholds: the average mine level from which an artificial player
// stops digging and builds the installations that make the rest possible.
const (
	RoboticsFrom   = 5
	LaboratoryFrom = 8
	ShipyardFrom   = 10
)

// Mine ratios: the shape an empire aims for, metal leading, deuterium trailing.
var mineRatios = map[string]float64{
	"metal_mine": 1, "crystal_mine": .66, "deuterium_synthesizer": .4,
}

// Option is one line of a catalogue as the game itself offers it to a player:
// what it costs, whether it is unlocked and whether it can be paid for now.
type Option struct {
	ID         string
	Level      int
	Owned      int64
	Cost       economy.Resources
	Available  bool
	Affordable bool
	// Capacity is how many of a unit the current stock could pay for.
	Capacity int64
}

// Body is one of the bodies of an artificial player, as its own pages show it.
type Body struct {
	ID         int64
	Coordinate universe.Coordinate
	FreeFields int
	Busy       bool
	Stock      economy.Resources
	Capacity   economy.Resources
	Energy     economy.Energy
	Levels     map[string]int
	Options    map[string]Option
}

// level reads a building level, missing meaning zero.
func (b Body) level(id string) int {
	return b.Levels[id]
}

// averageMine is how far the three mines have come together.
func (b Body) averageMine() int {
	return (b.level("metal_mine") + b.level("crystal_mine") + b.level("deuterium_synthesizer")) / 3
}

// BuildingPriorities is what a body wants next, in order. Energy comes before
// anything, then not wasting what is produced, then the installations that
// unlock the rest, and only then the mine that lags furthest behind.
func BuildingPriorities(body Body) []string {
	var wanted []string
	if body.Energy.Consumed > body.Energy.Produced {
		wanted = append(wanted, "solar_plant")
	}
	wanted = append(wanted, fullStores(body)...)
	average := body.averageMine()
	roboticsTarget := min(10, max(2, average-3))
	if average >= RoboticsFrom && body.level("robotics_factory") < roboticsTarget {
		wanted = append(wanted, "robotics_factory")
	}
	laboratoryTarget := min(12, max(3, average-4))
	if average >= LaboratoryFrom && body.level("research_lab") < laboratoryTarget {
		wanted = append(wanted, "research_lab")
	}
	shipyardTarget := min(12, max(2, average-6))
	if average >= ShipyardFrom && body.level("shipyard") < shipyardTarget {
		wanted = append(wanted, "shipyard")
	}
	if average >= 16 && body.level("robotics_factory") >= 10 && body.level("nanite_factory") < min(4, 1+(average-16)/3) {
		wanted = append(wanted, "nanite_factory")
	}
	return append(wanted, mineOrder(body)...)
}

// fullStores names the stores about to overflow, fullest first.
func fullStores(body Body) []string {
	type store struct {
		building string
		share    float64
	}
	var stores []store
	for _, candidate := range []struct {
		building        string
		stock, capacity int64
	}{
		{"metal_storage", body.Stock.Metal, body.Capacity.Metal},
		{"crystal_storage", body.Stock.Crystal, body.Capacity.Crystal},
		{"deuterium_tank", body.Stock.Deuterium, body.Capacity.Deuterium},
	} {
		if candidate.capacity <= 0 {
			continue
		}
		share := float64(candidate.stock) / float64(candidate.capacity)
		if share >= StorageAlert {
			stores = append(stores, store{candidate.building, share})
		}
	}
	sort.SliceStable(stores, func(first, second int) bool {
		return stores[first].share > stores[second].share
	})
	names := make([]string, 0, len(stores))
	for _, entry := range stores {
		names = append(names, entry.building)
	}
	return names
}

// mineOrder ranks the three mines by how far each is behind the shape the
// empire aims for. Ties keep the metal, crystal, deuterium order.
func mineOrder(body Body) []string {
	mines := []string{"metal_mine", "crystal_mine", "deuterium_synthesizer"}
	sort.SliceStable(mines, func(first, second int) bool {
		return float64(body.level(mines[first]))/mineRatios[mines[first]] <
			float64(body.level(mines[second]))/mineRatios[mines[second]]
	})
	return mines
}

// ResearchPriorities is the fixed line an artificial player follows: see, move,
// then fight.
func ResearchPriorities() []string {
	return []string{
		"energy_technology", "combustion_drive", "espionage_technology", "computer_technology",
		"weapons_technology", "shielding_technology", "armour_technology", "impulse_drive", "astrophysics",
	}
}

// PlannedResearchPriorities advances through unlock goals instead of selecting
// the first technology forever. Several milestones of the same technology are
// deliberate: they let useful unlocks in between take their turn.
func PlannedResearchPriorities(options map[string]Option, preferences Preferences) []string {
	goals := []struct {
		id     string
		target int
	}{
		{"energy_technology", 2},
		{"laser_technology", 3},
		{"combustion_drive", 3},
		{"espionage_technology", 4},
		{"computer_technology", 4},
		{"armour_technology", 2},
		{"impulse_drive", 4},
		{"weapons_technology", 3},
		{"shielding_technology", 2},
		{"combustion_drive", 6},
		{"laser_technology", 6},
		{"ion_technology", 4},
		{"energy_technology", 6},
		{"shielding_technology", 5},
		{"hyperspace_technology", 5},
		{"hyperspace_drive", 6},
		{"laser_technology", 12},
		{"energy_technology", 8},
		{"ion_technology", 5},
		{"plasma_technology", 7},
		{"astrophysics", 4},
	}
	var wanted []string
	for _, goal := range goals {
		if option, known := options[goal.id]; known && option.Level < goal.target {
			wanted = append(wanted, goal.id)
		}
	}
	// Mature empires balance their combat technologies. A military character
	// aims slightly higher than an economy-first one.
	combatTarget := 8
	if preferences.Economy < .6 {
		combatTarget = 12
	}
	for _, id := range []string{"weapons_technology", "shielding_technology", "armour_technology"} {
		if option, known := options[id]; known && option.Level < combatTarget {
			wanted = append(wanted, id)
		}
	}
	return wanted
}

// ProductionPriorities is what a body builds next. The share of defences of the
// archetype is the chance that the yard turns to the ground rather than the sky,
// drawn from the seed of the player so the choice stays reproducible.
func ProductionPriorities(preferences Preferences, source random.Source) []string {
	if random.Chance(source, preferences.DefenceShare) {
		return []string{"rocket_launcher", "light_laser", "small_cargo", "light_fighter"}
	}
	return []string{"small_cargo", "light_fighter", "rocket_launcher"}
}

// TunedProductionPriorities builds the utility ships needed to discover and
// exploit the universe, then varies a broad combat or defensive roster. Locked
// and unaffordable entries are skipped later by Pick, so a young empire falls
// back naturally to light units while a mature one stops doing only that.
func TunedProductionPriorities(tuning Tuning, options map[string]Option, source random.Source) []string {
	var wanted []string
	if tuning.EspionageEnabled {
		probeTarget := max(int64(2), tuning.Probes*2)
		if option, known := options["espionage_probe"]; known && option.Owned < probeTarget {
			wanted = append(wanted, "espionage_probe")
		}
	}
	if random.Chance(source, tuning.DefenceShare) {
		wanted = append(wanted,
			"plasma_turret", "gauss_cannon", "heavy_laser", "ion_cannon",
			"small_shield_dome", "large_shield_dome", "light_laser", "rocket_launcher")
	} else {
		warships := []string{
			"battlecruiser", "destroyer", "bomber", "battleship", "cruiser", "heavy_fighter", "light_fighter",
		}
		start := source.IntN(len(warships))
		wanted = append(wanted, warships[start:]...)
		wanted = append(wanted, warships[:start]...)
	}
	if tuning.RecycleEnabled {
		if option, known := options["recycler"]; known && option.Owned < 5 {
			wanted = append(wanted, "recycler")
		}
	}
	wanted = append(wanted, "large_cargo", "small_cargo")
	if tuning.EspionageEnabled {
		wanted = append(wanted, "espionage_probe")
	}
	return wanted
}

// Pick returns the first wanted option the game would actually accept, and the
// first one it would accept if only it could be paid for.
func Pick(options map[string]Option, wanted []string) (chosen string, blocked string) {
	for _, id := range wanted {
		option, known := options[id]
		if !known || !option.Available {
			continue
		}
		if !option.Affordable {
			if blocked == "" {
				blocked = id
			}
			continue
		}
		return id, blocked
	}
	return "", blocked
}

// OrderSize is how many units an artificial player commits to at once: half of
// what it could pay for, never more than a batch, never less than one.
func OrderSize(capacity int64) int64 {
	return OrderSizeUpTo(capacity, 10)
}

// OrderSizeUpTo keeps part of the stock available for the other layers while
// respecting the individual appetite configured by the administrator.
func OrderSizeUpTo(capacity, maximum int64) int64 {
	if capacity <= 0 {
		return 0
	}
	size := capacity / 2
	if maximum < 1 {
		maximum = 1
	}
	if size > maximum {
		size = maximum
	}
	if size < 1 {
		size = 1
	}
	return size
}
