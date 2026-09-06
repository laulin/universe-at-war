package ai

import "time"

// StaleFactor tells how many times the recency threshold of a report an
// artificial player keeps trusting it, less and less, before it means nothing.
const StaleFactor = 12

// Fresh grades what an observation is still worth: one while it is recent, then
// a straight decay down to zero once it has gone stale.
func Fresh(observedAt, now time.Time, recent time.Duration) float64 {
	if recent <= 0 {
		return 0
	}
	age := now.Sub(observedAt)
	if age < 0 {
		return 0
	}
	if age <= recent {
		return 1
	}
	stale := recent * StaleFactor
	if age >= stale {
		return 0
	}
	return 1 - float64(age-recent)/float64(stale-recent)
}
