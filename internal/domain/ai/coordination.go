package ai

import "universeatwar/internal/domain/random"

// coordinationStream keeps the cooperation drawn for a reflection independent of
// the jitter drawn for its schedule, which comes from the same seed and tick.
const coordinationStream uint64 = 0x9e3779b97f4a7c15

// Cooperates reports whether a member answers the call of its alliance on this
// reflection rather than fighting its own war. The degree of coordination a
// universe asks for is read as the share of reflections its members spend on
// what the alliance wants, and the draw is taken from the seed of the player and
// the number of the tick, so replaying a universe replays who answered when.
//
// An alliance asked to act as one always answers; one asked for nothing never
// does, and its members keep their information sharing all the same — telling
// an ally what you saw is not the same as marching where you are told.
func Cooperates(seed, tick int64, coordination float64) bool {
	if coordination >= 1 {
		return true
	}
	if coordination <= 0 {
		return false
	}
	source := random.NewSeeded(uint64(seed) ^ uint64(tick) ^ coordinationStream)
	return random.Chance(source, coordination)
}
