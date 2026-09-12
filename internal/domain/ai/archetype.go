// Package ai holds the rules a server-driven player follows: when it thinks,
// when it sleeps, what it prefers and how it scores what it knows. It knows
// nothing of storage, of HTTP or of the truth about other players.
package ai

import (
	"errors"
	"math"
)

var (
	ErrUnknownArchetype = errors.New("ai: unknown archetype")
	ErrInvalidWindow    = errors.New("ai: activity hours must fall between 0 and 23")
	ErrInvalidInterval  = errors.New("ai: the thinking interval must be positive")
	ErrInvalidTuning    = errors.New("ai: invalid custom tuning")
)

// Archetype is a set of preferences, never a script. Two archetypes facing the
// same universe reach different conclusions.
type Archetype string

const (
	CautiousMiner Archetype = "cautious_miner"
	Raider        Archetype = "raider"
	Fleeter       Archetype = "fleeter"
	Turtle        Archetype = "turtle"
	Opportunist   Archetype = "opportunist"
	Scout         Archetype = "scout"
	Logistician   Archetype = "logistician"
	Defender      Archetype = "defender"
)

// Fleetsave says when an archetype puts its fleet out of reach before sleeping.
type Fleetsave string

const (
	SaveAlways Fleetsave = "always"
	SaveLoaded Fleetsave = "loaded"
	SaveNever  Fleetsave = "never"
)

// Preferences are the weights that turn a shared set of rules into a character.
type Preferences struct {
	// Economy is the share of thinking spent on development rather than war.
	Economy float64 `json:"economy"`
	// Greed multiplies the plunder an archetype believes it can take.
	Greed float64 `json:"greed"`
	// Caution multiplies the defensive strength it fears.
	Caution float64 `json:"caution"`
	// SafetyMargin is how many times stronger it wants to be before striking.
	SafetyMargin float64 `json:"safety_margin"`
	// Probes is how many probes it sends to look at a target.
	Probes int64 `json:"probes"`
	// DefenceShare is the part of its production reserved for defences.
	DefenceShare float64 `json:"defence_share"`
	// RaidThreshold is the score below which a target is not worth the trip.
	RaidThreshold float64 `json:"raid_threshold"`
	// Fleetsave says whether it hides its fleet before sleeping.
	Fleetsave Fleetsave `json:"fleetsave"`
}

// Tuning is the complete, individually configurable behaviour of an
// artificial player. Preferences shape its judgement while the remaining
// switches shape what it is willing to do and how broadly it looks.
type Tuning struct {
	Preferences      `json:"preferences"`
	AttackEnabled    bool  `json:"attack_enabled"`
	EspionageEnabled bool  `json:"espionage_enabled"`
	RecycleEnabled   bool  `json:"recycle_enabled"`
	SearchRadius     int   `json:"search_radius"`
	BatchSize        int64 `json:"batch_size"`
}

var preferences = map[Archetype]Preferences{
	CautiousMiner: {Economy: .90, Greed: .30, Caution: 1.50, SafetyMargin: 4, Probes: 3, DefenceShare: .25, RaidThreshold: 12000, Fleetsave: SaveAlways},
	Raider:        {Economy: .40, Greed: 1.20, Caution: .60, SafetyMargin: 1.5, Probes: 2, DefenceShare: .05, RaidThreshold: 1500, Fleetsave: SaveLoaded},
	Fleeter:       {Economy: .50, Greed: .80, Caution: .80, SafetyMargin: 2, Probes: 3, DefenceShare: .10, RaidThreshold: 3000, Fleetsave: SaveAlways},
	Turtle:        {Economy: .70, Greed: .20, Caution: 2.00, SafetyMargin: 6, Probes: 2, DefenceShare: .45, RaidThreshold: 20000, Fleetsave: SaveNever},
	Opportunist:   {Economy: .55, Greed: 1.00, Caution: .90, SafetyMargin: 2, Probes: 3, DefenceShare: .10, RaidThreshold: 2500, Fleetsave: SaveLoaded},
	Scout:         {Economy: .60, Greed: .50, Caution: 1.20, SafetyMargin: 3, Probes: 5, DefenceShare: .10, RaidThreshold: 6000, Fleetsave: SaveAlways},
	Logistician:   {Economy: .80, Greed: .40, Caution: 1.20, SafetyMargin: 3, Probes: 2, DefenceShare: .20, RaidThreshold: 9000, Fleetsave: SaveAlways},
	Defender:      {Economy: .65, Greed: .30, Caution: 1.60, SafetyMargin: 4, Probes: 2, DefenceShare: .40, RaidThreshold: 15000, Fleetsave: SaveAlways},
}

