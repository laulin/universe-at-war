// Package expedition holds the rules of what a fleet finds beyond the last
// planet of a system. Every draw comes from a seed the caller supplies, so an
// expedition is replayable from what the database already keeps.
package expedition

import (
	"errors"
	"sort"

	"universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/random"
	"universeatwar/internal/domain/rules"
	"universeatwar/internal/domain/unit"
)

var (
	ErrDisabled   = errors.New("expedition: expeditions are disabled in this universe")
	ErrEmptyTable = errors.New("expedition: the result table is empty")
	ErrNoSlot     = errors.New("expedition: no free expedition slot")
	ErrEmptyFleet = errors.New("expedition: an expedition needs a fleet")
)

// Outcome is what an expedition brings back.
type Outcome string

const (
	Resources Outcome = "resources"
	Nothing   Outcome = "nothing"
	Ships     Outcome = "ships"
	Delay     Outcome = "delay"
	Pirates   Outcome = "pirates"
	Aliens    Outcome = "aliens"
	Losses    Outcome = "losses"
	Rare      Outcome = "rare"
)

// Valid reports whether the outcome is one this build knows.
func (o Outcome) Valid() bool {
	switch o {
	case Resources, Nothing, Ships, Delay, Pirates, Aliens, Losses, Rare:
		return true
	default:
		return false
	}
}

// Fights reports whether the outcome is settled by a battle.
func (o Outcome) Fights() bool {
	return o == Pirates || o == Aliens
}

// weighted is one row of the result table, kept in a stable order so the same
// draw always lands on the same outcome.
type weighted struct {
	outcome Outcome
	weight  int
}

func table(weights rules.ExpeditionOutcomes) []weighted {
	return []weighted{
		{Resources, weights.Resources},
		{Nothing, weights.Nothing},
		{Ships, weights.Ships},
		{Delay, weights.Delay},
		{Pirates, weights.Pirates},
		{Aliens, weights.Aliens},
		{Losses, weights.Losses},
		{Rare, weights.Rare},
	}
}

// Draw picks one outcome out of the configured table. A weight of zero simply
// removes an outcome from the table.
func Draw(settings rules.ExpeditionSettings, source random.Source) (Outcome, error) {
	total := settings.Weights.Total()
	if total <= 0 {
		return "", ErrEmptyTable
	}
	roll := source.IntN(total)
	for _, row := range table(settings.Weights) {
		if row.weight <= 0 {
			continue
		}
		if roll < row.weight {
			return row.outcome, nil
		}
		roll -= row.weight
	}
	return Nothing, nil
}

// Find is what an expedition brings home in resources: half metal, a third
// crystal, a sixth deuterium, and never more than the hold can carry.
func Find(capacity int64, factor float64) economy.Resources {
	if capacity <= 0 || factor <= 0 {
		return economy.Resources{}
	}
	total := int64(float64(capacity) * factor)
	if total > capacity {
		total = capacity
	}
	found := economy.Resources{
		Metal:     total / 2,
		Crystal:   total / 3,
		Deuterium: total / 6,
	}
	return found
}

// Salvage is how many small cargos an expedition brings back. A find of ships
// is bounded by the hold that went out, so it never becomes a shipyard.
func Salvage(capacity int64, factor float64, cost economy.Resources) int64 {
	unitCost := cost.Metal + cost.Crystal + cost.Deuterium
	if capacity <= 0 || factor <= 0 || unitCost <= 0 {
		return 0
	}
	found := int64(float64(capacity) * factor / float64(unitCost))
	if found < 1 {
		return 1
	}
	return found
}

// Lose takes a share out of every kind of ship, rounded down, and never the
// whole fleet: an expedition that goes wrong still limps home.
func Lose(composition map[unit.ID]int64, share float64) map[unit.ID]int64 {
	losses := map[unit.ID]int64{}
	if share <= 0 {
		return losses
	}
	var remaining int64
	for _, quantity := range composition {
		remaining += quantity
	}
	for _, id := range sortedUnits(composition) {
		quantity := composition[id]
		lost := int64(float64(quantity) * share)
		if lost <= 0 {
			continue
		}
		if remaining-lost < 1 {
			lost = remaining - 1
		}
		if lost <= 0 {
			continue
		}
		losses[id] = lost
		remaining -= lost
	}
	return losses
}

// Ambush builds the fleet an expedition runs into. It scales with what came
// looking, so a small fleet meets a small danger and a large one a large.
func Ambush(strength int64, kind Outcome, source random.Source) map[unit.ID]int64 {
	if strength <= 0 || !kind.Fights() {
		return nil
	}
	// Aliens are the harder meeting of the two.
	share := .35
	if kind == Aliens {
		share = .75
	}
	// A quarter of leeway either way keeps two identical fleets from meeting
	// exactly the same enemy.
	spread := .75 + source.Float64()*.5
	budget := int64(float64(strength) * share * spread)
	if budget <= 0 {
		return nil
	}
	// A light fighter is the yardstick of the pirate fleet.
	fighters := budget / lightFighterStrength
	if fighters < 1 {
		fighters = 1
	}
	ambush := map[unit.ID]int64{unit.LightFighter: fighters}
	if kind == Aliens && fighters >= 4 {
		// Aliens field cruisers, which is what makes them dreaded.
		cruisers := fighters / 4
		ambush[unit.Cruiser] = cruisers
		ambush[unit.LightFighter] = fighters - cruisers
	}
	return ambush
}

// lightFighterStrength is the weapon, shield and hull of one light fighter, the
// unit the ambush is measured in.
const lightFighterStrength = 50 + 10 + 400

func sortedUnits(composition map[unit.ID]int64) []unit.ID {
	identifiers := make([]unit.ID, 0, len(composition))
	for id := range composition {
		identifiers = append(identifiers, id)
	}
	sort.Slice(identifiers, func(first, second int) bool {
		return identifiers[first] < identifiers[second]
	})
	return identifiers
}
