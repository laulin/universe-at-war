package clock

import (
	"testing"
	"time"
)

func TestSystemClockReturnsUTC(t *testing.T) {
	if got := (System{}).Now(); got.Location() != time.UTC {
		t.Fatalf("System.Now() location = %v, want UTC", got.Location())
	}
}

func TestFakeClockCanAdvanceDeterministically(t *testing.T) {
	start := time.Date(2042, time.March, 4, 5, 6, 7, 0, time.FixedZone("test", 3600))
	clock := NewFake(start)

	if got, want := clock.Now(), start.UTC(); !got.Equal(want) || got.Location() != time.UTC {
		t.Fatalf("initial time = %v, want %v in UTC", got, want)
	}

	clock.Advance(90 * time.Minute)
	if got, want := clock.Now(), start.UTC().Add(90*time.Minute); !got.Equal(want) {
		t.Fatalf("advanced time = %v, want %v", got, want)
	}
}

func TestFakeClockRejectsMovingBackwards(t *testing.T) {
	clock := NewFake(time.Now())
	if err := clock.Set(clock.Now().Add(-time.Second)); err == nil {
		t.Fatal("Set() error = nil, want an error for backward time")
	}
}
