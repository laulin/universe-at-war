// Package research defines the technology graph and the progression formulas.
package research

import (
	"errors"
	"math"
	"slices"
	"time"

	"universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/prerequisite"
	"universeatwar/internal/domain/rules"
)

// ID is the stable persisted identifier of a research, never a translated label.
type ID string

const (
	EnergyTechnology             ID = "energy_technology"
	LaserTechnology              ID = "laser_technology"
	IonTechnology                ID = "ion_technology"
	HyperspaceTechnology         ID = "hyperspace_technology"
	PlasmaTechnology             ID = "plasma_technology"
	CombustionDrive              ID = "combustion_drive"
	ImpulseDrive                 ID = "impulse_drive"
	HyperspaceDrive              ID = "hyperspace_drive"
	EspionageTechnology          ID = "espionage_technology"
	ComputerTechnology           ID = "computer_technology"
	Astrophysics                 ID = "astrophysics"
	IntergalacticResearchNetwork ID = "intergalactic_research_network"
	WeaponsTechnology            ID = "weapons_technology"
	ShieldingTechnology          ID = "shielding_technology"
	ArmourTechnology             ID = "armour_technology"
	GravitonTechnology           ID = "graviton_technology"
)

// researchLaboratory is the building whose level drives research duration.
const researchLaboratory = "research_lab"

// Definition holds everything the generic progression algorithms need.
type Definition struct {
	ID            ID
	BaseCost      economy.Resources
	BaseEnergy    int64
	Growth        float64
	EnergyGrowth  float64
	Prerequisites []prerequisite.Requirement
	MaximumLevel  int
}

// Catalogue is the versioned content of the technology tree.
type Catalogue struct {
	definitions map[ID]Definition
	order       []ID
}

// Levels maps stable research identifiers to completed levels.
type Levels map[ID]int

// Generic converts the levels for the shared requirement checker.
func (l Levels) Generic() prerequisite.Levels {
	generic := make(prerequisite.Levels, len(l))
	for id, level := range l {
		generic[string(id)] = level
	}
	return generic
}

// Laboratories describes the laboratories a player may put on one research.
type Laboratories struct {
	Local        int
	Remote       []int
	NetworkLevel int
}

// Plan is the immutable calculation captured when a research starts.
type Plan struct {
	Research            ID
	TargetLevel         int
	Cost                economy.Resources
	Energy              int64
	EffectiveLaboratory int
	Duration            time.Duration
}

func building(id string, level int) prerequisite.Requirement {
	return prerequisite.Requirement{Kind: prerequisite.Building, ID: id, Level: level}
}

func research(id ID, level int) prerequisite.Requirement {
	return prerequisite.Requirement{Kind: prerequisite.Research, ID: string(id), Level: level}
}

