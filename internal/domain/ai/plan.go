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
	if average >= RoboticsFrom && body.level("robotics_factory") < 2 {
		wanted = append(wanted, "robotics_factory")
	}
	if average >= LaboratoryFrom && body.level("research_lab") < 3 {
		wanted = append(wanted, "research_lab")
	}
	if average >= ShipyardFrom && body.level("shipyard") < 2 {
		wanted = append(wanted, "shipyard")
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

// ProductionPriorities is what a body builds next. The share of defences of the
// archetype is the chance that the yard turns to the ground rather than the sky,
// drawn from the seed of the player so the choice stays reproducible.
func ProductionPriorities(preferences Preferences, source random.Source) []string {
	if random.Chance(source, preferences.DefenceShare) {
		return []string{"rocket_launcher", "light_laser", "small_cargo", "light_fighter"}
	}
	return []string{"small_cargo", "light_fighter", "rocket_launcher"}
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
	if capacity <= 0 {
		return 0
	}
	size := capacity / 2
	if size > 10 {
		size = 10
	}
	if size < 1 {
		size = 1
	}
	return size
}
