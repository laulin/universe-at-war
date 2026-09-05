// Package catalogue groups the versioned game content and validates it together
// with the ruleset. The setup wizard, an import and the administration all go
// through this single validator.
package catalogue

import (
	"errors"
	"fmt"

	"universeatwar/internal/domain/building"
	"universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/prerequisite"
	"universeatwar/internal/domain/research"
	"universeatwar/internal/domain/rules"
	"universeatwar/internal/domain/unit"
)

var (
	ErrUnknownVersion = errors.New("catalogue: unknown catalogue version")
	ErrUnknownEntry   = errors.New("catalogue: requirement references an unknown entry")
	ErrMismatch       = errors.New("catalogue: the ruleset expects another catalogue version")
)

// Set is the whole content of one catalogue version.
type Set struct {
	Version   string
	Buildings building.Catalogue
	Research  research.Catalogue
	Units     unit.Catalogue
}

// Default returns the catalogue shipped with this build.
func Default() Set {
	return Set{
		Version:   rules.DefaultCatalogueVersion,
		Buildings: building.DefaultCatalogue(),
		Research:  research.DefaultCatalogue(),
		Units:     unit.DefaultCatalogue(),
	}
}

// Load returns the catalogue of one version.
func Load(version string) (Set, error) {
	if version != rules.DefaultCatalogueVersion {
		return Set{}, fmt.Errorf("%w: %s", ErrUnknownVersion, version)
	}
	return Default(), nil
}

// Matches refuses a ruleset built for another catalogue version.
func (s Set) Matches(configured rules.Ruleset) error {
	if configured.Progression.CatalogueVersion != s.Version {
		return fmt.Errorf("%w: ruleset expects %s, catalogue is %s", ErrMismatch, configured.Progression.CatalogueVersion, s.Version)
	}
	return nil
}

// ValidateRuleset is the single validator: it checks the ruleset itself, the
// catalogue it names and the consistency between the two.
func ValidateRuleset(configured rules.Ruleset) error {
	if err := configured.Validate(); err != nil {
		return err
	}
	set, err := Load(configured.Progression.CatalogueVersion)
	if err != nil {
		return err
	}
	return set.Validate()
}

// Validate refuses a catalogue whose graph is circular, whose requirements point
// at unknown entries or whose content is impossible.
func (s Set) Validate() error {
	if err := prerequisite.ValidateGraph(s.Buildings.RequirementEdges()); err != nil {
		return err
	}
	if err := prerequisite.ValidateGraph(s.Research.RequirementEdges()); err != nil {
		return err
	}
	for _, definition := range s.Buildings.Definitions() {
		if err := s.checkEntry(string(definition.ID), definition.BaseCost, 0, definition.Prerequisites); err != nil {
			return err
		}
		if definition.Growth <= 1 {
			return fmt.Errorf("catalogue: building %s has a growth of %v", definition.ID, definition.Growth)
		}
	}
	for _, definition := range s.Research.Definitions() {
		if err := s.checkEntry(string(definition.ID), definition.BaseCost, definition.BaseEnergy, definition.Prerequisites); err != nil {
			return err
		}
		if definition.Growth <= 1 {
			return fmt.Errorf("catalogue: research %s has a growth of %v", definition.ID, definition.Growth)
		}
	}
	for _, family := range []unit.Family{unit.Ship, unit.Defense} {
		for _, definition := range s.Units.Definitions(family) {
			if err := s.checkEntry(string(definition.ID), definition.BaseCost, 0, definition.Prerequisites); err != nil {
				return err
			}
			if err := s.checkRapidFire(definition); err != nil {
				return err
			}
			if definition.Drive != "" {
				if _, known := s.Research.Definition(definition.Drive); !known {
					return fmt.Errorf("%w: unit %s uses drive %s", ErrUnknownEntry, definition.ID, definition.Drive)
				}
			}
			if definition.MaximumQuantity < 0 || definition.SiloSlots < 0 {
				return fmt.Errorf("catalogue: unit %s has negative limits", definition.ID)
			}
		}
	}
	return nil
}

// checkEntry refuses a free entry and a requirement pointing nowhere.
func (s Set) checkEntry(id string, cost economy.Resources, energy int64, requirements []prerequisite.Requirement) error {
	if err := cost.Validate(); err != nil {
		return fmt.Errorf("catalogue: %s has an invalid cost: %w", id, err)
	}
	if cost == (economy.Resources{}) && energy <= 0 {
		return fmt.Errorf("catalogue: %s costs nothing", id)
	}
	for _, requirement := range requirements {
		if requirement.Level <= 0 {
			return fmt.Errorf("catalogue: %s has a requirement without level", id)
		}
		if err := s.known(requirement); err != nil {
			return fmt.Errorf("%w: %s requires %s %s", err, id, requirement.Kind, requirement.ID)
		}
	}
	return nil
}

func (s Set) known(requirement prerequisite.Requirement) error {
	switch requirement.Kind {
	case prerequisite.Building:
		if _, known := s.Buildings.Definition(building.ID(requirement.ID)); !known {
			return ErrUnknownEntry
		}
	case prerequisite.Research:
		if _, known := s.Research.Definition(research.ID(requirement.ID)); !known {
			return ErrUnknownEntry
		}
	default:
		return ErrUnknownEntry
	}
	return nil
}

func (s Set) checkRapidFire(definition unit.Definition) error {
	for target, shots := range definition.RapidFire {
		if target == definition.ID {
			return fmt.Errorf("catalogue: unit %s has rapid fire against itself", definition.ID)
		}
		if shots < 2 {
			return fmt.Errorf("catalogue: unit %s has a rapid fire of %d against %s", definition.ID, shots, target)
		}
		if _, known := s.Units.Definition(target); !known {
			return fmt.Errorf("%w: unit %s has rapid fire against %s", ErrUnknownEntry, definition.ID, target)
		}
	}
	return nil
}
