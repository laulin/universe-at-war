// Package unit defines the ships and defenses catalogue and the production
// formulas of the shipyard.
package unit

import (
	"errors"
	"math"
	"math/bits"
	"time"

	"universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/prerequisite"
	"universeatwar/internal/domain/research"
	"universeatwar/internal/domain/rules"
)

// MaximumOrderQuantity caps a single production order.
const MaximumOrderQuantity = 1_000_000

// missileSlotsPerSiloLevel is how many missiles one silo level holds.
const missileSlotsPerSiloLevel = 10

var (
	ErrUnknownUnit     = errors.New("unit: unknown unit")
	ErrInvalidQuantity = errors.New("unit: quantity must be positive and representable")
	ErrQuantityLimit   = errors.New("unit: quantity exceeds the allowed maximum")
	ErrSiloCapacity    = errors.New("unit: missile silo capacity exceeded")
)

// ID is the stable persisted identifier of a unit, never a translated label.
type ID string

// Family separates the mobile units from the ones bound to a planet.
type Family string

const (
	Ship    Family = "ship"
	Defense Family = "defense"
)

const (
	SmallCargo     ID = "small_cargo"
	LargeCargo     ID = "large_cargo"
	LightFighter   ID = "light_fighter"
	HeavyFighter   ID = "heavy_fighter"
	Cruiser        ID = "cruiser"
	Battleship     ID = "battleship"
	ColonyShip     ID = "colony_ship"
	Recycler       ID = "recycler"
	EspionageProbe ID = "espionage_probe"
	Bomber         ID = "bomber"
	SolarSatellite ID = "solar_satellite"
	Destroyer      ID = "destroyer"
	Deathstar      ID = "deathstar"
	Battlecruiser  ID = "battlecruiser"

	RocketLauncher        ID = "rocket_launcher"
	LightLaser            ID = "light_laser"
	HeavyLaser            ID = "heavy_laser"
	GaussCannon           ID = "gauss_cannon"
	IonCannon             ID = "ion_cannon"
	PlasmaTurret          ID = "plasma_turret"
	SmallShieldDome       ID = "small_shield_dome"
	LargeShieldDome       ID = "large_shield_dome"
	AntiBallisticMissile  ID = "anti_ballistic_missile"
	InterplanetaryMissile ID = "interplanetary_missile"
)

// DriveUpgrade replaces the drive and the base speed of a unit once a
// technology reaches a level. The last matching upgrade of a definition wins,
// so upgrades are declared from the least to the most advanced.
type DriveUpgrade struct {
	Drive        research.ID
	MinimumLevel int
	BaseSpeed    int64
}

// Definition holds everything the production and combat algorithms need.
type Definition struct {
	ID              ID
	Family          Family
	BaseCost        economy.Resources
	Shield          int64
	Weapon          int64
	Cargo           int64
	BaseSpeed       int64
	Drive           research.ID
	DriveUpgrades   []DriveUpgrade
	FuelConsumption int64
	Prerequisites   []prerequisite.Requirement
	RapidFire       map[ID]int
	MaximumQuantity int64
	SiloSlots       int
}

// Hull is the structure a unit brings to combat.
func (d Definition) Hull() int64 {
	return (d.BaseCost.Metal + d.BaseCost.Crystal) / 10
}

// Inventory maps stable unit identifiers to owned quantities.
type Inventory map[ID]int64

// Validate refuses a negative inventory.
func (i Inventory) Validate() error {
	for id, quantity := range i {
		if quantity < 0 {
			return errors.New("unit: negative quantity of " + string(id))
		}
	}
	return nil
}

// Catalogue is the versioned content of the shipyard.
type Catalogue struct {
	definitions map[ID]Definition
	order       []ID
}

// Inputs is the planet state a production order is validated against.
type Inputs struct {
	State            prerequisite.State
	ShipyardLevel    int
	NaniteLevel      int
	MissileSiloLevel int
	Inventory        Inventory
	Pending          Inventory
}

