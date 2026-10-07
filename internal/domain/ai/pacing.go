package ai

import "time"

// BuildingQueueDepth is the modest backlog a character keeps. It uses the
// same queue and pays at order time like a human; the depth only prevents a
// fast universe from leaving a completed site idle until the next strategic
// reflection.
func (p Profile) BuildingQueueDepth() int {
	depth := 1 + int(p.Preferences().Economy*3)
	return min(3, max(2, depth))
}

// ResearchQueueDepth keeps the laboratory moving without planning a whole
// technology era from old information.
func (p Profile) ResearchQueueDepth() int { return 2 }

// YardQueueDepth lets military characters prepare one extra batch while an
// economy-first character keeps more stock available for buildings.
func (p Profile) YardQueueDepth() int {
	if p.Preferences().Economy < .6 {
		return 3
	}
	return 2
}

// ReconnaissanceInterval is the campaign budget of a character. Coverage is
// still tracked per target; this interval only prevents a scout with many
// fleet slots from blanketing the map in one burst.
func (p Profile) ReconnaissanceInterval() time.Duration {
	switch p.Archetype {
	case Scout:
		return 10 * time.Minute
	case Raider:
		return 20 * time.Minute
	case Fleeter, Opportunist:
		return 30 * time.Minute
	case Logistician:
		return 45 * time.Minute
	default:
		return time.Hour
	}
}

// RaidCooldown is how long a character waits before reconsidering a body it
// has just fought over. Once it expires the previous espionage report is
// deliberately invalidated, so another look is required before another raid.
func (p Profile) RaidCooldown() time.Duration {
	switch p.Archetype {
	case Raider:
		return 30 * time.Minute
	case Fleeter, Opportunist:
		return time.Hour
	case Scout:
		return 90 * time.Minute
	default:
		return 2 * time.Hour
	}
}

// ConcurrentScouts caps reconnaissance below the real fleet-slot limit. The
// normal fleet use case remains the final authority.
func (p Profile) ConcurrentScouts() int {
	switch p.Archetype {
	case Scout:
		return 3
	case Raider, Opportunist:
		return 2
	default:
		return 1
	}
}
