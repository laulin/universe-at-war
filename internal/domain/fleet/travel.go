package fleet

import (
	"errors"
	"math"
	"time"

	"universeatwar/internal/domain/research"
	"universeatwar/internal/domain/rules"
	"universeatwar/internal/domain/unit"
	"universeatwar/internal/domain/universe"
)

var (
	ErrEmptyComposition     = errors.New("fleet: composition is empty")
	ErrInvalidComposition   = errors.New("fleet: composition holds an invalid quantity")
	ErrImmobileUnit         = errors.New("fleet: unit cannot fly")
	ErrInvalidSpeed         = errors.New("fleet: speed must be a multiple of ten percent between 10 and 100")
	ErrInvalidTarget        = errors.New("fleet: target is not a valid destination")
	ErrInvalidMission       = errors.New("fleet: unknown mission")
	ErrInsufficientUnits    = errors.New("fleet: not enough units on the planet")
	ErrInsufficientFuel     = errors.New("fleet: not enough deuterium for the trip")
	ErrCargoExceedsCapacity = errors.New("fleet: cargo exceeds the remaining capacity")
	ErrNoFleetSlot          = errors.New("fleet: no free fleet slot")
)

// Composition maps stable ship identifiers to the quantity sent.
type Composition map[unit.ID]int64

// Count is the total number of ships of the composition.
func (c Composition) Count() int64 {
	var total int64
	for _, quantity := range c {
		total += quantity
	}
	return total
}

// Validate refuses an empty composition, a non-positive quantity and any unit
// that cannot fly.
func (c Composition) Validate(catalogue unit.Catalogue) error {
	if len(c) == 0 {
		return ErrEmptyComposition
	}
	empty := true
	for id, quantity := range c {
		if quantity < 0 {
			return ErrInvalidComposition
		}
		if quantity == 0 {
			continue
		}
		empty = false
		definition, known := catalogue.Definition(id)
		if !known {
			return ErrInvalidComposition
		}
		if definition.Family != unit.Ship || definition.BaseSpeed <= 0 || definition.Drive == "" {
			return ErrImmobileUnit
		}
	}
	if empty {
		return ErrEmptyComposition
	}
	return nil
}

// Distance is the travel distance between two coordinates under a topology.
func Distance(from, to universe.Coordinate, topology rules.TopologySettings) (int64, error) {
	limits := universe.Limits{
		Galaxies:  topology.Galaxies,
		Systems:   topology.SystemsPerGalaxy,
		Positions: topology.PositionsPerSystem,
	}
	if err := from.Validate(limits); err != nil {
		return 0, errors.Join(ErrInvalidTarget, err)
	}
	if err := to.Validate(limits); err != nil {
		return 0, errors.Join(ErrInvalidTarget, err)
	}
	if from.Galaxy != to.Galaxy {
		delta := circularDelta(from.Galaxy, to.Galaxy, topology.Galaxies, topology.CircularGalaxies)
		return int64(topology.InterGalaxyDistance) * int64(delta), nil
	}
	if from.System != to.System {
		delta := circularDelta(from.System, to.System, topology.SystemsPerGalaxy, topology.CircularSystems)
		return 2700 + int64(topology.InterSystemDistance)*int64(delta), nil
	}
	if from.Position != to.Position {
		return 1000 + 5*int64(absolute(from.Position-to.Position)), nil
	}
	return 5, nil
}

func circularDelta(first, second, total int, circular bool) int {
	delta := absolute(first - second)
	if circular && total-delta < delta {
		return total - delta
	}
	return delta
}