// Order is the immutable calculation captured when a production starts.
type Order struct {
	Unit         ID
	Family       Family
	Quantity     int64
	UnitCost     economy.Resources
	TotalCost    economy.Resources
	UnitDuration time.Duration
	Duration     time.Duration
}

func building(id string, level int) prerequisite.Requirement {
	return prerequisite.Requirement{Kind: prerequisite.Building, ID: id, Level: level}
}

func technology(id research.ID, level int) prerequisite.Requirement {
	return prerequisite.Requirement{Kind: prerequisite.Research, ID: string(id), Level: level}
}

// probeRapidFire is the rapid fire every armed ship has against the unarmed
// units of the battlefield.
func probeRapidFire(extra map[ID]int) map[ID]int {
	rapidFire := map[ID]int{EspionageProbe: 5, SolarSatellite: 5}
	for id, shots := range extra {
		rapidFire[id] = shots
	}
	return rapidFire
}

// DefaultCatalogue returns the classic ship and defense catalogue.
func DefaultCatalogue() Catalogue {
	definitions := []Definition{
		{ID: SmallCargo, Family: Ship, BaseCost: economy.Resources{Metal: 2000, Crystal: 2000},
			Shield: 10, Weapon: 5, Cargo: 5000, BaseSpeed: 5000, Drive: research.CombustionDrive, FuelConsumption: 10,
			DriveUpgrades: []DriveUpgrade{{Drive: research.ImpulseDrive, MinimumLevel: 5, BaseSpeed: 10000}},
			Prerequisites: []prerequisite.Requirement{building("shipyard", 2), technology(research.CombustionDrive, 2)},
			RapidFire:     probeRapidFire(nil)},
		{ID: LargeCargo, Family: Ship, BaseCost: economy.Resources{Metal: 6000, Crystal: 6000},
			Shield: 25, Weapon: 5, Cargo: 25000, BaseSpeed: 7500, Drive: research.CombustionDrive, FuelConsumption: 50,
			Prerequisites: []prerequisite.Requirement{building("shipyard", 4), technology(research.CombustionDrive, 6)},
			RapidFire:     probeRapidFire(nil)},
		{ID: LightFighter, Family: Ship, BaseCost: economy.Resources{Metal: 3000, Crystal: 1000},
			Shield: 10, Weapon: 50, Cargo: 50, BaseSpeed: 12500, Drive: research.CombustionDrive, FuelConsumption: 20,
			Prerequisites: []prerequisite.Requirement{building("shipyard", 1), technology(research.CombustionDrive, 1)},
			RapidFire:     probeRapidFire(nil)},
		{ID: HeavyFighter, Family: Ship, BaseCost: economy.Resources{Metal: 6000, Crystal: 4000},
			Shield: 25, Weapon: 150, Cargo: 100, BaseSpeed: 10000, Drive: research.ImpulseDrive, FuelConsumption: 75,
			Prerequisites: []prerequisite.Requirement{building("shipyard", 3), technology(research.ArmourTechnology, 2), technology(research.ImpulseDrive, 2)},
			RapidFire:     probeRapidFire(map[ID]int{SmallCargo: 3})},
		{ID: Cruiser, Family: Ship, BaseCost: economy.Resources{Metal: 20000, Crystal: 7000, Deuterium: 2000},
			Shield: 50, Weapon: 400, Cargo: 800, BaseSpeed: 15000, Drive: research.ImpulseDrive, FuelConsumption: 300,
			Prerequisites: []prerequisite.Requirement{building("shipyard", 5), technology(research.ImpulseDrive, 4), technology(research.IonTechnology, 2)},
			RapidFire:     probeRapidFire(map[ID]int{LightFighter: 6, RocketLauncher: 10})},
		{ID: Battleship, Family: Ship, BaseCost: economy.Resources{Metal: 45000, Crystal: 15000},
			Shield: 200, Weapon: 1000, Cargo: 1500, BaseSpeed: 10000, Drive: research.HyperspaceDrive, FuelConsumption: 500,
			Prerequisites: []prerequisite.Requirement{building("shipyard", 7), technology(research.HyperspaceDrive, 4)},
			RapidFire:     probeRapidFire(nil)},
		{ID: ColonyShip, Family: Ship, BaseCost: economy.Resources{Metal: 10000, Crystal: 20000, Deuterium: 10000},
			Shield: 100, Weapon: 50, Cargo: 7500, BaseSpeed: 2500, Drive: research.ImpulseDrive, FuelConsumption: 1000,
			Prerequisites: []prerequisite.Requirement{building("shipyard", 4), technology(research.ImpulseDrive, 3)},
			RapidFire:     probeRapidFire(nil)},
		{ID: Recycler, Family: Ship, BaseCost: economy.Resources{Metal: 10000, Crystal: 6000, Deuterium: 2000},
			Shield: 10, Weapon: 1, Cargo: 20000, BaseSpeed: 2000, Drive: research.CombustionDrive, FuelConsumption: 300,
			DriveUpgrades: []DriveUpgrade{
				{Drive: research.ImpulseDrive, MinimumLevel: 17, BaseSpeed: 4000},
				{Drive: research.HyperspaceDrive, MinimumLevel: 15, BaseSpeed: 6000},
			},
			Prerequisites: []prerequisite.Requirement{building("shipyard", 4), technology(research.CombustionDrive, 6), technology(research.ShieldingTechnology, 2)},
			RapidFire:     probeRapidFire(nil)},
		{ID: EspionageProbe, Family: Ship, BaseCost: economy.Resources{Crystal: 1000},
			Shield: 0, Weapon: 0, Cargo: 5, BaseSpeed: 100000000, Drive: research.CombustionDrive, FuelConsumption: 1,
			Prerequisites: []prerequisite.Requirement{building("shipyard", 3), technology(research.CombustionDrive, 3), technology(research.EspionageTechnology, 2)}},
		{ID: Bomber, Family: Ship, BaseCost: economy.Resources{Metal: 50000, Crystal: 25000, Deuterium: 15000},
			Shield: 500, Weapon: 1000, Cargo: 500, BaseSpeed: 4000, Drive: research.ImpulseDrive, FuelConsumption: 1000,
			DriveUpgrades: []DriveUpgrade{{Drive: research.HyperspaceDrive, MinimumLevel: 8, BaseSpeed: 5000}},
			Prerequisites: []prerequisite.Requirement{building("shipyard", 8), technology(research.ImpulseDrive, 6), technology(research.PlasmaTechnology, 5)},
			RapidFire:     probeRapidFire(map[ID]int{RocketLauncher: 20, LightLaser: 20, HeavyLaser: 10, IonCannon: 10})},
		{ID: SolarSatellite, Family: Ship, BaseCost: economy.Resources{Crystal: 2000, Deuterium: 500},
			Shield: 1, Weapon: 1,
			Prerequisites: []prerequisite.Requirement{building("shipyard", 1)}},
		{ID: Destroyer, Family: Ship, BaseCost: economy.Resources{Metal: 60000, Crystal: 50000, Deuterium: 15000},
			Shield: 500, Weapon: 2000, Cargo: 2000, BaseSpeed: 5000, Drive: research.HyperspaceDrive, FuelConsumption: 1000,
			Prerequisites: []prerequisite.Requirement{building("shipyard", 9), technology(research.HyperspaceDrive, 6), technology(research.HyperspaceTechnology, 5)},
			RapidFire:     probeRapidFire(map[ID]int{LightLaser: 10, Battlecruiser: 2})},
		{ID: Deathstar, Family: Ship, BaseCost: economy.Resources{Metal: 5000000, Crystal: 4000000, Deuterium: 1000000},
			Shield: 50000, Weapon: 200000, Cargo: 1000000, BaseSpeed: 100, Drive: research.HyperspaceDrive, FuelConsumption: 1,
			Prerequisites: []prerequisite.Requirement{building("shipyard", 12), technology(research.HyperspaceDrive, 7), technology(research.HyperspaceTechnology, 6), technology(research.GravitonTechnology, 1)},
			RapidFire: probeRapidFire(map[ID]int{
				EspionageProbe: 1250, SolarSatellite: 1250, SmallCargo: 250, LargeCargo: 250, LightFighter: 200,
				HeavyFighter: 100, Cruiser: 33, Battleship: 30, ColonyShip: 250, Recycler: 250, Bomber: 25,
				Destroyer: 5, Battlecruiser: 15, RocketLauncher: 200, LightLaser: 200, HeavyLaser: 100,
				GaussCannon: 50, IonCannon: 100,
			})},
		{ID: Battlecruiser, Family: Ship, BaseCost: economy.Resources{Metal: 30000, Crystal: 40000, Deuterium: 15000},
			Shield: 400, Weapon: 700, Cargo: 750, BaseSpeed: 10000, Drive: research.HyperspaceDrive, FuelConsumption: 250,
			Prerequisites: []prerequisite.Requirement{building("shipyard", 8), technology(research.HyperspaceTechnology, 5), technology(research.HyperspaceDrive, 5), technology(research.LaserTechnology, 12)},
			RapidFire:     probeRapidFire(map[ID]int{SmallCargo: 3, LargeCargo: 3, HeavyFighter: 4, Cruiser: 4, Battleship: 7})},

		{ID: RocketLauncher, Family: Defense, BaseCost: economy.Resources{Metal: 2000},
			Shield: 20, Weapon: 80,
			Prerequisites: []prerequisite.Requirement{building("shipyard", 1)}},
		{ID: LightLaser, Family: Defense, BaseCost: economy.Resources{Metal: 1500, Crystal: 500},
			Shield: 25, Weapon: 100,
			Prerequisites: []prerequisite.Requirement{building("shipyard", 2), technology(research.EnergyTechnology, 1), technology(research.LaserTechnology, 3)}},
		{ID: HeavyLaser, Family: Defense, BaseCost: economy.Resources{Metal: 6000, Crystal: 2000},
			Shield: 100, Weapon: 250,
			Prerequisites: []prerequisite.Requirement{building("shipyard", 4), technology(research.EnergyTechnology, 3), technology(research.LaserTechnology, 6)}},
		{ID: GaussCannon, Family: Defense, BaseCost: economy.Resources{Metal: 20000, Crystal: 15000, Deuterium: 2000},
			Shield: 200, Weapon: 1100,
			Prerequisites: []prerequisite.Requirement{building("shipyard", 6), technology(research.EnergyTechnology, 6), technology(research.WeaponsTechnology, 3), technology(research.ShieldingTechnology, 1)}},
		{ID: IonCannon, Family: Defense, BaseCost: economy.Resources{Metal: 2000, Crystal: 6000},
			Shield: 500, Weapon: 150,
			Prerequisites: []prerequisite.Requirement{building("shipyard", 4), technology(research.IonTechnology, 4)}},
		{ID: PlasmaTurret, Family: Defense, BaseCost: economy.Resources{Metal: 50000, Crystal: 50000, Deuterium: 30000},
			Shield: 300, Weapon: 3000,
			Prerequisites: []prerequisite.Requirement{building("shipyard", 8), technology(research.PlasmaTechnology, 7)}},
		{ID: SmallShieldDome, Family: Defense, BaseCost: economy.Resources{Metal: 10000, Crystal: 10000},
			Shield: 2000, Weapon: 1, MaximumQuantity: 1,
			Prerequisites: []prerequisite.Requirement{building("shipyard", 1), technology(research.ShieldingTechnology, 2)}},
		{ID: LargeShieldDome, Family: Defense, BaseCost: economy.Resources{Metal: 50000, Crystal: 50000},
			Shield: 10000, Weapon: 1, MaximumQuantity: 1,
			Prerequisites: []prerequisite.Requirement{building("shipyard", 6), technology(research.ShieldingTechnology, 6)}},
		{ID: AntiBallisticMissile, Family: Defense, BaseCost: economy.Resources{Metal: 8000, Deuterium: 2000},
			Shield: 1, Weapon: 1, SiloSlots: 1,
			Prerequisites: []prerequisite.Requirement{building("shipyard", 1), building("missile_silo", 2)}},
		{ID: InterplanetaryMissile, Family: Defense, BaseCost: economy.Resources{Metal: 12500, Crystal: 2500, Deuterium: 10000},
			Shield: 1, Weapon: 12000, SiloSlots: 2,
			Prerequisites: []prerequisite.Requirement{building("shipyard", 1), building("missile_silo", 4), technology(research.ImpulseDrive, 1)}},
	}
	return NewCatalogue(definitions)
}

