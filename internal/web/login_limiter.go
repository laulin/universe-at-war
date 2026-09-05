package web

import (
	"sync"
	"time"
)

type loginAttempt struct {
	failures     uint
	blockedUntil time.Time
	lastSeen     time.Time
}

// LoginLimiter implements an in-process progressive login backoff. Persistent
// audit remains in SQLite; this limiter protects the live process from bursts.
type LoginLimiter struct {
	mu       sync.Mutex
	now      func() time.Time
	attempts map[string]loginAttempt
	checks   uint
}

func NewLoginLimiter(now func() time.Time) *LoginLimiter {
	return &LoginLimiter{now: now, attempts: make(map[string]loginAttempt)}
}

// Allow reports when the caller may try again.
func (l *LoginLimiter) Allow(key string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.checks++
	if l.checks%256 == 0 {
		l.prune(now)
	}
	attempt, exists := l.attempts[key]
	if !exists {
		return 0, true
	}
	if now.Sub(attempt.lastSeen) > 15*time.Minute {
		delete(l.attempts, key)
		return 0, true
	}
	if now.Before(attempt.blockedUntil) {
		return attempt.blockedUntil.Sub(now), false
	}
	return 0, true
}

// Failure increases the delay exponentially up to one minute.
func (l *LoginLimiter) Failure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	attempt := l.attempts[key]
	attempt.failures++
	exponent := attempt.failures - 1
	if exponent > 6 {
		exponent = 6
	}
	delay := time.Second * time.Duration(1<<exponent)
	if delay > time.Minute {
		delay = time.Minute
	}
	attempt.blockedUntil = now.Add(delay)
	attempt.lastSeen = now
	l.attempts[key] = attempt
}

// Success clears all accumulated delay for the key.
func (l *LoginLimiter) Success(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

func (l *LoginLimiter) prune(now time.Time) {
	for key, attempt := range l.attempts {
		if now.Sub(attempt.lastSeen) > 15*time.Minute {
			delete(l.attempts, key)
		}
	}
}
