package rules

import (
	"bytes"
	"encoding/json"
	"errors"
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
		{name: "decreasing espionage thresholds", mutate: func(r *Ruleset) { r.Espionage.FleetThreshold = 0 }},
		{name: "impossible detection base", mutate: func(r *Ruleset) { r.Espionage.DetectionBase = 2 }},
		{name: "no report freshness", mutate: func(r *Ruleset) { r.Espionage.RecentReportSeconds = 0 }},
		{name: "moon without a field", mutate: func(r *Ruleset) { r.Expansion.BaseMoonFields = 0 }},
		{name: "jump gate without a cooldown", mutate: func(r *Ruleset) { r.Expansion.JumpGateCooldownSeconds = 0 }},
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

func TestDecodeFillsSectionsMissingFromOlderDocuments(t *testing.T) {
	document, err := Encode(Default())
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	var generic map[string]any
	if err := json.Unmarshal(document, &generic); err != nil {
		t.Fatalf("unmarshal document: %v", err)
	}
	delete(generic, "schema_version")
	delete(generic, "espionage")
	delete(generic, "expansion")
	progression, ok := generic["progression"].(map[string]any)
	if !ok {
		t.Fatal("progression section is missing from the encoded document")
	}
	delete(progression, "catalogue_version")
	legacy, err := json.Marshal(generic)
	if err != nil {
		t.Fatalf("marshal legacy document: %v", err)
	}

	decoded, err := Decode(legacy)
	if err != nil {
		t.Fatalf("Decode(legacy) error = %v", err)
	}
	if decoded.SchemaVersion != CurrentSchemaVersion {
		t.Fatalf("schema version = %d, want %d", decoded.SchemaVersion, CurrentSchemaVersion)
	}
	if decoded.Progression.CatalogueVersion != DefaultCatalogueVersion {
		t.Fatalf("catalogue version = %q, want %q", decoded.Progression.CatalogueVersion, DefaultCatalogueVersion)
	}
	if decoded.Espionage != Default().Espionage {
		t.Fatalf("espionage section = %+v, want the defaults", decoded.Espionage)
	}
	if decoded.Expansion != Default().Expansion {
		t.Fatalf("expansion section = %+v, want the defaults", decoded.Expansion)
	}
}

func TestDecodeRejectsFutureSchemaAndUnknownFields(t *testing.T) {
	document, err := Encode(Default())
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	future := bytes.Replace(document, []byte(`"schema_version":4`), []byte(`"schema_version":99`), 1)
	if _, err := Decode(future); !errors.Is(err, ErrFutureSchema) {
		t.Fatalf("Decode(future schema) error = %v, want ErrFutureSchema", err)
	}
	unknown := bytes.Replace(document, []byte(`"schema_version":4`), []byte(`"schema_version":4,"mystery":1`), 1)
	if _, err := Decode(unknown); err == nil {
		t.Fatal("Decode(unknown field) accepted a document with an unknown field")
	}
}
