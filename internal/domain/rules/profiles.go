package rules

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// ErrUnknownProfile is returned when a universe asks for a profile this build
// does not carry.
var ErrUnknownProfile = errors.New("rules: unknown profile")

// Profile is a named starting point for a universe: a complete ruleset with a
// short account of what it changes and who it is for.
type Profile struct {
	ID          string
	Name        string
	Description string
	Rules       Ruleset
}

// Profiles lists every profile this build carries, in a stable order.
func Profiles() []Profile {
	classic := Default()

	slow := Default()
	slow.Time.EconomySpeed = .5
	slow.Time.BuildingSpeed = .5
	slow.Time.ResearchSpeed = .5
	slow.Time.ShipyardSpeed = .5
	slow.Time.DefenseSpeed = .5
	slow.Time.PeacefulFleetSpeed = .5
	slow.Time.HostileFleetSpeed = .5
	slow.Time.HoldingFleetSpeed = .5
	slow.Time.ExpeditionSpeed = .5

	fast := Default()
	fast.Time.EconomySpeed = 5
	fast.Time.BuildingSpeed = 5
	fast.Time.ResearchSpeed = 5
	fast.Time.ShipyardSpeed = 5
	fast.Time.DefenseSpeed = 5
	fast.Time.PeacefulFleetSpeed = 3
	fast.Time.HostileFleetSpeed = 3
	fast.Time.HoldingFleetSpeed = 3
	fast.Time.ExpeditionSpeed = 3

	conflict := Default()
	conflict.Time.HostileFleetSpeed = 2
	conflict.Economy.PillageRatio = .75
	conflict.Combat.MaximumPillage = .75
	conflict.Combat.DefenseRebuildChance = .3
	conflict.Protection.BeginnerProtectionPoints = 1000
	conflict.Team.MaximumAllianceSize = 10

	cooperative := Default()
	cooperative.Economy.PillageRatio = .25
	cooperative.Combat.MaximumPillage = .25
	cooperative.Combat.DefenseRebuildChance = .9
	cooperative.Protection.BeginnerProtectionPoints = 20000
	cooperative.Team.MaximumAllianceSize = 40
	cooperative.Team.StartMode = "predefined_teams"

	pvpve := Default()
	pvpve.Team.StartMode = "pvpve"
	pvpve.AI.Total = 12
	pvpve.AI.IndependentCount = 6
	pvpve.AI.AllianceCount = 2
	pvpve.AI.AllianceSize = 3
	pvpve.Expedition.Weights.Pirates = 20
	pvpve.Expedition.Weights.Aliens = 12
	pvpve.Expedition.Weights.Nothing = 15

	return []Profile{
		{ID: "classic", Name: "Proche classique", Rules: classic,
			Description: "Les réglages de référence, au plus près du jeu d'origine."},
		{ID: "slow", Name: "Lent", Rules: slow,
			Description: "Tout prend deux fois plus de temps : une partie de fond."},
		{ID: "fast", Name: "Accéléré", Rules: fast,
			Description: "Production et files cinq fois plus rapides, voyages trois fois."},
		{ID: "conflict", Name: "Conflit", Rules: conflict,
			Description: "Pillage élevé, défenses fragiles, protection courte : on se bat."},
		{ID: "cooperative", Name: "Coopératif", Rules: cooperative,
			Description: "Pillage faible, défenses solides, grandes alliances : on construit."},
		{ID: "pvpve", Name: "PvPvE", Rules: pvpve,
			Description: "Beaucoup d'intelligences artificielles et des expéditions dangereuses."},
	}
}

// ProfileByID returns one profile by its stable identifier.
func ProfileByID(id string) (Profile, error) {
	for _, profile := range Profiles() {
		if profile.ID == id {
			return profile, nil
		}
	}
	return Profile{}, fmt.Errorf("%w: %q", ErrUnknownProfile, id)
}

// Difference is one setting that two rulesets disagree on.
type Difference struct {
	Path   string
	Before string
	After  string
}

// Compare lists every setting two rulesets disagree on, by its JSON path, in a
// stable order. It is what an administrator reads before activating anything.
func Compare(before, after Ruleset) ([]Difference, error) {
	first, err := flatten(before)
	if err != nil {
		return nil, err
	}
	second, err := flatten(after)
	if err != nil {
		return nil, err
	}
	paths := map[string]bool{}
	for path := range first {
		paths[path] = true
	}
	for path := range second {
		paths[path] = true
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	var differences []Difference
	for _, path := range ordered {
		if first[path] == second[path] {
			continue
		}
		differences = append(differences, Difference{Path: path, Before: first[path], After: second[path]})
	}
	return differences, nil
}

// flatten turns a ruleset into one line per setting, keyed by its JSON path.
func flatten(configured Ruleset) (map[string]string, error) {
	document, err := Encode(configured)
	if err != nil {
		return nil, err
	}
	var generic map[string]any
	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.UseNumber()
	if err := decoder.Decode(&generic); err != nil {
		return nil, fmt.Errorf("rules: flatten: %w", err)
	}
	flat := map[string]string{}
	walk("", generic, flat)
	return flat, nil
}

func walk(prefix string, value any, flat map[string]string) {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			walk(path, nested, flat)
		}
	default:
		flat[prefix] = fmt.Sprint(value)
	}
}