// DefaultCatalogue returns the classic technology tree.
func DefaultCatalogue() Catalogue {
	definitions := []Definition{
		{ID: EnergyTechnology, BaseCost: economy.Resources{Crystal: 800, Deuterium: 400}, Growth: 2,
			Prerequisites: []prerequisite.Requirement{building(researchLaboratory, 1)}},
		{ID: LaserTechnology, BaseCost: economy.Resources{Metal: 200, Crystal: 100}, Growth: 2,
			Prerequisites: []prerequisite.Requirement{building(researchLaboratory, 1), research(EnergyTechnology, 2)}},
		{ID: IonTechnology, BaseCost: economy.Resources{Metal: 1000, Crystal: 300, Deuterium: 100}, Growth: 2,
			Prerequisites: []prerequisite.Requirement{building(researchLaboratory, 4), research(LaserTechnology, 5), research(EnergyTechnology, 4)}},
		{ID: HyperspaceTechnology, BaseCost: economy.Resources{Crystal: 4000, Deuterium: 2000}, Growth: 2,
			Prerequisites: []prerequisite.Requirement{building(researchLaboratory, 7), research(EnergyTechnology, 5), research(ShieldingTechnology, 5)}},
		{ID: PlasmaTechnology, BaseCost: economy.Resources{Metal: 2000, Crystal: 4000, Deuterium: 1000}, Growth: 2,
			Prerequisites: []prerequisite.Requirement{building(researchLaboratory, 4), research(EnergyTechnology, 8), research(LaserTechnology, 10), research(IonTechnology, 5)}},
		{ID: CombustionDrive, BaseCost: economy.Resources{Metal: 400, Deuterium: 600}, Growth: 2,
			Prerequisites: []prerequisite.Requirement{building(researchLaboratory, 1), research(EnergyTechnology, 1)}},
		{ID: ImpulseDrive, BaseCost: economy.Resources{Metal: 2000, Crystal: 4000, Deuterium: 600}, Growth: 2,
			Prerequisites: []prerequisite.Requirement{building(researchLaboratory, 2), research(EnergyTechnology, 1)}},
		{ID: HyperspaceDrive, BaseCost: economy.Resources{Metal: 10000, Crystal: 20000, Deuterium: 6000}, Growth: 2,
			Prerequisites: []prerequisite.Requirement{building(researchLaboratory, 7), research(HyperspaceTechnology, 3)}},
		{ID: EspionageTechnology, BaseCost: economy.Resources{Metal: 200, Crystal: 1000, Deuterium: 200}, Growth: 2,
			Prerequisites: []prerequisite.Requirement{building(researchLaboratory, 3)}},
		{ID: ComputerTechnology, BaseCost: economy.Resources{Crystal: 400, Deuterium: 600}, Growth: 2,
			Prerequisites: []prerequisite.Requirement{building(researchLaboratory, 1)}},
		{ID: Astrophysics, BaseCost: economy.Resources{Metal: 4000, Crystal: 8000, Deuterium: 4000}, Growth: 1.75,
			Prerequisites: []prerequisite.Requirement{building(researchLaboratory, 3), research(EspionageTechnology, 4), research(ImpulseDrive, 3)}},
		{ID: IntergalacticResearchNetwork, BaseCost: economy.Resources{Metal: 240000, Crystal: 400000, Deuterium: 160000}, Growth: 2,
			Prerequisites: []prerequisite.Requirement{building(researchLaboratory, 10), research(ComputerTechnology, 8), research(HyperspaceTechnology, 8)}},
		{ID: WeaponsTechnology, BaseCost: economy.Resources{Metal: 800, Crystal: 200}, Growth: 2,
			Prerequisites: []prerequisite.Requirement{building(researchLaboratory, 4)}},
		{ID: ShieldingTechnology, BaseCost: economy.Resources{Metal: 200, Crystal: 600}, Growth: 2,
			Prerequisites: []prerequisite.Requirement{building(researchLaboratory, 6), research(EnergyTechnology, 3)}},
		{ID: ArmourTechnology, BaseCost: economy.Resources{Metal: 1000}, Growth: 2,
			Prerequisites: []prerequisite.Requirement{building(researchLaboratory, 2)}},
		{ID: GravitonTechnology, BaseEnergy: 300000, Growth: 3, EnergyGrowth: 3,
			Prerequisites: []prerequisite.Requirement{building(researchLaboratory, 12)}},
	}
	return NewCatalogue(definitions)
}

// NewCatalogue indexes definitions while keeping their declaration order, which
// is the order the interface displays.
func NewCatalogue(definitions []Definition) Catalogue {
	indexed := make(map[ID]Definition, len(definitions))
	order := make([]ID, 0, len(definitions))
	for _, definition := range definitions {
		if definition.EnergyGrowth == 0 {
			definition.EnergyGrowth = definition.Growth
		}
		indexed[definition.ID] = definition
		order = append(order, definition.ID)
	}
	return Catalogue{definitions: indexed, order: order}
}

// Definitions returns catalogue entries in stable interface order.
func (c Catalogue) Definitions() []Definition {
	result := make([]Definition, 0, len(c.order))
	for _, id := range c.order {
		result = append(result, c.definitions[id])
	}
	return result
}

// Definition returns one entry of the catalogue.
func (c Catalogue) Definition(id ID) (Definition, bool) {
	definition, known := c.definitions[id]
	return definition, known
}

// RequirementEdges exposes the research graph for validation.
func (c Catalogue) RequirementEdges() map[prerequisite.Node][]prerequisite.Node {
	edges := make(map[prerequisite.Node][]prerequisite.Node, len(c.definitions))
	for id, definition := range c.definitions {
		node := prerequisite.Node{Kind: prerequisite.Research, ID: string(id)}
		var dependencies []prerequisite.Node
		for _, requirement := range definition.Prerequisites {
			if requirement.Kind == prerequisite.Research {
				dependencies = append(dependencies, requirement.Node())
			}
		}
		edges[node] = dependencies
	}
	return edges
}

