package research

import (
	"errors"
	"testing"
)

func TestEffectsDeriveFromLevelsOnly(t *testing.T) {
	levels := Levels{
		ComputerTechnology:  3,
		Astrophysics:        5,
		CombustionDrive:     4,
		WeaponsTechnology:   2,
		ShieldingTechnology: 1,
		EspionageTechnology: 7,
	}

	if got := levels.FleetSlots(); got != 4 {
		t.Fatalf("FleetSlots() = %d, want 4", got)
	}
	if got := levels.ColonySlots(9); got != 3 {
		t.Fatalf("ColonySlots(9) = %d, want 3", got)
	}
	if got := levels.ColonySlots(2); got != 2 {
		t.Fatalf("ColonySlots(2) = %d, want 2", got)
	}
	if got := levels.ExpeditionSlots(); got != 2 {
		t.Fatalf("ExpeditionSlots() = %d, want 2", got)
	}
	if got := levels.EspionageLevel(); got != 7 {
		t.Fatalf("EspionageLevel() = %d, want 7", got)
	}
	if got := levels.WeaponsFactor(); got != 1.2 {
		t.Fatalf("WeaponsFactor() = %v, want 1.2", got)
	}
	if got := levels.ShieldingFactor(); got != 1.1 {
		t.Fatalf("ShieldingFactor() = %v, want 1.1", got)
	}
	if got := levels.ArmourFactor(); got != 1 {
		t.Fatalf("ArmourFactor() = %v, want 1", got)
	}

	combustion, err := levels.DriveFactor(CombustionDrive)
	if err != nil || combustion != 1.4 {
		t.Fatalf("DriveFactor(combustion) = %v %v, want 1.4", combustion, err)
	}
	hyperspace, err := levels.DriveFactor(HyperspaceDrive)
	if err != nil || hyperspace != 1 {
		t.Fatalf("DriveFactor(hyperspace) = %v %v, want 1", hyperspace, err)
	}
	if _, err := levels.DriveFactor(WeaponsTechnology); !errors.Is(err, ErrNotADrive) {
		t.Fatalf("DriveFactor(weapons) error = %v, want ErrNotADrive", err)
	}
}

func TestEmptyLevelsGiveTheStartingEmpire(t *testing.T) {
	levels := Levels{}
	if levels.FleetSlots() != 1 || levels.ColonySlots(9) != 0 || levels.ExpeditionSlots() != 0 {
		t.Fatalf("empty levels = %d %d %d", levels.FleetSlots(), levels.ColonySlots(9), levels.ExpeditionSlots())
	}
	if levels.WeaponsFactor() != 1 || levels.ShieldingFactor() != 1 || levels.ArmourFactor() != 1 {
		t.Fatal("empty levels must not change combat factors")
	}
}
