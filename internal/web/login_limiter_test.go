package web

import (
	"testing"
	"time"
)

func TestLoginLimiterAppliesProgressiveBackoff(t *testing.T) {
	now := time.Date(2042, time.November, 12, 13, 14, 15, 0, time.UTC)
	limiter := NewLoginLimiter(func() time.Time { return now })
	key := "127.0.0.1|admin"

	if retry, allowed := limiter.Allow(key); !allowed || retry != 0 {
		t.Fatalf("initial Allow() = %v %v, want allowed", retry, allowed)
	}
	limiter.Failure(key)
	if retry, allowed := limiter.Allow(key); allowed || retry != time.Second {
		t.Fatalf("Allow() after first failure = %v %v, want 1s denial", retry, allowed)
	}

	now = now.Add(time.Second)
	if _, allowed := limiter.Allow(key); !allowed {
		t.Fatal("Allow() remained blocked after retry interval")
	}
	limiter.Failure(key)
	if retry, allowed := limiter.Allow(key); allowed || retry != 2*time.Second {
		t.Fatalf("Allow() after second failure = %v %v, want 2s denial", retry, allowed)
	}

	limiter.Success(key)
	if retry, allowed := limiter.Allow(key); !allowed || retry != 0 {
		t.Fatalf("Allow() after success = %v %v, want reset", retry, allowed)
	}
}
