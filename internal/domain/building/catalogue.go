// Package building defines the generic building catalogue and construction formulas.
package building

import (
	"errors"
	"math"
	"time"

	"universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/rules"
)

type ID string

const (
	MetalMine            ID = "metal_mine"
	CrystalMine          ID = "crystal_mine"
	DeuteriumSynthesizer ID = "deuterium_synthesizer"
	SolarPlant           ID = "solar_plant"
	MetalStorage         ID = "metal_storage"
	CrystalStorage       ID = "crystal_storage"
	DeuteriumTank        ID = "deuterium_tank"
	RoboticsFactory      ID = "robotics_factory"
	NaniteFactory        ID = "nanite_factory"
	Shipyard             ID = "shipyard"
	ResearchLab          ID = "research_lab"
	MissileSilo          ID = "missile_silo"
	Terraformer          ID = "terraformer"
)

// Requirement is a minimum completed building level.
type Requirement struct {
	Building ID
	Level    int
}

// Definition contains all data needed by generic construction algorithms.
type Definition struct {
	ID            ID
	BaseCost      economy.Resources
	Growth        float64
	Prerequisites []Requirement
	MaximumLevel  int
}

type Catalogue struct {
	definitions map[ID]Definition
}

// Levels maps stable building identifiers to completed levels.
type Levels map[ID]int

// Plan is the immutable calculation captured when construction starts.
type Plan struct {
	Building    ID
	TargetLevel int
	Cost        economy.Resources
	Duration    time.Duration
}

func DefaultCatalogue() Catalogue {
	definitions := []Definition{
		{ID: MetalMine, BaseCost: economy.Resources{Metal: 60, Crystal: 15}, Growth: 1.5},
		{ID: CrystalMine, BaseCost: economy.Resources{Metal: 48, Crystal: 24}, Growth: 1.6},
		{ID: DeuteriumSynthesizer, BaseCost: economy.Resources{Metal: 225, Crystal: 75}, Growth: 1.5},
		{ID: SolarPlant, BaseCost: economy.Resources{Metal: 75, Crystal: 30}, Growth: 1.5},
		{ID: MetalStorage, BaseCost: economy.Resources{Metal: 1000}, Growth: 2},
		{ID: CrystalStorage, BaseCost: economy.Resources{Metal: 1000, Crystal: 500}, Growth: 2},
		{ID: DeuteriumTank, BaseCost: economy.Resources{Metal: 1000, Crystal: 1000}, Growth: 2},
		{ID: RoboticsFactory, BaseCost: economy.Resources{Metal: 400, Crystal: 120, Deuterium: 200}, Growth: 2},
		{ID: NaniteFactory, BaseCost: economy.Resources{Metal: 1_000_000, Crystal: 500_000, Deuterium: 100_000}, Growth: 2, Prerequisites: []Requirement{{Building: RoboticsFactory, Level: 10}}},
		{ID: Shipyard, BaseCost: economy.Resources{Metal: 400, Crystal: 200, Deuterium: 100}, Growth: 2, Prerequisites: []Requirement{{Building: RoboticsFactory, Level: 2}}},
		{ID: ResearchLab, BaseCost: economy.Resources{Metal: 200, Crystal: 400, Deuterium: 200}, Growth: 2},
		{ID: MissileSilo, BaseCost: economy.Resources{Metal: 20_000, Crystal: 20_000, Deuterium: 1000}, Growth: 2, Prerequisites: []Requirement{{Building: Shipyard, Level: 1}}},
		{ID: Terraformer, BaseCost: economy.Resources{Crystal: 50_000, Deuterium: 100_000}, Growth: 2, Prerequisites: []Requirement{{Building: NaniteFactory, Level: 1}}},
	}
	indexed := make(map[ID]Definition, len(definitions))
	for _, definition := range definitions {
		indexed[definition.ID] = definition
	}
	return Catalogue{definitions: indexed}
}