func absolute(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

// UnitSpeed is the speed of one ship under the player's drive technologies.
func UnitSpeed(definition unit.Definition, levels research.Levels) (int64, error) {
	if definition.Family != unit.Ship || definition.BaseSpeed <= 0 || definition.Drive == "" {
		return 0, ErrImmobileUnit
	}
	drive := definition.Drive
	base := definition.BaseSpeed
	for _, upgrade := range definition.DriveUpgrades {
		if levels[upgrade.Drive] >= upgrade.MinimumLevel {
			drive = upgrade.Drive
			base = upgrade.BaseSpeed
		}
	}
	factor, err := levels.DriveFactor(drive)
	if err != nil {
		return 0, err
	}
	speed := math.Floor(float64(base) * factor)
	if math.IsNaN(speed) || math.IsInf(speed, 0) || speed < 1 || speed > math.MaxInt64 {
		return 0, errors.New("fleet: speed overflow")
	}
	return int64(speed), nil
}

// FleetSpeed is the speed of the slowest ship of the composition.
func FleetSpeed(composition Composition, catalogue unit.Catalogue, levels research.Levels) (int64, error) {
	if err := composition.Validate(catalogue); err != nil {
		return 0, err
	}
	slowest := int64(math.MaxInt64)
	for id, quantity := range composition {
		if quantity == 0 {
			continue
		}
		definition, _ := catalogue.Definition(id)
		speed, err := UnitSpeed(definition, levels)
		if err != nil {
			return 0, err
		}
		if speed < slowest {
			slowest = speed
		}
	}
	return slowest, nil
}

// Duration is the one-way travel time of a fleet.
func Duration(distance, fleetSpeed int64, percent int, universeSpeed float64, minimum time.Duration) (time.Duration, error) {
	if percent < 10 || percent > 100 || percent%10 != 0 {
		return 0, ErrInvalidSpeed
	}
	if distance <= 0 || fleetSpeed <= 0 || universeSpeed <= 0 ||
		math.IsNaN(universeSpeed) || math.IsInf(universeSpeed, 0) || minimum <= 0 {
		return 0, errors.New("fleet: invalid duration input")
	}
	seconds := math.Floor((10 + 35000/float64(percent)*math.Sqrt(10*float64(distance)/float64(fleetSpeed))) / universeSpeed)
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds > float64(math.MaxInt64/int64(time.Second)) {
		return 0, errors.New("fleet: duration overflow")
	}
	duration := time.Duration(int64(seconds)) * time.Second
	if duration < minimum {
		return minimum, nil
	}
	return duration, nil
}

// Fuel is the deuterium a round trip burns, taken at departure.
func Fuel(composition Composition, catalogue unit.Catalogue, levels research.Levels, distance int64, percent int, consumption float64) (int64, error) {
	if err := composition.Validate(catalogue); err != nil {
		return 0, err
	}
	if percent < 10 || percent > 100 || percent%10 != 0 {
		return 0, ErrInvalidSpeed
	}
	if distance <= 0 || consumption < 0 || math.IsNaN(consumption) || math.IsInf(consumption, 0) {
		return 0, errors.New("fleet: invalid fuel input")
	}
	factor := math.Pow(float64(percent)/100+1, 2)
	total := 0.0
	for id, quantity := range composition {
		definition, _ := catalogue.Definition(id)
		total += float64(quantity) * float64(definition.FuelConsumption) * float64(distance) / 35000 * factor
	}
	total *= consumption
	if math.IsNaN(total) || math.IsInf(total, 0) || total < 0 || total > math.MaxInt64-1 {
		return 0, errors.New("fleet: fuel overflow")
	}
	return 1 + int64(math.Floor(total)), nil
}

// Capacity is the total cargo hold of the composition, before fuel.
func Capacity(composition Composition, catalogue unit.Catalogue) (int64, error) {
	if err := composition.Validate(catalogue); err != nil {
		return 0, err
	}
	var capacity int64
	for id, quantity := range composition {
		definition, _ := catalogue.Definition(id)
		if definition.Cargo > 0 && quantity > (math.MaxInt64-capacity)/definition.Cargo {
			return 0, errors.New("fleet: capacity overflow")
		}
		capacity += definition.Cargo * quantity
	}
	return capacity, nil
}

// ErrCompositionMismatch reports a composition a mission cannot fly with.
var ErrCompositionMismatch = errors.New("fleet: this composition cannot fly this mission")

// ValidateComposition applies the rules a mission puts on its ships: an
// espionage flies probes only, a recycling needs recyclers, and an attack needs
// something that can actually fight.
func ValidateComposition(mission Mission, composition Composition, catalogue unit.Catalogue) error {
	switch mission {
	case MissionEspionage:
		for id, quantity := range composition {
			if quantity > 0 && id != unit.EspionageProbe {
				return ErrCompositionMismatch
			}
		}
		if composition[unit.EspionageProbe] <= 0 {
			return ErrCompositionMismatch
		}
	case MissionRecycle:
		if composition[unit.Recycler] <= 0 {
			return ErrCompositionMismatch
		}
	case MissionColonize:
		if composition[unit.ColonyShip] <= 0 {
			return ErrCompositionMismatch
		}
	case MissionAttack:
		armed := false
		for id, quantity := range composition {
			if quantity <= 0 {
				continue
			}
			if definition, known := catalogue.Definition(id); known && definition.Weapon > 0 {
				armed = true
				break
			}
		}
		if !armed {
			return ErrCompositionMismatch
		}
	}
	return nil
}
