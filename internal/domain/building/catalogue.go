// Package building defines the generic building catalogue and construction formulas.
package building

import (
	"errors"
	"math"
	"time"

	"universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/prerequisite"
	"universeatwar/internal/domain/rules"
)

type ID string

const (
	MetalMine            ID = "metal_mine"
	CrystalMine          ID = "crystal_mine"
	DeuteriumSynthesizer ID = "deuterium_synthesizer"
	SolarPlant           ID = "solar_plant"
	FusionReactor        ID = "fusion_reactor"
	MetalStorage         ID = "metal_storage"
	CrystalStorage       ID = "crystal_storage"
	DeuteriumTank        ID = "deuterium_tank"
	RoboticsFactory      ID = "robotics_factory"
	NaniteFactory        ID = "nanite_factory"
	Shipyard             ID = "shipyard"
	ResearchLab          ID = "research_lab"
	AllianceDepot        ID = "alliance_depot"
	MissileSilo          ID = "missile_silo"
	Terraformer          ID = "terraformer"

	LunarBase     ID = "lunar_base"
	SensorPhalanx ID = "sensor_phalanx"
	JumpGate      ID = "jump_gate"
)

// Placement says which kind of celestial body may hold a building. A lunar
// building never appears on a planet, and the other way round.
type Placement string

const (
	OnPlanet Placement = "planet"
	OnMoon   Placement = "moon"
)

// Definition contains all data needed by generic construction algorithms.
type Definition struct {
	ID            ID
	Placement     Placement
	BaseCost      economy.Resources
	Growth        float64
	Prerequisites []prerequisite.Requirement
	MaximumLevel  int
}

func requiresBuilding(id ID, level int) prerequisite.Requirement {
	return prerequisite.Requirement{Kind: prerequisite.Building, ID: string(id), Level: level}
}

func requiresResearch(id string, level int) prerequisite.Requirement {
	return prerequisite.Requirement{Kind: prerequisite.Research, ID: id, Level: level}
}

type Catalogue struct {
	definitions map[ID]Definition
}

// Levels maps stable building identifiers to completed levels.
type Levels map[ID]int

// Generic converts the levels for the shared requirement checker.
func (l Levels) Generic() prerequisite.Levels {
	generic := make(prerequisite.Levels, len(l))
	for id, level := range l {
		generic[string(id)] = level
	}
	return generic
}

// Plan is the immutable calculation captured when construction starts.
type Plan struct {
	Building    ID
	TargetLevel int
	Cost        economy.Resources
	Duration    time.Duration
	// EnergyChange is what reaching the target level does to the body's energy
	// balance: negative for a mine, which draws more, positive for the solar
	// plant, which yields more, and zero for everything else.
	EnergyChange int64
}