// NewCatalogue indexes definitions while keeping their declaration order, which
// is the order the interface displays.
func NewCatalogue(definitions []Definition) Catalogue {
	indexed := make(map[ID]Definition, len(definitions))
	order := make([]ID, 0, len(definitions))
	for _, definition := range definitions {
		indexed[definition.ID] = definition
		order = append(order, definition.ID)
	}
	return Catalogue{definitions: indexed, order: order}
}

// Definitions returns the entries of one family in stable interface order.
func (c Catalogue) Definitions(family Family) []Definition {
	result := make([]Definition, 0, len(c.order))
	for _, id := range c.order {
		if definition := c.definitions[id]; definition.Family == family {
			result = append(result, definition)
		}
	}
	return result
}

// Definition returns one entry of the catalogue.
// Strength grades a pile of units by what each brings to a battle: its gun, its
// shield and its hull. It is a rough measure, and it is meant to be.
func (c Catalogue) Strength(units map[ID]int64) int64 {
	var total int64
	for id, quantity := range units {
		if quantity <= 0 {
			continue
		}
		definition, known := c.Definition(id)
		if !known {
			continue
		}
		total += quantity * (definition.Weapon + definition.Shield + definition.Hull())
	}
	return total
}

func (c Catalogue) Definition(id ID) (Definition, bool) {
	definition, known := c.definitions[id]
	return definition, known
}

