package acs

import (
	"testing"
	"time"

	"universeatwar/internal/domain/economy"
)

func TestStateMachineAllowsOnlyDocumentedTransitions(t *testing.T) {
	allowed := map[[2]State]bool{
		{Forming, Locked}: true, {Forming, Cancelled}: true,
		{Locked, Resolved}: true, {Locked, Cancelled}: true,
	}
	states := []State{Forming, Locked, Resolved, Cancelled}
	for _, from := range states {
		for _, to := range states {
			if got := CanTransition(from, to); got != allowed[[2]State{from, to}] {
				t.Fatalf("CanTransition(%s, %s) = %v", from, to, got)
			}
		}
	}
	if !Forming.Open() || Locked.Open() || Resolved.Open() {
		t.Fatal("only a forming operation is open")
	}
	if State("waiting").Valid() {
		t.Fatal("an unknown state was accepted")
	}
}

func TestSynchroniseWaitsForTheSlowestFleet(t *testing.T) {
	base := time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC)
	later := base.Add(10 * time.Minute)
	earlier := base.Add(-10 * time.Minute)

	if got := Synchronise(base, later); !got.Equal(later) {
		t.Fatalf("a late fleet did not delay the group: %v", got)
	}
	if got := Synchronise(base, earlier); !got.Equal(base) {
		t.Fatalf("an early fleet changed the group arrival: %v", got)
	}
	if got := Synchronise(base, base); !got.Equal(base) {
		t.Fatalf("a synchronised fleet changed the group arrival: %v", got)
	}
}

func TestDistributeLootFillsTheBiggestHoldsFirst(t *testing.T) {
	tests := []struct {
		name       string
		loot       economy.Resources
		capacities []int64
		want       []economy.Resources
	}{
		{
			name: "even split", loot: economy.Resources{Metal: 100}, capacities: []int64{50, 50},
			want: []economy.Resources{{Metal: 50}, {Metal: 50}},
		},
		{
			name: "proportional split", loot: economy.Resources{Metal: 90, Crystal: 30},
			capacities: []int64{100, 200},
			want:       []economy.Resources{{Metal: 30, Crystal: 10}, {Metal: 60, Crystal: 20}},
		},
		{
			name: "the remainder goes to the biggest hold", loot: economy.Resources{Metal: 5},
			capacities: []int64{3, 6},
			want:       []economy.Resources{{Metal: 2}, {Metal: 3}},
		},
		{
			name: "a loot larger than the holds is capped", loot: economy.Resources{Metal: 100},
			capacities: []int64{1, 2},
			want:       []economy.Resources{{Metal: 1}, {Metal: 2}},
		},
		{
			name: "an empty hold takes nothing", loot: economy.Resources{Metal: 10},
			capacities: []int64{0, 10},
			want:       []economy.Resources{{}, {Metal: 10}},
		},
		{name: "nothing to share", loot: economy.Resources{}, capacities: []int64{10, 10},
			want: []economy.Resources{{}, {}}},
		{name: "nowhere to put it", loot: economy.Resources{Metal: 10}, capacities: []int64{0, 0},
			want: []economy.Resources{{}, {}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := DistributeLoot(test.loot, test.capacities)
			if len(got) != len(test.want) {
				t.Fatalf("DistributeLoot() = %d shares, want %d", len(got), len(test.want))
			}
			var total economy.Resources
			for index := range got {
				if got[index] != test.want[index] {
					t.Fatalf("share %d = %+v, want %+v", index, got[index], test.want[index])
				}
				total.Metal += got[index].Metal
				total.Crystal += got[index].Crystal
				total.Deuterium += got[index].Deuterium
				if sum := got[index].Metal + got[index].Crystal + got[index].Deuterium; sum > test.capacities[index] {
					t.Fatalf("share %d holds %d in a hold of %d", index, sum, test.capacities[index])
				}
			}
			var room int64
			for _, capacity := range test.capacities {
				room += capacity
			}
			expected := test.loot
			if expected.Metal > room {
				expected.Metal = room
			}
			if total != expected {
				t.Fatalf("the shares add up to %+v, want %+v", total, expected)
			}
		})
	}
}
