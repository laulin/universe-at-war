package ai

import (
	"time"

	"universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/unit"
	"universeatwar/internal/domain/universe"
)

// Knowledge is one thing an alliance believes, and where that belief comes
// from. Nothing enters it that a member did not observe and share on purpose.
type Knowledge struct {
	Kind       KnowledgeKind
	Coordinate universe.Coordinate
	// AuthorID and AuthorName say who saw it: a belief without an author is
	// not a belief, it is a leak.
	AuthorID   int64
	AuthorName string
	ObservedAt time.Time
	ExpiresAt  time.Time
	Confidence float64
	// ReportID ties the belief to the report it came from, so unsharing that
	// report takes the belief away with it.
	ReportID int64
	Plunder  economy.Resources
	Defence  int64
	Complete bool
	Summary  string
	// Capability is filled on a declaration: what the author says it owns.
	Capability Capability
}

// KnowledgeKind is what a shared belief is about.
type KnowledgeKind string

const (
	// TargetKnowledge is a body somebody looked at.
	TargetKnowledge KnowledgeKind = "target"
	// ThreatKnowledge is a member that has just been attacked.
	ThreatKnowledge KnowledgeKind = "threat"
	// DebrisKnowledge is a field the public map shows.
	DebrisKnowledge KnowledgeKind = "debris"
	// CapabilityKnowledge is what a member says it owns.
	CapabilityKnowledge KnowledgeKind = "capability"
)

// Valid reports whether the kind is one this build knows.
func (k KnowledgeKind) Valid() bool {
	switch k {
	case TargetKnowledge, ThreatKnowledge, DebrisKnowledge, CapabilityKnowledge:
		return true
	default:
		return false
	}
}

// Fresh reports whether a belief still means anything at that instant.
func (k Knowledge) Fresh(now time.Time) bool {
	return k.Confidence > 0 && now.Before(k.ExpiresAt)
}

// Believe grades an observation for the alliance: how much it is worth now, and
// how long it will be worth anything at all.
func Believe(observedAt, now time.Time, recent time.Duration) (confidence float64, expiresAt time.Time) {
	return Fresh(observedAt, now, recent), observedAt.Add(recent * StaleFactor)
}

// Capability is what a member tells its allies it can bring. It is a
// declaration, never a reading of somebody else's planet.
type Capability struct {
	PlayerID      int64
	PlayerName    string
	Coordinate    universe.Coordinate
	BodyID        int64
	Awake         bool
	Probes        int64
	Recyclers     int64
	WarStrength   int64
	GroundDefence int64
	Hauling       int64
}

// Assess reads out of an inventory what a member can honestly declare to its
// allies: what it owns, counted with the public catalogue.
func Assess(inventory map[unit.ID]int64, catalogue unit.Catalogue) Capability {
	capability := Capability{
		Probes:    inventory[unit.EspionageProbe],
		Recyclers: inventory[unit.Recycler],
	}
	warships := map[unit.ID]int64{}
	ground := map[unit.ID]int64{}
	for id, quantity := range inventory {
		if quantity <= 0 {
			continue
		}
		definition, known := catalogue.Definition(id)
		if !known {
			continue
		}
		if definition.Family == unit.Defense {
			ground[id] = quantity
			continue
		}
		if ShipRoleOf(definition) == Warship {
			warships[id] = quantity
		}
	}
	capability.WarStrength = Strength(warships, catalogue)
	capability.GroundDefence = Strength(ground, catalogue)
	for id, quantity := range inventory {
		definition, known := catalogue.Definition(id)
		if known && quantity > 0 && ShipRoleOf(definition) == Carrier {
			capability.Hauling += quantity * definition.Cargo
		}
	}
	return capability
}