func DefaultCatalogue() Catalogue {
	definitions := []Definition{
		{ID: MetalMine, BaseCost: economy.Resources{Metal: 60, Crystal: 15}, Growth: 1.5},
		{ID: CrystalMine, BaseCost: economy.Resources{Metal: 48, Crystal: 24}, Growth: 1.6},
		{ID: DeuteriumSynthesizer, BaseCost: economy.Resources{Metal: 225, Crystal: 75}, Growth: 1.5},
		{ID: SolarPlant, BaseCost: economy.Resources{Metal: 75, Crystal: 30}, Growth: 1.5},
		{ID: FusionReactor, BaseCost: economy.Resources{Metal: 900, Crystal: 360, Deuterium: 180}, Growth: 1.8, Prerequisites: []prerequisite.Requirement{requiresBuilding(DeuteriumSynthesizer, 5), requiresResearch("energy_technology", 3)}},
		{ID: MetalStorage, BaseCost: economy.Resources{Metal: 1000}, Growth: 2},
		{ID: CrystalStorage, BaseCost: economy.Resources{Metal: 1000, Crystal: 500}, Growth: 2},
		{ID: DeuteriumTank, BaseCost: economy.Resources{Metal: 1000, Crystal: 1000}, Growth: 2},
		{ID: RoboticsFactory, BaseCost: economy.Resources{Metal: 400, Crystal: 120, Deuterium: 200}, Growth: 2},
		{ID: NaniteFactory, BaseCost: economy.Resources{Metal: 1_000_000, Crystal: 500_000, Deuterium: 100_000}, Growth: 2, Prerequisites: []prerequisite.Requirement{requiresBuilding(RoboticsFactory, 10), requiresResearch("computer_technology", 10)}},
		{ID: Shipyard, BaseCost: economy.Resources{Metal: 400, Crystal: 200, Deuterium: 100}, Growth: 2, Prerequisites: []prerequisite.Requirement{requiresBuilding(RoboticsFactory, 2)}},
		{ID: ResearchLab, BaseCost: economy.Resources{Metal: 200, Crystal: 400, Deuterium: 200}, Growth: 2},
		{ID: AllianceDepot, BaseCost: economy.Resources{Metal: 20_000, Crystal: 40_000}, Growth: 2},
		{ID: MissileSilo, BaseCost: economy.Resources{Metal: 20_000, Crystal: 20_000, Deuterium: 1000}, Growth: 2, Prerequisites: []prerequisite.Requirement{requiresBuilding(Shipyard, 1)}},
		{ID: Terraformer, BaseCost: economy.Resources{Crystal: 50_000, Deuterium: 100_000}, Growth: 2, Prerequisites: []prerequisite.Requirement{requiresBuilding(NaniteFactory, 1), requiresResearch("energy_technology", 12)}},

		{ID: LunarBase, Placement: OnMoon, BaseCost: economy.Resources{Metal: 20_000, Crystal: 40_000, Deuterium: 20_000}, Growth: 2},
		{ID: SensorPhalanx, Placement: OnMoon, BaseCost: economy.Resources{Metal: 20_000, Crystal: 40_000, Deuterium: 20_000}, Growth: 2, Prerequisites: []prerequisite.Requirement{requiresBuilding(LunarBase, 1)}},
		{ID: JumpGate, Placement: OnMoon, BaseCost: economy.Resources{Metal: 2_000_000, Crystal: 4_000_000, Deuterium: 2_000_000}, Growth: 2, Prerequisites: []prerequisite.Requirement{requiresBuilding(LunarBase, 1), requiresResearch("hyperspace_technology", 7)}},
	}
	indexed := make(map[ID]Definition, len(definitions))
	for _, definition := range definitions {
		if definition.Placement == "" {
			definition.Placement = OnPlanet
		}
		indexed[definition.ID] = definition
	}
	return Catalogue{definitions: indexed}
}

// order is the stable interface order of the catalogue.
var order = []ID{
	MetalMine, CrystalMine, DeuteriumSynthesizer, SolarPlant, FusionReactor, MetalStorage, CrystalStorage, DeuteriumTank,
	RoboticsFactory, NaniteFactory, Shipyard, ResearchLab, AllianceDepot, MissileSilo, Terraformer,
	LunarBase, SensorPhalanx, JumpGate,
}

// Definitions returns catalogue entries in stable UI order.
func (c Catalogue) Definitions() []Definition {
	result := make([]Definition, 0, len(c.definitions))
	for _, id := range order {
		if definition, known := c.definitions[id]; known {
			result = append(result, definition)
		}
	}
	return result
}

// DefinitionsFor returns the entries one kind of body may hold.
func (c Catalogue) DefinitionsFor(placement Placement) []Definition {
	result := make([]Definition, 0, len(c.definitions))
	for _, definition := range c.Definitions() {
		if definition.Placement == placement {
			result = append(result, definition)
		}
	}
	return result
}

// Definition returns one entry of the catalogue.
func (c Catalogue) Definition(id ID) (Definition, bool) {
	definition, known := c.definitions[id]
	return definition, known
}

