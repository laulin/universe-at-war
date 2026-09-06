// Package espionage separates what the server knows from what a player has
// learned. A section that is not revealed is never copied into a report.
package espionage

import (
	"errors"
	"math"

	"universeatwar/internal/domain/building"
	"universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/random"
	"universeatwar/internal/domain/research"
	"universeatwar/internal/domain/rules"
	"universeatwar/internal/domain/unit"
)

// ErrNoProbe reports an espionage mission without any probe.
var ErrNoProbe = errors.New("espionage: at least one probe is required")

// Truth is the real state of a planet, which only the server ever sees.
type Truth struct {
	Resources economy.Resources
	Fleet     unit.Inventory
	Defenses  unit.Inventory
	Buildings building.Levels
	Research  research.Levels
}

// Report is what one espionage mission brought back. A nil section was not
// revealed, which is different from a section revealed empty.
type Report struct {
	Level     int
	Resources *economy.Resources
	Fleet     unit.Inventory
	Defenses  unit.Inventory
	Buildings building.Levels
	Research  research.Levels
}

// Level is the revelation level of a mission.
func Level(attackerLevel, defenderLevel int, probes int64) int {
	return attackerLevel - defenderLevel + integerRoot(probes)
}

// Reveal returns the sections the mission earned, as deep copies so the caller
// can never hand the truth itself to a player.
func Reveal(settings rules.EspionageSettings, attackerLevel, defenderLevel int, probes int64, truth Truth) (Report, error) {
	if probes <= 0 {
		return Report{}, ErrNoProbe
	}
	level := Level(attackerLevel, defenderLevel, probes)
	report := Report{Level: level}
	if level >= settings.ResourcesThreshold {
		resources := truth.Resources
		report.Resources = &resources
	}
	if level >= settings.FleetThreshold {
		report.Fleet = copyInventory(truth.Fleet)
	}
	if level >= settings.DefensesThreshold {
		report.Defenses = copyInventory(truth.Defenses)
	}
	if level >= settings.BuildingsThreshold {
		report.Buildings = copyBuildings(truth.Buildings)
	}
	if level >= settings.ResearchThreshold {
		report.Research = copyResearch(truth.Research)
	}
	return report, nil
}

// DetectionChance is the probability that the target notices the probes.
func DetectionChance(settings rules.EspionageSettings, attackerLevel, defenderLevel int, probes, defenderFleetSize int64) float64 {
	if probes <= 0 || defenderFleetSize <= 0 {
		return 0
	}
	chance := settings.DetectionBase * float64(probes) * float64(defenderFleetSize) *
		math.Pow(2, float64(defenderLevel-attackerLevel))
	if math.IsNaN(chance) || chance <= 0 {
		return 0
	}
	if chance > 1 {
		return 1
	}
	return chance
}

// Detected draws whether the mission was noticed.
func Detected(chance float64, source random.Source) bool {
	return random.Chance(source, chance)
}

// integerRoot is the integer square root, so the revelation level never depends
// on floating point rounding.
func integerRoot(value int64) int {
	if value <= 0 {
		return 0
	}
	root := int64(math.Sqrt(float64(value)))
	for root*root > value {
		root--
	}
	for (root+1)*(root+1) <= value {
		root++
	}
	return int(root)
}

func copyInventory(source unit.Inventory) unit.Inventory {
	copied := make(unit.Inventory, len(source))
	for id, quantity := range source {
		copied[id] = quantity
	}
	return copied
}

func copyBuildings(source building.Levels) building.Levels {
	copied := make(building.Levels, len(source))
	for id, level := range source {
		copied[id] = level
	}
	return copied
}

func copyResearch(source research.Levels) research.Levels {
	copied := make(research.Levels, len(source))
	for id, level := range source {
		copied[id] = level
	}
	return copied
}
