// Package missile implements one-way missile travel and shield-piercing strikes.
package missile

import (
	"errors"
	"math"
	"time"

	"universeatwar/internal/domain/random"
	"universeatwar/internal/domain/rules"
	"universeatwar/internal/domain/unit"
	"universeatwar/internal/domain/universe"
)

var (
	ErrInvalid = errors.New("missile: invalid strike")
	ErrRange   = errors.New("missile: target outside range")
)

// Targets is the public list of defenses that may be selected as a priority.
func Targets() []unit.ID {
	return []unit.ID{unit.RocketLauncher, unit.LightLaser, unit.HeavyLaser, unit.GaussCannon,
		unit.IonCannon, unit.PlasmaTurret, unit.SmallShieldDome, unit.LargeShieldDome}
}

func ValidPriority(id unit.ID) bool {
	if id == "" {
		return true
	}
	for _, target := range Targets() {
		if id == target {
			return true
		}
	}
	return false
}

func Range(impulse int) int {
	if impulse < 1 {
		return 0
	}
	if impulse > (math.MaxInt-1)/5 {
		return math.MaxInt
	}
	return impulse*5 - 1
}

// Flight uses system distance, never the weighted distance of a fleet.
// Positions within the same system all take thirty seconds at speed one.
func Flight(from, to universe.Coordinate, impulse int, topology rules.TopologySettings, speed float64) (time.Duration, error) {
	limits := universe.Limits{Galaxies: topology.Galaxies, Systems: topology.SystemsPerGalaxy, Positions: topology.PositionsPerSystem}
	if from.Validate(limits) != nil || to.Validate(limits) != nil || speed <= 0 || math.IsNaN(speed) || math.IsInf(speed, 0) {
		return 0, ErrInvalid
	}
	distance := from.System - to.System
	if distance < 0 {
		distance = -distance
	}
	if topology.CircularSystems {
		distance = min(distance, topology.SystemsPerGalaxy-distance)
	}
	if impulse < 1 || from.Galaxy != to.Galaxy || distance > Range(impulse) {
		return 0, ErrRange
	}
	seconds := math.Max(1, math.Floor((30+60*float64(distance))/speed))
	if seconds >= float64(math.MaxInt64/int64(time.Second)) {
		return 0, ErrInvalid
	}
	return time.Duration(seconds) * time.Second, nil
}

type Result struct {
	Intercepted int64
	Losses      unit.Inventory
	Remaining   unit.Inventory
}

// Resolve consumes interceptors first, then attacks hull without shields,
// reconstruction, debris, or the explosion rolls used by fleet battles.
func Resolve(count, interceptors int64, weapons, armour int, priority unit.ID, inventory unit.Inventory, catalogue unit.Catalogue, source random.Source) (Result, error) {
	if count <= 0 || interceptors < 0 || weapons < 0 || armour < 0 || !ValidPriority(priority) || inventory.Validate() != nil || source == nil {
		return Result{}, ErrInvalid
	}
	result := Result{Intercepted: min(count, interceptors), Losses: unit.Inventory{}, Remaining: unit.Inventory{}}
	for _, id := range append(Targets(), unit.InterplanetaryMissile) {
		result.Remaining[id] = inventory[id]
	}
	attack, err := effective(12000, weapons)
	if err != nil || count-result.Intercepted > math.MaxInt64/attack {
		return Result{}, ErrInvalid
	}
	damage := (count - result.Intercepted) * attack
	if damage == 0 {
		return result, nil
	}
	// A stable input and persisted seed produce the same secondary target order.
	order := Targets()
	for i := len(order) - 1; i > 0; i-- {
		j := source.IntN(i + 1)
		order[i], order[j] = order[j], order[i]
	}
	if priority != "" {
		filtered := []unit.ID{priority}
		for _, id := range order {
			if id != priority {
				filtered = append(filtered, id)
			}
		}
		order = filtered
	}
	for _, id := range append(order, unit.InterplanetaryMissile) {
		if id == unit.InterplanetaryMissile {
			for _, defense := range Targets() {
				if result.Remaining[defense] > 0 {
					return result, nil
				}
			}
		}
		definition, ok := catalogue.Definition(id)
		if !ok {
			return Result{}, ErrInvalid
		}
		hull, err := effective(definition.Hull(), armour)
		if err != nil {
			return Result{}, err
		}
		lost := min(result.Remaining[id], damage/hull)
		result.Remaining[id] -= lost
		if lost > 0 {
			result.Losses[id] = lost
		}
		damage -= lost * hull
	}
	return result, nil
}

func effective(base int64, technology int) (int64, error) {
	value := math.Floor(float64(base)*(1+float64(technology)/10) + 0.5)
	if value < 1 || value >= float64(math.MaxInt64) {
		return 0, ErrInvalid
	}
	return int64(value), nil
}
