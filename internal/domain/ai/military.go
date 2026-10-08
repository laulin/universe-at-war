package ai

import (
	"math"
	"sort"
	"time"

	"universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/random"
	"universeatwar/internal/domain/unit"
	"universeatwar/internal/domain/universe"
)

// Intel is what one espionage report told an artificial player about a body.
// Nothing else ever feeds it: no report, no target.
type Intel struct {
	Coordinate universe.Coordinate
	ReportID   int64
	OwnerName  string
	ObservedAt time.Time
	// Plunder is what the report says could be carried away.
	Plunder economy.Resources
	// Debris is the upper-bound wreckage value of the ships and defenses the
	// report revealed. It is an estimate, not knowledge of a future battle.
	Debris economy.Resources
	// Defence is the strength of the fleet and the turrets that were seen.
	Defence int64
	// Complete says whether the report revealed both the fleet and the
	// defences. A partial report never justifies an attack.
	Complete bool
	Distance int64
}

// Target is one candidate, graded by what an archetype makes of it.
type Target struct {
	Intel
	Freshness float64
	Score     float64
}

// MinimumHold is the smallest hold worth taking along for the loot: below it a
// ship would die for a handful of metal.
const MinimumHold = 1000

// ShipRole is what an artificial player expects a ship to do in a raid. A ship
// fights when its guns weigh more than its hold, and hauls otherwise.
type ShipRole string

const (
	Warship ShipRole = "warship"
	Carrier ShipRole = "carrier"
	Idle    ShipRole = "idle"
)

// ShipRoleOf reads the role of a ship out of the catalogue alone.
func ShipRoleOf(definition unit.Definition) ShipRole {
	if definition.Family != unit.Ship || definition.BaseSpeed <= 0 {
		return Idle
	}
	if definition.Weapon > 0 && definition.Weapon*100 >= definition.Cargo {
		return Warship
	}
	if definition.Cargo >= MinimumHold {
		return Carrier
	}
	return Idle
}

// Strength grades a pile of units by what it brings to a battle. It is a rough
// measure, and it is meant to be: an artificial player estimates, it does not
// resolve the fight in advance.
func Strength(units map[unit.ID]int64, catalogue unit.Catalogue) int64 {
	return catalogue.Strength(units)
}

// ScoreTarget grades what a report is worth to an archetype today.
func ScoreTarget(intel Intel, now time.Time, recent time.Duration, preferences Preferences) Target {
	freshness := Fresh(intel.ObservedAt, now, recent)
	plunder := intel.Plunder.Metal + intel.Plunder.Crystal + intel.Plunder.Deuterium
	debris := intel.Debris.Metal + intel.Debris.Crystal
	score := float64(plunder+debris)*freshness*preferences.Greed -
		float64(intel.Defence)*preferences.Caution -
		float64(intel.Distance)
	return Target{Intel: intel, Freshness: freshness, Score: score}
}

// BestTarget picks the target worth the trip: a complete report, still worth
// something, and a score above what the archetype demands. It also reports the
// best target that failed only for lack of a fresh look, which is what an
// artificial player goes and spies on.
func BestTarget(targets []Target, preferences Preferences) (best Target, stale Target, found bool) {
	sorted := make([]Target, len(targets))
	copy(sorted, targets)
	sort.SliceStable(sorted, func(first, second int) bool {
		if sorted[first].Score != sorted[second].Score {
			return sorted[first].Score > sorted[second].Score
		}
		return sorted[first].ReportID < sorted[second].ReportID
	})
	for _, target := range sorted {
		if target.Freshness <= 0 || !target.Complete {
			if stale.ReportID == 0 {
				stale = target
			}
			continue
		}
		if target.Score < preferences.RaidThreshold {
			continue
		}
		return target, stale, true
	}
	return Target{}, stale, false
}

// ComposeRaid builds the force an artificial player intends without a sizing
// mistake. Runtime players use ComposeRaidSized with their competence draw.
func ComposeRaid(inventory map[unit.ID]int64, catalogue unit.Catalogue,
	expected economy.Resources, defence int64, preferences Preferences) (map[unit.ID]int64, bool) {
	return ComposeRaidSized(inventory, catalogue, expected, defence, preferences, 1)
}

// RaidSizingFactor is the error made while translating an observed defence
// into a number of ships. The caller supplies a seeded source, so even a bad
// beginner calculation remains deterministic and replayable.
func RaidSizingFactor(difficulty Difficulty, source random.Source) float64 {
	if source == nil {
		return 1
	}
	minimum, maximum := .45, 1.15
	switch difficulty {
	case Easy:
		minimum, maximum = .15, 1.15
	case Hard:
		minimum, maximum = .90, 1.05
	}
	return minimum + source.Float64()*(maximum-minimum)
}