func (c Catalogue) Cost(id ID, targetLevel int, multiplier float64) (economy.Resources, error) {
	definition, ok := c.definitions[id]
	if !ok {
		return economy.Resources{}, ErrUnknownBuilding
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

var (
	// ErrWrongPlacement reports a building that cannot stand on this body.
	ErrWrongPlacement = errors.New("building: this building cannot be built here")
	// ErrUnknownBuilding reports an identifier the catalogue does not carry.
	ErrUnknownBuilding = errors.New("building: unknown building")
	// ErrNoFreeField reports a body with no room left for another building.
	ErrNoFreeField = errors.New("building: no free field")
)

// Plan validates placement, fields and prerequisites and calculates the next
// level.
func (c Catalogue) Plan(id ID, placement Placement, levels Levels, researches prerequisite.Levels, usedFields, totalFields int, configured rules.Ruleset) (Plan, error) {
	if err := configured.Validate(); err != nil {
		return Plan{}, err
	}
	definition, ok := c.definitions[id]
	if !ok {
		return Plan{}, ErrUnknownBuilding
	}
	if definition.Placement != placement {
		return Plan{}, ErrWrongPlacement
	}
	if usedFields < 0 || totalFields <= 0 || usedFields >= totalFields {
		return Plan{}, ErrNoFreeField
	}
	for buildingID, level := range levels {
		if _, known := c.definitions[buildingID]; !known || level < 0 {
			return Plan{}, errors.New("building: invalid levels")
		}
	}
	if err := prerequisite.Check(definition.Prerequisites, prerequisite.State{Buildings: levels.Generic(), Researches: researches}); err != nil {
		return Plan{}, err
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
	change, err := energyChange(id, levels[id], target, researches, configured.Economy.EnergyConsumptionGrowth)
	if err != nil {
		return Plan{}, err
	}
	return Plan{Building: id, TargetLevel: target, Cost: cost, Duration: duration, EnergyChange: change}, nil
}

// energyChange answers what the level being ordered does to the body's energy
// balance, which is the figure a player decides on: a mine costs energy, the
// solar plant and fusion reactor give it, and the rest of the catalogue neither.
func energyChange(id ID, currentLevel, targetLevel int, researches prerequisite.Levels, consumptionGrowth float64) (int64, error) {
	var before, after int64
	var err error
	switch id {
	case MetalMine, CrystalMine, DeuteriumSynthesizer:
		if before, err = economy.MineEnergy(currentLevel, consumptionGrowth); err != nil {
			return 0, err
		}
		if after, err = economy.MineEnergy(targetLevel, consumptionGrowth); err != nil {
			return 0, err
		}
		return before - after, nil
	case SolarPlant:
		if before, err = economy.SolarPlantEnergy(currentLevel); err != nil {
			return 0, err
		}
		if after, err = economy.SolarPlantEnergy(targetLevel); err != nil {
			return 0, err
		}
		return after - before, nil
	case FusionReactor:
		energyTechnology := researches["energy_technology"]
		if before, err = economy.FusionReactorEnergy(currentLevel, energyTechnology); err != nil {
			return 0, err
		}
		if after, err = economy.FusionReactorEnergy(targetLevel, energyTechnology); err != nil {
			return 0, err
		}
		return after - before, nil
	default:
		return 0, nil
	}
}

func costComponent(base int64, factor float64) (int64, error) {
	value := float64(base) * factor
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > math.MaxInt64 {
		return 0, errors.New("building: cost overflow")
	}
	return int64(math.Floor(value)), nil
}

// RequirementEdges exposes the building graph for validation.
func (c Catalogue) RequirementEdges() map[prerequisite.Node][]prerequisite.Node {
	edges := make(map[prerequisite.Node][]prerequisite.Node, len(c.definitions))
	for id, definition := range c.definitions {
		node := prerequisite.Node{Kind: prerequisite.Building, ID: string(id)}
		var dependencies []prerequisite.Node
		for _, requirement := range definition.Prerequisites {
			if requirement.Kind == prerequisite.Building {
				dependencies = append(dependencies, requirement.Node())
			}
		}
		edges[node] = dependencies
	}
	return edges
}