// Definitions returns catalogue entries in stable UI order.
func (c Catalogue) Definitions() []Definition {
	result := make([]Definition, 0, len(c.definitions))
	for _, id := range []ID{MetalMine, CrystalMine, DeuteriumSynthesizer, SolarPlant, MetalStorage, CrystalStorage, DeuteriumTank, RoboticsFactory, NaniteFactory, Shipyard, ResearchLab, MissileSilo, Terraformer} {
		result = append(result, c.definitions[id])
	}
	return result
}

func (c Catalogue) Cost(id ID, targetLevel int, multiplier float64) (economy.Resources, error) {
	definition, ok := c.definitions[id]
	if !ok {
		return economy.Resources{}, errors.New("building: unknown building")
	}
	if targetLevel < 1 || (definition.MaximumLevel > 0 && targetLevel > definition.MaximumLevel) || multiplier <= 0 || math.IsNaN(multiplier) || math.IsInf(multiplier, 0) {
		return economy.Resources{}, errors.New("building: invalid target level or multiplier")
	}
	factor := math.Pow(definition.Growth, float64(targetLevel-1)) * multiplier
	metal, err := costComponent(definition.BaseCost.Metal, factor)
	if err != nil {
		return economy.Resources{}, err
	}
	crystal, err := costComponent(definition.BaseCost.Crystal, factor)
	if err != nil {
		return economy.Resources{}, err
	}
	deuterium, err := costComponent(definition.BaseCost.Deuterium, factor)
	if err != nil {
		return economy.Resources{}, err
	}
	return economy.Resources{Metal: metal, Crystal: crystal, Deuterium: deuterium}, nil
}

// Duration calculates construction time from its snapshotted cost and bonuses.
func (c Catalogue) Duration(cost economy.Resources, roboticsLevel, naniteLevel int, speed float64) (time.Duration, error) {
	if err := cost.Validate(); err != nil || roboticsLevel < 0 || naniteLevel < 0 || speed <= 0 || math.IsNaN(speed) || math.IsInf(speed, 0) {
		return 0, errors.New("building: invalid duration input")
	}
	if cost.Metal > math.MaxInt64-cost.Crystal {
		return 0, errors.New("building: work overflow")
	}
	seconds := math.Floor(3600 * float64(cost.Metal+cost.Crystal) /
		(2500 * float64(1+roboticsLevel) * math.Pow(2, float64(naniteLevel)) * speed))
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds > float64(math.MaxInt64/int64(time.Second)) {
		return 0, errors.New("building: duration overflow")
	}
	if seconds < 1 {
		seconds = 1
	}
	return time.Duration(int64(seconds)) * time.Second, nil
}

// Plan validates fields and prerequisites and calculates the next level.
func (c Catalogue) Plan(id ID, levels Levels, usedFields, totalFields int, configured rules.Ruleset) (Plan, error) {
	if err := configured.Validate(); err != nil {
		return Plan{}, err
	}
	definition, ok := c.definitions[id]
	if !ok {
		return Plan{}, errors.New("building: unknown building")
	}
	if usedFields < 0 || totalFields <= 0 || usedFields >= totalFields {
		return Plan{}, errors.New("building: no free field")
	}
	for buildingID, level := range levels {
		if _, known := c.definitions[buildingID]; !known || level < 0 {
			return Plan{}, errors.New("building: invalid levels")
		}
	}
	for _, prerequisite := range definition.Prerequisites {
		if levels[prerequisite.Building] < prerequisite.Level {
			return Plan{}, errors.New("building: prerequisites are not met")
		}
	}
	target := levels[id] + 1
	cost, err := c.Cost(id, target, configured.Progression.BuildingCostMultiplier)
	if err != nil {
		return Plan{}, err
	}
	duration, err := c.Duration(cost, levels[RoboticsFactory], levels[NaniteFactory], configured.Time.BuildingSpeed)
	if err != nil {
		return Plan{}, err
	}
	return Plan{Building: id, TargetLevel: target, Cost: cost, Duration: duration}, nil
}

func costComponent(base int64, factor float64) (int64, error) {
	value := float64(base) * factor
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > math.MaxInt64 {
		return 0, errors.New("building: cost overflow")
	}
	return int64(math.Floor(value)), nil
}
