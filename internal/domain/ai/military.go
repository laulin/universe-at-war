package ai

import (
	"sort"
	"time"

	"universeatwar/internal/domain/economy"
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

// Role is what an artificial player expects a ship to do in a raid. A ship
// fights when its guns weigh more than its hold, and hauls otherwise.
type Role string

const (
	Warship Role = "warship"
	Carrier Role = "carrier"
	Idle    Role = "idle"
)

// RoleOf reads the role of a ship out of the catalogue alone.
func RoleOf(definition unit.Definition) Role {
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
	var total int64
	for id, quantity := range units {
		if quantity <= 0 {
			continue
		}
		definition, known := catalogue.Definition(id)
		if !known {
			continue
		}
		total += quantity * (definition.Weapon + definition.Shield + definition.Hull())
	}
	return total
}

// ScoreTarget grades what a report is worth to an archetype today.
func ScoreTarget(intel Intel, now time.Time, recent time.Duration, preferences Preferences) Target {
	freshness := Fresh(intel.ObservedAt, now, recent)
	plunder := intel.Plunder.Metal + intel.Plunder.Crystal + intel.Plunder.Deuterium
	score := float64(plunder)*freshness*preferences.Greed -
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

// ComposeRaid builds the fleet an artificial player sends: everything that can
// fight, and just enough holds to carry what it hopes to take. It refuses to
// send anything it does not believe strong enough.
func ComposeRaid(inventory map[unit.ID]int64, catalogue unit.Catalogue,
	expected economy.Resources, defence int64, preferences Preferences) (map[unit.ID]int64, bool) {
	composition := map[unit.ID]int64{}
	var carriers []unit.ID
	for _, id := range sortedUnits(inventory) {
		quantity := inventory[id]
		if quantity <= 0 {
			continue
		}
		definition, known := catalogue.Definition(id)
		if !known {
			continue
		}
		switch RoleOf(definition) {
		case Warship:
			composition[id] = quantity
		case Carrier:
			carriers = append(carriers, id)
		}
	}
	if len(composition) == 0 {
		return nil, false
	}
	if float64(Strength(composition, catalogue)) < float64(defence)*preferences.SafetyMargin {
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
