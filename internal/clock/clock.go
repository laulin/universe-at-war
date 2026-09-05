// Package clock provides production and deterministic implementations of the
// domain clock contract.
package clock

import (
	"errors"
	"sync"
	"time"
)

// System is the production wall clock.
type System struct{}

// Now returns the current instant normalized to UTC.
func (System) Now() time.Time {
	return time.Now().UTC()
}

// Fake is a concurrency-safe controllable clock for tests and protected
// development tools.
type Fake struct {
	mu  sync.RWMutex
	now time.Time
}

// NewFake creates a clock fixed at the supplied instant.
func NewFake(now time.Time) *Fake {
	return &Fake{now: now.UTC()}
}

// Now returns the clock's current instant.
func (c *Fake) Now() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.now
}

// Advance moves the clock forward by a non-negative duration.
func (c *Fake) Advance(duration time.Duration) {
	if duration < 0 {
		panic("clock: cannot advance by a negative duration")
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(duration)
}

// Set moves the clock to a later instant. Moving time backwards is rejected so
// tests cannot accidentally create impossible production intervals.
func (c *Fake) Set(now time.Time) error {
	now = now.UTC()
	c.mu.Lock()
	defer c.mu.Unlock()
	if now.Before(c.now) {
		return errors.New("clock: cannot move backwards")
	}
	c.now = now
	return nil
}