// UnitCost returns the cost of one unit under the family multiplier.
func (c Catalogue) UnitCost(id ID, configured rules.Ruleset) (economy.Resources, error) {
	definition, known := c.definitions[id]
	if !known {
		return economy.Resources{}, ErrUnknownUnit
	}
	multiplier := configured.Progression.ShipCostMultiplier
	if definition.Family == Defense {
		multiplier = configured.Progression.DefenseCostMultiplier
	}
	if multiplier <= 0 || math.IsNaN(multiplier) || math.IsInf(multiplier, 0) {
		return economy.Resources{}, errors.New("unit: invalid cost multiplier")
	}
	metal, err := scaled(definition.BaseCost.Metal, multiplier)
	if err != nil {
		return economy.Resources{}, err
	}
	crystal, err := scaled(definition.BaseCost.Crystal, multiplier)
	if err != nil {
		return economy.Resources{}, err
	}
	deuterium, err := scaled(definition.BaseCost.Deuterium, multiplier)
	if err != nil {
		return economy.Resources{}, err
	}
	return economy.Resources{Metal: metal, Crystal: crystal, Deuterium: deuterium}, nil
}

// TotalCost multiplies a unit cost by a quantity, refusing any overflow instead
// of silently saturating.
func TotalCost(unitCost economy.Resources, quantity int64) (economy.Resources, error) {
	if quantity <= 0 {
		return economy.Resources{}, ErrInvalidQuantity
	}
	metal, err := multiply(unitCost.Metal, quantity)
	if err != nil {
		return economy.Resources{}, err
	}
	crystal, err := multiply(unitCost.Crystal, quantity)
	if err != nil {
		return economy.Resources{}, err
	}
	deuterium, err := multiply(unitCost.Deuterium, quantity)
	if err != nil {
		return economy.Resources{}, err
	}
	return economy.Resources{Metal: metal, Crystal: crystal, Deuterium: deuterium}, nil
}

