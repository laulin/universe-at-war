package ai

import (
	"sort"
	"time"

	"universeatwar/internal/domain/universe"
)

// Role is what an alliance asks of one of its members. A member holds one role
// at a time, and a sleeping member holds none but the plain one.
type Role string

const (
	ScoutRole       Role = "scout"
	FleeterRole     Role = "fleeter"
	RecyclerRole    Role = "recycler"
	DefenderRole    Role = "defender"
	LogisticianRole Role = "logistician"
	MinerRole       Role = "miner"
)

// Valid reports whether the role is one this build knows.
func (r Role) Valid() bool {
	switch r {
	case ScoutRole, FleeterRole, RecyclerRole, DefenderRole, LogisticianRole, MinerRole:
		return true
	default:
		return false
	}
}

// AssignRoles hands out the roles from what the members declared and from
// nothing else. Ties are broken by identifier, so the same declarations always
// give the same team.
func AssignRoles(capabilities []Capability) map[int64]Role {
	roles := make(map[int64]Role, len(capabilities))
	for _, capability := range capabilities {
		roles[capability.PlayerID] = MinerRole
	}
	taken := map[int64]bool{}
	for _, assignment := range []struct {
		role  Role
		score func(Capability) int64
	}{
		{ScoutRole, func(c Capability) int64 { return c.Probes }},
		{FleeterRole, func(c Capability) int64 { return c.WarStrength }},
		{RecyclerRole, func(c Capability) int64 { return c.Recyclers }},
		{DefenderRole, func(c Capability) int64 { return c.GroundDefence }},
		{LogisticianRole, func(c Capability) int64 { return c.Hauling }},
	} {
		best, found := bestMember(capabilities, taken, assignment.score)
		if !found {
			continue
		}
		roles[best] = assignment.role
		taken[best] = true
	}
	return roles
}

// bestMember picks the awake member with the most of something, ignoring those
// that already carry a role and those that have none of it at all.
func bestMember(capabilities []Capability, taken map[int64]bool, score func(Capability) int64) (int64, bool) {
	ordered := make([]Capability, len(capabilities))
	copy(ordered, capabilities)
	sort.SliceStable(ordered, func(first, second int) bool {
		return ordered[first].PlayerID < ordered[second].PlayerID
	})
	var best int64
	var value int64
	for _, capability := range ordered {
		if taken[capability.PlayerID] || !capability.Awake {
			continue
		}
		if candidate := score(capability); candidate > value {
			best, value = capability.PlayerID, candidate
		}
	}
	return best, best != 0
}

// ObjectiveKind is what an alliance is trying to do.
type ObjectiveKind string

const (
	RaidObjective    ObjectiveKind = "raid"
	DefenceObjective ObjectiveKind = "defence"
)

// ObjectiveState is where a collective plan has got to.
type ObjectiveState string

const (
	Scouting   ObjectiveState = "scouting"
	Assembling ObjectiveState = "assembling"
	Achieved   ObjectiveState = "resolved"
	Abandoned  ObjectiveState = "abandoned"
)

// Open reports whether the objective still asks anything of the alliance.
func (s ObjectiveState) Open() bool {
	return s == Scouting || s == Assembling
}

// CanAdvance reports whether a plan may move from one state to another.
func CanAdvance(from, to ObjectiveState) bool {
	switch from {
	case Scouting:
		return to == Assembling || to == Abandoned
	case Assembling:
		return to == Achieved || to == Abandoned
	default:
		return false
	}
}

// Objective is the single plan an alliance pursues at a time.
type Objective struct {
	ID         int64
	Kind       ObjectiveKind
	Coordinate universe.Coordinate
	State      ObjectiveState
	GroupID    int64
	Quorum     int
	OpenedAt   time.Time
	DeadlineAt time.Time
	Reason     string
}

// Quorum is how many fleets an alliance wants before it commits to a strike.
func Quorum(awake int) int {
	quorum := awake / 2
	if quorum < 2 {
		quorum = 2
	}
	return quorum
}

// ScoreBelief grades a shared belief for the character of the leader. Second
// hand knowledge is worth its confidence, no more.
func ScoreBelief(belief Knowledge, preferences Preferences) float64 {
	plunder := belief.Plunder.Metal + belief.Plunder.Crystal + belief.Plunder.Deuterium
	return float64(plunder)*belief.Confidence*preferences.Greed -
		float64(belief.Defence)*preferences.Caution
}

// ChooseObjective reads the common memory and says what the alliance should do
// next: come to the rescue of a member first, then strike what it knows best.
// A target nobody has looked at properly is still worth scouting.
func ChooseObjective(beliefs []Knowledge, preferences Preferences, now time.Time) (ObjectiveKind, universe.Coordinate, bool) {
	var threat Knowledge
	for _, belief := range beliefs {
		if belief.Kind != ThreatKnowledge || !belief.Fresh(now) {
			continue
		}
		if threat.AuthorID == 0 || belief.ObservedAt.After(threat.ObservedAt) {
			threat = belief
		}
	}
	if threat.AuthorID != 0 {
		return DefenceObjective, threat.Coordinate, true
	}
	var best Knowledge
	var bestScore float64
	for _, belief := range beliefs {
		if belief.Kind != TargetKnowledge || !belief.Fresh(now) {
			continue
		}
		score := ScoreBelief(belief, preferences)
		if best.AuthorID == 0 || score > bestScore {
			best, bestScore = belief, score
		}
	}
	if best.AuthorID == 0 || bestScore < preferences.RaidThreshold {
		return "", universe.Coordinate{}, false
	}
	return RaidObjective, best.Coordinate, true
}
