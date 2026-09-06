// Package acs holds the rules of a grouped operation: how several fleets arrive
// together, and how they share what they take.
package acs

import (
	"errors"
	"sort"
	"time"

	"universeatwar/internal/domain/economy"
)

var (
	ErrNotForming   = errors.New("acs: this operation no longer accepts fleets")
	ErrTooLate      = errors.New("acs: this operation has already arrived")
	ErrGroupFull    = errors.New("acs: this operation is full")
	ErrNotAMember   = errors.New("acs: only the alliance of the operation may join it")
	ErrEmptyGroup   = errors.New("acs: an operation needs at least one fleet")
	ErrUnknownState = errors.New("acs: unknown operation state")
)

// State is where an operation stands.
type State string

const (
	Forming   State = "forming"
	Locked    State = "locked"
	Resolved  State = "resolved"
	Cancelled State = "cancelled"
)

// Kind separates a grouped attack from a grouped defense.
type Kind string

const (
	Attack  Kind = "attack"
	Defense Kind = "defense"
)

// Valid reports whether the state is one this build knows.
func (s State) Valid() bool {
	switch s {
	case Forming, Locked, Resolved, Cancelled:
		return true
	default:
		return false
	}
}

// Open reports whether fleets may still join or leave.
func (s State) Open() bool {
	return s == Forming
}

// CanTransition reports whether an operation may move from one state to another.
func CanTransition(from, to State) bool {
	switch from {
	case Forming:
		return to == Locked || to == Cancelled
	case Locked:
		return to == Resolved || to == Cancelled
	default:
		return false
	}
}

// Synchronise returns the arrival of the whole operation once a fleet with its
// own arrival joins. The group waits for its slowest member: a late fleet
// delays everybody, and an early one slows down.
func Synchronise(groupArrival, fleetArrival time.Time) time.Time {
	if fleetArrival.After(groupArrival) {
		return fleetArrival
	}
	return groupArrival
}

// DistributeLoot splits what the operation took between its surviving fleets,
// proportionally to the room each still has. The remainders go to the largest
// holds first, so the shares add up to exactly the loot. The loot is computed
// from the very same holds, so it never exceeds them; a larger amount is capped
// rather than overflowing a hold.
func DistributeLoot(loot economy.Resources, capacities []int64) []economy.Resources {
	shares := make([]economy.Resources, len(capacities))
	var total int64
	for _, capacity := range capacities {
		if capacity > 0 {
			total += capacity
		}
	}
	if total <= 0 {
		return shares
	}
	for index, amount := range []int64{loot.Metal, loot.Crystal, loot.Deuterium} {
		for holder, share := range split(amount, capacities, total) {
			switch index {
			case 0:
				shares[holder].Metal = share
			case 1:
				shares[holder].Crystal = share
			default:
				shares[holder].Deuterium = share
			}
		}
	}
	return shares
}

// split shares one resource with the largest remainder method.
func split(amount int64, capacities []int64, total int64) []int64 {
	shares := make([]int64, len(capacities))
	if amount <= 0 {
		return shares
	}
	if amount > total {
		amount = total
	}
	type remainder struct {
		holder int
		value  int64
	}
	remainders := make([]remainder, 0, len(capacities))
	var distributed int64
	for holder, capacity := range capacities {
		if capacity <= 0 {
			continue
		}
		exact := amount * capacity
		shares[holder] = exact / total
		distributed += shares[holder]
		remainders = append(remainders, remainder{holder: holder, value: exact % total})
	}
	// The biggest leftover wins the extra unit; equal leftovers go to the
	// bigger hold, then to the earliest fleet, which keeps the split stable.
	sort.SliceStable(remainders, func(first, second int) bool {
		if remainders[first].value != remainders[second].value {
			return remainders[first].value > remainders[second].value
		}
		return capacities[remainders[first].holder] > capacities[remainders[second].holder]
	})
	for index := 0; distributed < amount && index < len(remainders); index++ {
		shares[remainders[index].holder]++
		distributed++
	}
	return shares
}