// UnitDuration calculates the build time of one unit.
func (c Catalogue) UnitDuration(cost economy.Resources, shipyardLevel, naniteLevel int, speed float64) (time.Duration, error) {
	if err := cost.Validate(); err != nil || shipyardLevel < 0 || naniteLevel < 0 ||
		speed <= 0 || math.IsNaN(speed) || math.IsInf(speed, 0) {
		return 0, errors.New("unit: invalid duration input")
	}
	if cost.Metal > math.MaxInt64-cost.Crystal {
		return 0, errors.New("unit: work overflow")
	}
	seconds := math.Floor(3600 * float64(cost.Metal+cost.Crystal) /
		(2500 * float64(1+shipyardLevel) * math.Pow(2, float64(naniteLevel)) * speed))
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds > float64(math.MaxInt64/int64(time.Second)) {
		return 0, errors.New("unit: duration overflow")
	}
	if seconds < 1 {
		seconds = 1
	}
	return time.Duration(int64(seconds)) * time.Second, nil
}

// Order validates a production request and captures its immutable calculation.
func (c Catalogue) Order(id ID, quantity int64, inputs Inputs, configured rules.Ruleset) (Order, error) {
	if err := configured.Validate(); err != nil {
		return Order{}, err
	}
	definition, known := c.definitions[id]
	if !known {
		return Order{}, ErrUnknownUnit
	}
	if quantity <= 0 || quantity > MaximumOrderQuantity {
		return Order{}, ErrInvalidQuantity
	}
	if err := prerequisite.Check(definition.Prerequisites, inputs.State); err != nil {
		return Order{}, err
	}
	if definition.MaximumQuantity > 0 {
		owned := inputs.Inventory[id] + inputs.Pending[id]
		if owned+quantity > definition.MaximumQuantity {
			return Order{}, ErrQuantityLimit
		}
	}
	if definition.SiloSlots > 0 {
		if err := c.checkSiloCapacity(definition, quantity, inputs); err != nil {
			return Order{}, err
		}
	}
	unitCost, err := c.UnitCost(id, configured)
	if err != nil {
		return Order{}, err
	}
	totalCost, err := TotalCost(unitCost, quantity)
	if err != nil {
		return Order{}, err
	}
	speed := configured.Time.ShipyardSpeed
	if definition.Family == Defense {
		speed = configured.Time.DefenseSpeed
	}
	unitDuration, err := c.UnitDuration(unitCost, inputs.ShipyardLevel, inputs.NaniteLevel, speed)
	if err != nil {
		return Order{}, err
	}
	if int64(unitDuration) > math.MaxInt64/quantity {
		return Order{}, errors.New("unit: duration overflow")
	}
	return Order{
		Unit:         id,
		Family:       definition.Family,
		Quantity:     quantity,
		UnitCost:     unitCost,
		TotalCost:    totalCost,
		UnitDuration: unitDuration,
		Duration:     unitDuration * time.Duration(quantity),
	}, nil
}

