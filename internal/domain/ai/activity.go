package ai

import (
	"time"

	"universeatwar/internal/domain/random"
)

// JitterAmplitude spreads the thinking of an artificial player around its
// interval, so a universe full of them does not think in lockstep.
const JitterAmplitude = .25

// Window is the daily stretch of hours, in UTC, during which an artificial
// player takes new decisions. A window whose bounds are equal never closes.
type Window struct {
	Start int
	End   int
}

// Valid reports whether both bounds name an hour of the day.
func (w Window) Valid() bool {
	return w.Start >= 0 && w.Start <= 23 && w.End >= 0 && w.End <= 23
}

// Always reports whether the window covers the whole day.
func (w Window) Always() bool {
	return w.Start == w.End
}

// Awake reports whether new decisions are allowed at that instant. A window may
// wrap around midnight, in which case it covers the two ends of the day.
func (w Window) Awake(at time.Time) bool {
	if !w.Valid() {
		return false
	}
	if w.Always() {
		return true
	}
	hour := at.UTC().Hour()
	if w.Start < w.End {
		return hour >= w.Start && hour < w.End
	}
	return hour >= w.Start || hour < w.End
}

// NextOpening is the first instant after `at` when the window is open again. An
// always-open window opens right away.
func (w Window) NextOpening(at time.Time) time.Time {
	at = at.UTC().Truncate(time.Second)
	if !w.Valid() || w.Always() {
		return at
	}
	if w.Awake(at) {
		return at
	}
	opening := time.Date(at.Year(), at.Month(), at.Day(), w.Start, 0, 0, 0, time.UTC)
	if !opening.After(at) {
		opening = opening.AddDate(0, 0, 1)
	}
	return opening
}

// NextThink is the instant of the following reflection: the configured interval
// spread by a jitter drawn from the seed of the player and the number of the
// tick, so replaying the same universe replays the same schedule.
func NextThink(now time.Time, interval time.Duration, seed, tick int64) time.Time {
	if interval <= 0 {
		interval = time.Minute
	}
	source := random.NewSeeded(uint64(seed) ^ uint64(tick))
	spread := float64(interval) * JitterAmplitude * (source.Float64()*2 - 1)
	delay := time.Duration(float64(interval) + spread)
	if delay < time.Second {
		delay = time.Second
	}
	return now.UTC().Truncate(time.Second).Add(delay.Truncate(time.Second))
}
