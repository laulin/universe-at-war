// Package combat resolves a battle as a pure function of its inputs and one
// seeded source of randomness. It knows nothing of HTTP or of the database.
package combat

// Rules of the engine itself, as opposed to the tunable ruleset settings. They
// are constants because changing them changes the game, not a universe.
const (
	// explosionThresholdPercent is the hull share below which a damaged unit
	// may explode at the end of a round.
	explosionThresholdPercent = 70
	// shieldBouncePercent is the share of a shield below which a shot has no
	// effect at all.
	shieldBouncePercent = 1
	// moonChanceDebrisStep is the amount of debris worth one percent of moon
	// chance.
	moonChanceDebrisStep = 100_000
	// MaximumUnitsPerSide bounds a battle so a single resolution cannot exhaust
	// the memory of the server.
	MaximumUnitsPerSide = 10_000_000
)