// Cost returns the resources and the available energy the next level demands.
func (c Catalogue) Cost(id ID, targetLevel int, multiplier float64) (economy.Resources, int64, error) {
	definition, known := c.definitions[id]
	if !known {
		return economy.Resources{}, 0, errors.New("research: unknown research")
	}
	if targetLevel < 1 || (definition.MaximumLevel > 0 && targetLevel > definition.MaximumLevel) ||
		multiplier <= 0 || math.IsNaN(multiplier) || math.IsInf(multiplier, 0) {
		return economy.Resources{}, 0, errors.New("research: invalid target level or multiplier")
	}
	factor := math.Pow(definition.Growth, float64(targetLevel-1)) * multiplier
	metal, err := scaled(definition.BaseCost.Metal, factor)
	if err != nil {
		return economy.Resources{}, 0, err
	}
	crystal, err := scaled(definition.BaseCost.Crystal, factor)
	if err != nil {
		return economy.Resources{}, 0, err
	}
	deuterium, err := scaled(definition.BaseCost.Deuterium, factor)
	if err != nil {
		return economy.Resources{}, 0, err
	}
	energy, err := scaled(definition.BaseEnergy, math.Pow(definition.EnergyGrowth, float64(targetLevel-1))*multiplier)
	if err != nil {
		return economy.Resources{}, 0, err
	}
	return economy.Resources{Metal: metal, Crystal: crystal, Deuterium: deuterium}, energy, nil
}

// Duration calculates research time from the snapshotted cost and the
// laboratories the player may put on it.
func (c Catalogue) Duration(cost economy.Resources, laboratories Laboratories, minimumLaboratory int, bonus, speed float64) (time.Duration, error) {
	if err := cost.Validate(); err != nil {
		return 0, errors.New("research: invalid duration cost")
	}
	if laboratories.Local < 0 || laboratories.NetworkLevel < 0 || minimumLaboratory < 0 ||
		bonus <= 0 || speed <= 0 || math.IsNaN(bonus) || math.IsInf(bonus, 0) || math.IsNaN(speed) || math.IsInf(speed, 0) {
		return 0, errors.New("research: invalid duration input")
	}
	if cost.Metal > math.MaxInt64-cost.Crystal {
		return 0, errors.New("research: work overflow")
	}
	effective := EffectiveLaboratory(laboratories, minimumLaboratory)
	seconds := math.Floor(3600 * float64(cost.Metal+cost.Crystal) /
		(1000 * (1 + bonus*float64(effective)) * speed))
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds > float64(math.MaxInt64/int64(time.Second)) {
		return 0, errors.New("research: duration overflow")
	}
	if seconds < 1 {
		seconds = 1
	}
	return time.Duration(int64(seconds)) * time.Second, nil
}

// EffectiveLaboratory adds the best eligible remote laboratories allowed by the
// research network. A laboratory that could not run the research alone never
// contributes.
func EffectiveLaboratory(laboratories Laboratories, minimumLaboratory int) int {
	eligible := make([]int, 0, len(laboratories.Remote))
	for _, level := range laboratories.Remote {
		if level > 0 && level >= minimumLaboratory {
			eligible = append(eligible, level)
		}
	}
	slices.SortFunc(eligible, func(a, b int) int { return b - a })
	effective := laboratories.Local
	for index := 0; index < laboratories.NetworkLevel && index < len(eligible); index++ {
		effective += eligible[index]
	}
	return effective
}

// Plan validates prerequisites and calculates the next level of a research.
func (c Catalogue) Plan(id ID, state prerequisite.State, laboratories Laboratories, configured rules.Ruleset) (Plan, error) {
	if err := configured.Validate(); err != nil {
		return Plan{}, err
	}
	definition, known := c.definitions[id]
	if !known {
		return Plan{}, errors.New("research: unknown research")
	}
	if err := prerequisite.Check(definition.Prerequisites, state); err != nil {
		return Plan{}, err
	}
	target := state.Level(prerequisite.Research, string(id)) + 1
	cost, energy, err := c.Cost(id, target, configured.Progression.ResearchCostMultiplier)
	if err != nil {
		return Plan{}, err
	}
	if !configured.Progression.ResearchNetworkEnabled {
		laboratories.NetworkLevel = 0
	}
	minimum := minimumLaboratory(definition)
	duration, err := c.Duration(cost, laboratories, minimum, configured.Progression.LaboratoryBonus, configured.Time.ResearchSpeed)
	if err != nil {
		return Plan{}, err
	}
	return Plan{
		Research:            id,
		TargetLevel:         target,
		Cost:                cost,
		Energy:              energy,
		EffectiveLaboratory: EffectiveLaboratory(laboratories, minimum),
		Duration:            duration,
	}, nil
}

// minimumLaboratory is the laboratory level the research itself demands.
func minimumLaboratory(definition Definition) int {
	for _, requirement := range definition.Prerequisites {
		if requirement.Kind == prerequisite.Building && requirement.ID == researchLaboratory {
			return requirement.Level
		}
	}
	return 0
}

func scaled(base int64, factor float64) (int64, error) {
	if base == 0 {
		return 0, nil
	}
	value := float64(base) * factor
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > math.MaxInt64 {
		return 0, errors.New("research: cost overflow")
	}
	return int64(math.Floor(value)), nil
}
