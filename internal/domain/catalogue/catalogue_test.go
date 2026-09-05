package catalogue

import (
	"errors"
	"testing"

	"universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/prerequisite"
	"universeatwar/internal/domain/research"
	"universeatwar/internal/domain/rules"
	"universeatwar/internal/domain/unit"
)

func TestDefaultCatalogueMatchesTheDefaultRuleset(t *testing.T) {
	if err := ValidateRuleset(rules.Default()); err != nil {
		t.Fatalf("ValidateRuleset(default) error = %v", err)
	}
	set, err := Load(rules.DefaultCatalogueVersion)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if set.Version != rules.DefaultCatalogueVersion {
		t.Fatalf("version = %q", set.Version)
	}
	if err := set.Matches(rules.Default()); err != nil {
		t.Fatalf("Matches() error = %v", err)
	}
}

func TestValidateRulesetRejectsAnUnknownCatalogue(t *testing.T) {
	configured := rules.Default()
	configured.Progression.CatalogueVersion = "classic-99"
	if err := ValidateRuleset(configured); !errors.Is(err, ErrUnknownVersion) {
		t.Fatalf("ValidateRuleset() error = %v, want ErrUnknownVersion", err)
	}
	if err := Default().Matches(configured); !errors.Is(err, ErrMismatch) {
		t.Fatalf("Matches() error = %v, want ErrMismatch", err)
	}
	if _, err := Load("classic-99"); !errors.Is(err, ErrUnknownVersion) {
		t.Fatalf("Load() error = %v, want ErrUnknownVersion", err)
	}
}

func TestValidateRejectsBrokenCatalogues(t *testing.T) {
	cycle := Default()
	cycle.Research = research.NewCatalogue([]research.Definition{
		{ID: "first", BaseCost: economy.Resources{Metal: 10}, Growth: 2,
			Prerequisites: []prerequisite.Requirement{{Kind: prerequisite.Research, ID: "second", Level: 1}}},
		{ID: "second", BaseCost: economy.Resources{Metal: 10}, Growth: 2,
			Prerequisites: []prerequisite.Requirement{{Kind: prerequisite.Research, ID: "first", Level: 1}}},
	})
	if err := cycle.Validate(); !errors.Is(err, prerequisite.ErrCycle) {
		t.Fatalf("Validate() with a cycle error = %v, want ErrCycle", err)
	}

	unknown := Default()
	unknown.Research = research.NewCatalogue([]research.Definition{
		{ID: "first", BaseCost: economy.Resources{Metal: 10}, Growth: 2,
			Prerequisites: []prerequisite.Requirement{{Kind: prerequisite.Building, ID: "space_elevator", Level: 1}}},
	})
	if err := unknown.Validate(); !errors.Is(err, ErrUnknownEntry) {
		t.Fatalf("Validate() with an unknown building error = %v, want ErrUnknownEntry", err)
	}

	rapidFire := Default()
	rapidFire.Units = unit.NewCatalogue([]unit.Definition{
		{ID: unit.LightFighter, Family: unit.Ship, BaseCost: economy.Resources{Metal: 3000, Crystal: 1000},
			RapidFire: map[unit.ID]int{"star_destroyer": 5}},
	})
	if err := rapidFire.Validate(); !errors.Is(err, ErrUnknownEntry) {
		t.Fatalf("Validate() with unknown rapid fire error = %v, want ErrUnknownEntry", err)
	}

	selfFire := Default()
	selfFire.Units = unit.NewCatalogue([]unit.Definition{
		{ID: unit.LightFighter, Family: unit.Ship, BaseCost: economy.Resources{Metal: 3000, Crystal: 1000},
			RapidFire: map[unit.ID]int{unit.LightFighter: 5}},
	})
	if err := selfFire.Validate(); err == nil {
		t.Fatal("Validate() accepted rapid fire against itself")
	}

	free := Default()
	free.Units = unit.NewCatalogue([]unit.Definition{
		{ID: unit.LightFighter, Family: unit.Ship},
	})
	if err := free.Validate(); err == nil {
		t.Fatal("Validate() accepted a unit that costs nothing")
	}
}
