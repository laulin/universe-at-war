// Package phalanx tells when fleets reach a position, never what they carry.
package phalanx

import (
	"errors"

	"universeatwar/internal/domain/rules"
	"universeatwar/internal/domain/universe"
)

var (
	ErrNoPhalanx   = errors.New("phalanx: this moon carries no sensor phalanx")
	ErrOutOfRange  = errors.New("phalanx: this position is out of range")
	ErrOtherGalaxy = errors.New("phalanx: a phalanx never reaches another galaxy")
)

// Radius is how many systems away a phalanx of that level can look.
func Radius(level int) int {
	if level <= 0 {
		return -1
	}
	return level*level - 1
}

// InRange reports whether a phalanx standing on one coordinate may scan
// another. The gap between systems follows the circularity of the topology.
func InRange(from, to universe.Coordinate, level int, topology rules.TopologySettings) error {
	if level <= 0 {
		return ErrNoPhalanx
	}
	if from.Galaxy != to.Galaxy {
		return ErrOtherGalaxy
	}
	gap := from.System - to.System
	if gap < 0 {
		gap = -gap
	}
	if topology.CircularSystems && topology.SystemsPerGalaxy-gap < gap {
		gap = topology.SystemsPerGalaxy - gap
	}
	if gap > Radius(level) {
		return ErrOutOfRange
	}
	return nil
}