// checkSiloCapacity refuses missiles the silo cannot hold, counting the ones
// already stored and the ones still in production.
func (c Catalogue) checkSiloCapacity(definition Definition, quantity int64, inputs Inputs) error {
	capacity := int64(missileSlotsPerSiloLevel * inputs.MissileSiloLevel)
	used := int64(0)
	for id, stored := range inputs.Inventory {
		used += stored * int64(c.definitions[id].SiloSlots)
	}
	for id, pending := range inputs.Pending {
		used += pending * int64(c.definitions[id].SiloSlots)
	}
	requested, err := multiply(int64(definition.SiloSlots), quantity)
	if err != nil {
		return ErrInvalidQuantity
	}
	if used+requested > capacity {
		return ErrSiloCapacity
	}
	return nil
}

func scaled(base int64, factor float64) (int64, error) {
	if base == 0 {
		return 0, nil
	}
	value := float64(base) * factor
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > math.MaxInt64 {
		return 0, errors.New("unit: cost overflow")
	}
	return int64(math.Floor(value)), nil
}

func multiply(value, quantity int64) (int64, error) {
	if value == 0 || quantity == 0 {
		return 0, nil
	}
	if value < 0 || quantity < 0 {
		return 0, ErrInvalidQuantity
	}
	high, low := bits.Mul64(uint64(value), uint64(quantity))
	if high != 0 || low > math.MaxInt64 {
		return 0, ErrInvalidQuantity
	}
	return int64(low), nil
}
