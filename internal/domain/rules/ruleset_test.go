package rules

import (
	"math"
	"testing"
)

func TestDefaultRulesetIsValid(t *testing.T) {
	ruleset := Default()
	if err := ruleset.Validate(); err != nil {
		t.Fatalf("Default().Validate() error = %v", err)
	}
}

func TestRulesetValidationRejectsImpossibleValues(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Ruleset)
	}{
		{name: "blank name", mutate: func(r *Ruleset) { r.Identity.Name = "" }},
		{name: "unknown timezone", mutate: func(r *Ruleset) { r.Identity.Timezone = "Mars/Olympus" }},
		{name: "zero galaxies", mutate: func(r *Ruleset) { r.Topology.Galaxies = 0 }},
		{name: "invalid planet fields", mutate: func(r *Ruleset) { r.Topology.MinPlanetFields = r.Topology.MaxPlanetFields + 1 }},
		{name: "zero economy speed", mutate: func(r *Ruleset) { r.Time.EconomySpeed = 0 }},
		{name: "NaN economy speed", mutate: func(r *Ruleset) { r.Time.EconomySpeed = math.NaN() }},
		{name: "negative production", mutate: func(r *Ruleset) { r.Economy.BaseMetalPerHour = -1 }},
		{name: "pillage over one", mutate: func(r *Ruleset) { r.Economy.PillageRatio = 1.1 }},
		{name: "moon chance over one", mutate: func(r *Ruleset) { r.Combat.MaximumMoonChance = 1.1 }},
		{name: "ACS without alliances", mutate: func(r *Ruleset) { r.Team.AlliancesEnabled = false; r.Team.ACSEnabled = true }},
		{name: "AI alliance overflow", mutate: func(r *Ruleset) { r.AI.Total = 1; r.AI.AllianceCount = 1; r.AI.AllianceSize = 2 }},
		{name: "invalid registration", mutate: func(r *Ruleset) { r.Identity.RegistrationPolicy = "everyone" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ruleset := Default()
			tt.mutate(&ruleset)
			if err := ruleset.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want validation error")
			}
		})
	}
}