// ComposeRaidSized sends only the force the player believes necessary. A low
// sizing factor can therefore make an aggressive beginner attack with less
// strength than the report actually showed. Combat still resolves the real
// fleets and punishes the mistake normally.
func ComposeRaidSized(inventory map[unit.ID]int64, catalogue unit.Catalogue,
	expected economy.Resources, defence int64, preferences Preferences,
	sizingFactor float64) (map[unit.ID]int64, bool) {
	if sizingFactor <= 0 || math.IsNaN(sizingFactor) || math.IsInf(sizingFactor, 0) {
		sizingFactor = 1
	}
	composition := map[unit.ID]int64{}
	var carriers []unit.ID
	var warships []unit.ID
	for _, id := range sortedUnits(inventory) {
		quantity := inventory[id]
		if quantity <= 0 {
			continue
		}
		definition, known := catalogue.Definition(id)
		if !known {
			continue
		}
		switch ShipRoleOf(definition) {
		case Warship:
			warships = append(warships, id)
		case Carrier:
			carriers = append(carriers, id)
		}
	}
	if len(warships) == 0 {
		return nil, false
	}
	required := float64(max(0, defence)) * preferences.SafetyMargin * sizingFactor
	if required < 1 {
		required = 1
	}
	var committed int64
	for _, id := range warships {
		definition, _ := catalogue.Definition(id)
		one := definition.Weapon + definition.Shield + definition.Hull()
		if one <= 0 {
			continue
		}
		needed := int64(math.Ceil((required - float64(committed)) / float64(one)))
		if needed < 1 {
			break
		}
		if needed > inventory[id] {
			needed = inventory[id]
		}
		composition[id] = needed
		committed += needed * one
		if float64(committed) >= required {
			break
		}
	}
	if len(composition) == 0 || float64(committed) < required {
		return nil, false
	}
	// Add holds until the expected haul fits, without emptying the yard of
	// what it does not need.
	wanted := expected.Metal + expected.Crystal + expected.Deuterium
	carried := capacityOf(composition, catalogue)
	for _, id := range carriers {
		if carried >= wanted {
			break
		}
		definition, _ := catalogue.Definition(id)
		if definition.Cargo <= 0 {
			continue
		}
		missing := (wanted - carried + definition.Cargo - 1) / definition.Cargo
		if missing > inventory[id] {
			missing = inventory[id]
		}
		if missing <= 0 {
			continue
		}
		composition[id] = missing
		carried += missing * definition.Cargo
	}
	return composition, true
}

// ComposeFleetsave takes everything that can fly out of reach before the night.
func ComposeFleetsave(inventory map[unit.ID]int64, catalogue unit.Catalogue) map[unit.ID]int64 {
	composition := map[unit.ID]int64{}
	for _, id := range sortedUnits(inventory) {
		quantity := inventory[id]
		if quantity <= 0 {
			continue
		}
		definition, known := catalogue.Definition(id)
		if !known || definition.Family != unit.Ship || definition.BaseSpeed <= 0 {
			continue
		}
		composition[id] = quantity
	}
	if len(composition) == 0 {
		return nil
	}
	return composition
}

// FleetsaveCargo fills the available holds in a stable resource order while
// leaving part of the deuterium on the body for the launch fuel. The fleet
// service performs the authoritative capacity and fuel validation afterwards.
func FleetsaveCargo(stock economy.Resources, composition map[unit.ID]int64,
	catalogue unit.Catalogue) economy.Resources {
	return FleetsaveCargoUpTo(stock, capacityOf(composition, catalogue))
}

// FleetsaveCargoUpTo fills the capacity left after the fleet planner accounted
// for fuel. It is separate so the AI can preview a real route before loading.
func FleetsaveCargoUpTo(stock economy.Resources, capacity int64) economy.Resources {
	if capacity <= 0 {
		return economy.Resources{}
	}
	load := func(available int64) int64 {
		if available < 0 {
			return 0
		}
		if available > capacity {
			available = capacity
		}
		capacity -= available
		return available
	}
	cargo := economy.Resources{}
	cargo.Metal = load(stock.Metal)
	cargo.Crystal = load(stock.Crystal)
	// Twenty per cent stays available for fuel and the first decisions after
	// waking. This is intentionally conservative; PlanLaunch still decides
	// whether the trip is affordable.
	cargo.Deuterium = load(stock.Deuterium * 4 / 5)
	return cargo
}

// LastBefore reports whether a reflection is the last one before the night: the
// following one would already fall outside the window.
func (w Window) LastBefore(now time.Time, interval time.Duration) bool {
	if w.Always() || !w.Valid() {
		return false
	}
	return w.Awake(now) && !w.Awake(now.Add(interval))
}

func capacityOf(composition map[unit.ID]int64, catalogue unit.Catalogue) int64 {
	var total int64
	for id, quantity := range composition {
		definition, known := catalogue.Definition(id)
		if !known {
			continue
		}
		total += quantity * definition.Cargo
	}
	return total
}

func sortedUnits(inventory map[unit.ID]int64) []unit.ID {
	identifiers := make([]unit.ID, 0, len(inventory))
	for id := range inventory {
		identifiers = append(identifiers, id)
	}
	sort.Slice(identifiers, func(first, second int) bool {
		return identifiers[first] < identifiers[second]
	})
	return identifiers
}

// ComposeRecycling sends just enough recyclers to lift a debris field, and none
// if the player owns none. A field nobody can reach is left where it lies.
func ComposeRecycling(inventory map[unit.ID]int64, field economy.Resources, capacity int64) (map[unit.ID]int64, bool) {
	owned := inventory[unit.Recycler]
	if owned <= 0 || capacity <= 0 {
		return nil, false
	}
	total := field.Metal + field.Crystal
	if total <= 0 {
		return nil, false
	}
	wanted := (total + capacity - 1) / capacity
	if wanted > owned {
		wanted = owned
	}
	if wanted < 1 {
		wanted = 1
	}
	return map[unit.ID]int64{unit.Recycler: wanted}, true
}
