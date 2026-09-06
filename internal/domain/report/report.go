// Package report holds the immutable narrative of the game. A payload is
// filtered when it is created: an unauthorised section is never stored, so it
// can never leak later.
package report

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/universe"
)

// CurrentPayloadVersion is the layout this build writes.
const CurrentPayloadVersion = 1

// ErrUnsupportedPayload reports a payload this build cannot read.
var ErrUnsupportedPayload = errors.New("report: unsupported payload version")

// Kind is the stable persisted identifier of a report type.
type Kind string

const (
	Espionage         Kind = "espionage"
	EspionageDetected Kind = "espionage_detected"
	CombatAttack      Kind = "combat_attack"
	CombatDefense     Kind = "combat_defense"
	Recycling         Kind = "recycling"
)

// Hostile reports whether a report tells its recipient they are under threat.
func (k Kind) Hostile() bool {
	return k == CombatDefense || k == EspionageDetected
}

// Valid reports whether the kind is one this build knows.
func (k Kind) Valid() bool {
	switch k {
	case Espionage, EspionageDetected, CombatAttack, CombatDefense, Recycling:
		return true
	default:
		return false
	}
}

// Freshness tells how much a piece of intelligence can still be trusted.
type Freshness string

const (
	Recent  Freshness = "recent"
	Old     Freshness = "old"
	Unknown Freshness = "unknown"
)

// FreshnessOf classifies a report by its age.
func FreshnessOf(occurredAt, now time.Time, recent time.Duration) Freshness {
	if occurredAt.IsZero() {
		return Unknown
	}
	if now.Sub(occurredAt) <= recent {
		return Recent
	}
	return Old
}

// EspionagePayload is what a spy brought back. A nil section was not revealed
// and is absent from the stored document.
type EspionagePayload struct {
	Target           universe.Coordinate `json:"target"`
	TargetPlayerName string              `json:"target_player_name"`
	TargetPlanetName string              `json:"target_planet_name"`
	Probes           int64               `json:"probes"`
	Level            int                 `json:"level"`
	ProbesLost       bool                `json:"probes_lost"`
	Resources        *economy.Resources  `json:"resources,omitempty"`
	Fleet            map[string]int64    `json:"fleet,omitempty"`
	Defenses         map[string]int64    `json:"defenses,omitempty"`
	Buildings        map[string]int      `json:"buildings,omitempty"`
	Research         map[string]int      `json:"research,omitempty"`
}

// DetectedPayload is what the target of an espionage learns: who came, never
// what they saw.
type DetectedPayload struct {
	AttackerPlayerName string              `json:"attacker_player_name"`
	Origin             universe.Coordinate `json:"origin"`
	Probes             int64               `json:"probes"`
	ProbesDestroyed    bool                `json:"probes_destroyed"`
	TargetPlanetName   string              `json:"target_planet_name"`
}

// Participant is one side of a battle as every witness saw it.
type Participant struct {
	PlayerName string           `json:"player_name"`
	Weapons    int              `json:"weapons"`
	Shielding  int              `json:"shielding"`
	Armour     int              `json:"armour"`
	Initial    map[string]int64 `json:"initial"`
	Survivors  map[string]int64 `json:"survivors"`
	Losses     map[string]int64 `json:"losses"`
}

// RoundSummary is the public account of one round.
type RoundSummary struct {
	Number           int   `json:"number"`
	AttackerShots    int64 `json:"attacker_shots"`
	DefenderShots    int64 `json:"defender_shots"`
	AttackerDamage   int64 `json:"attacker_damage"`
	DefenderDamage   int64 `json:"defender_damage"`
	AttackerAbsorbed int64 `json:"attacker_absorbed"`
	DefenderAbsorbed int64 `json:"defender_absorbed"`
}

// CombatPayload is the battle as one recipient may know it. Everything a
// participant could observe during the fight is shared; the rebuilt defenses
// are the defender's own business, and the seed never appears.
type CombatPayload struct {
	Coordinate universe.Coordinate `json:"coordinate"`
	Outcome    string              `json:"outcome"`
	Attackers  []Participant       `json:"attackers"`
	Defenders  []Participant       `json:"defenders"`
	Rounds     []RoundSummary      `json:"rounds"`
	Loot       economy.Resources   `json:"loot"`
	Debris     economy.Resources   `json:"debris"`
	MoonChance float64             `json:"moon_chance"`
	Rebuilt    map[string]int64    `json:"rebuilt,omitempty"`
}

// RecyclingPayload is what a recycler brought home.
type RecyclingPayload struct {
	Position  universe.Coordinate `json:"position"`
	Recyclers int64               `json:"recyclers"`
	Capacity  int64               `json:"capacity"`
	Collected economy.Resources   `json:"collected"`
	Remaining economy.Resources   `json:"remaining"`
}

// Marshal encodes a payload with the version this build writes.
func Marshal(kind Kind, payload any) (int, []byte, error) {
	if !kind.Valid() {
		return 0, nil, fmt.Errorf("report: unknown kind %q", kind)
	}
	if err := matches(kind, payload); err != nil {
		return 0, nil, err
	}
	document, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, fmt.Errorf("report: encode %s: %w", kind, err)
	}
	return CurrentPayloadVersion, document, nil
}

// Unmarshal decodes a stored payload, refusing a version it cannot read.
func Unmarshal(kind Kind, version int, document []byte) (any, error) {
	if version != CurrentPayloadVersion {
		return nil, fmt.Errorf("%w: %d", ErrUnsupportedPayload, version)
	}
	switch kind {
	case Espionage:
		return decode[EspionagePayload](document)
	case EspionageDetected:
		return decode[DetectedPayload](document)
	case CombatAttack, CombatDefense:
		return decode[CombatPayload](document)
	case Recycling:
		return decode[RecyclingPayload](document)
	default:
		return nil, fmt.Errorf("report: unknown kind %q", kind)
	}
}

func decode[T any](document []byte) (any, error) {
	var payload T
	if err := json.Unmarshal(document, &payload); err != nil {
		return nil, fmt.Errorf("report: decode: %w", err)
	}
	return payload, nil
}

// matches refuses a payload that does not belong to its kind, which would make
// a report unreadable once stored.
func matches(kind Kind, payload any) error {
	valid := false
	switch payload.(type) {
	case EspionagePayload:
		valid = kind == Espionage
	case DetectedPayload:
		valid = kind == EspionageDetected
	case CombatPayload:
		valid = kind == CombatAttack || kind == CombatDefense
	case RecyclingPayload:
		valid = kind == Recycling
	}
	if !valid {
		return fmt.Errorf("report: payload does not match kind %q", kind)
	}
	return nil
}