var tunings = map[Archetype]Tuning{
	CautiousMiner: {Preferences: preferences[CautiousMiner], AttackEnabled: true, EspionageEnabled: true, RecycleEnabled: true, SearchRadius: 3, BatchSize: 10},
	Raider:        {Preferences: preferences[Raider], AttackEnabled: true, EspionageEnabled: true, RecycleEnabled: true, SearchRadius: 10, BatchSize: 25},
	Fleeter:       {Preferences: preferences[Fleeter], AttackEnabled: true, EspionageEnabled: true, RecycleEnabled: true, SearchRadius: 8, BatchSize: 20},
	Turtle:        {Preferences: preferences[Turtle], AttackEnabled: false, EspionageEnabled: true, RecycleEnabled: true, SearchRadius: 2, BatchSize: 15},
	Opportunist:   {Preferences: preferences[Opportunist], AttackEnabled: true, EspionageEnabled: true, RecycleEnabled: true, SearchRadius: 7, BatchSize: 20},
	Scout:         {Preferences: preferences[Scout], AttackEnabled: true, EspionageEnabled: true, RecycleEnabled: true, SearchRadius: 15, BatchSize: 12},
	Logistician:   {Preferences: preferences[Logistician], AttackEnabled: true, EspionageEnabled: true, RecycleEnabled: true, SearchRadius: 5, BatchSize: 18},
	Defender:      {Preferences: preferences[Defender], AttackEnabled: true, EspionageEnabled: true, RecycleEnabled: true, SearchRadius: 4, BatchSize: 15},
}

// Archetypes lists every character this build knows, in a stable order.
func Archetypes() []Archetype {
	return []Archetype{CautiousMiner, Raider, Fleeter, Turtle, Opportunist, Scout, Logistician, Defender}
}

// Valid reports whether the archetype is one this build knows.
func (a Archetype) Valid() bool {
	_, known := preferences[a]
	return known
}

// Preferences returns the weights of an archetype. An unknown archetype has the
// weights of the cautious miner, which never attacks by surprise.
func (a Archetype) Preferences() Preferences {
	if known, ok := preferences[a]; ok {
		return known
	}
	return preferences[CautiousMiner]
}

// Tuning returns the complete default behaviour of an archetype.
func (a Archetype) Tuning() Tuning {
	if known, ok := tunings[a]; ok {
		return known
	}
	return tunings[CautiousMiner]
}

// Validate refuses values that would make decisions degenerate or turn one
// reflection into an unbounded order or map scan.
func (t Tuning) Validate() error {
	finite := func(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }
	if !finite(t.Economy) || t.Economy < 0 || t.Economy > 1 ||
		!finite(t.Greed) || t.Greed < 0 || t.Greed > 10 ||
		!finite(t.Caution) || t.Caution < 0 || t.Caution > 10 ||
		!finite(t.SafetyMargin) || t.SafetyMargin <= 0 || t.SafetyMargin > 100 ||
		t.Probes <= 0 || t.Probes > 100 ||
		!finite(t.DefenceShare) || t.DefenceShare < 0 || t.DefenceShare > 1 ||
		!finite(t.RaidThreshold) || t.RaidThreshold < 0 ||
		(t.Fleetsave != SaveAlways && t.Fleetsave != SaveLoaded && t.Fleetsave != SaveNever) ||
		t.SearchRadius < 1 || t.SearchRadius > 100 || t.BatchSize < 1 || t.BatchSize > 1000 {
		return ErrInvalidTuning
	}
	return nil
}
