package research

import (
	"errors"
	"math"
)

// ErrNotADrive reports a drive factor asked of a research that is not a drive.
var ErrNotADrive = errors.New("research: technology is not a drive")

// driveBonus is the speed gained per level of each drive.
var driveBonus = map[ID]float64{
	CombustionDrive: .1,
	ImpulseDrive:    .2,
	HyperspaceDrive: .3,
}

// FleetSlots is the number of missions a player may run at once.
func (l Levels) FleetSlots() int {
	return 1 + l[ComputerTechnology]
}

// ColonySlots is the number of colonies allowed, capped by the universe.
func (l Levels) ColonySlots(limit int) int {
	slots := (l[Astrophysics] + 1) / 2
	if limit > 0 && slots > limit {
		return limit
	}
	return slots
}

// ExpeditionSlots is the number of expeditions allowed at once.
func (l Levels) ExpeditionSlots() int {
	return int(math.Sqrt(float64(l[Astrophysics])))
}

// EspionageLevel drives what a spy report reveals.
func (l Levels) EspionageLevel() int {
	return l[EspionageTechnology]
}

// NetworkSize is the number of remote laboratories the network may add.
func (l Levels) NetworkSize() int {
	return l[IntergalacticResearchNetwork]
}

// DriveFactor multiplies the base speed of the ships using that drive.
func (l Levels) DriveFactor(drive ID) (float64, error) {
	bonus, isDrive := driveBonus[drive]
	if !isDrive {
		return 0, ErrNotADrive
	}
	return 1 + bonus*float64(l[drive]), nil
}

// WeaponsFactor multiplies the weapon of every unit in combat.
func (l Levels) WeaponsFactor() float64 {
	return 1 + .1*float64(l[WeaponsTechnology])
}

// ShieldingFactor multiplies the shield of every unit in combat.
func (l Levels) ShieldingFactor() float64 {
	return 1 + .1*float64(l[ShieldingTechnology])
}

// ArmourFactor multiplies the hull of every unit in combat.
func (l Levels) ArmourFactor() float64 {
	return 1 + .1*float64(l[ArmourTechnology])
}
