package unit

import (
	"testing"
	"time"
)

func TestDeliveredIsMonotonicAndBounded(t *testing.T) {
	started := time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC)
	unitDuration := 2880 * time.Second
	tests := []struct {
		name    string
		elapsed time.Duration
		want    int64
	}{
		{name: "before the first unit", elapsed: 0, want: 0},
		{name: "one second short", elapsed: unitDuration - time.Second, want: 0},
		{name: "first unit exactly", elapsed: unitDuration, want: 1},
		{name: "second unit exactly", elapsed: 2 * unitDuration, want: 2},
		{name: "whole batch", elapsed: 3 * unitDuration, want: 3},
		{name: "long after the batch", elapsed: 100 * time.Hour, want: 3},
		{name: "before the order started", elapsed: -time.Second, want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Delivered(started, unitDuration, 3, started.Add(test.elapsed)); got != test.want {
				t.Fatalf("Delivered(%v) = %d, want %d", test.elapsed, got, test.want)
			}
		})
	}
}

func TestDeliveredSplitsExactlyLikeASingleSettlement(t *testing.T) {
	started := time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC)
	unitDuration := 60 * time.Second
	for elapsed := time.Duration(0); elapsed <= 400*time.Second; elapsed += 7 * time.Second {
		single := Delivered(started, unitDuration, 5, started.Add(elapsed))
		intermediate := Delivered(started, unitDuration, 5, started.Add(elapsed/2))
		total := intermediate + (Delivered(started, unitDuration, 5, started.Add(elapsed)) - intermediate)
		if single != total {
			t.Fatalf("split settlement at %v = %d, want %d", elapsed, total, single)
		}
	}
}

func BenchmarkDelivered(b *testing.B) {
	started := time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC)
	now := started.Add(50 * time.Hour)
	for b.Loop() {
		Delivered(started, 2880*time.Second, 1_000_000, now)
	}
}
