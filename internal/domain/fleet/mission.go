// Package fleet models fleet travel, missions and the transitions a fleet may
// go through. A fleet that has left is committed: no real-time piloting exists.
package fleet

import "time"

// Mission is the stable persisted identifier of what a fleet goes to do.
type Mission string

const (
	MissionTransport  Mission = "transport"
	MissionDeploy     Mission = "deploy"
	MissionAttack     Mission = "attack"
	MissionEspionage  Mission = "espionage"
	MissionRecycle    Mission = "recycle"
	MissionColonize   Mission = "colonize"
	MissionHold       Mission = "hold"
	MissionExpedition Mission = "expedition"
	MissionJump       Mission = "jump"
)

// SpeedClass selects the universe speed applied to a mission.
type SpeedClass string

const (
	Peaceful      SpeedClass = "peaceful"
	Hostile       SpeedClass = "hostile"
	Stationary    SpeedClass = "holding"
	Expeditionary SpeedClass = "expedition"
)

// TargetKind is the sort of body a mission aims at.
type TargetKind string

const (
	TargetPlanet TargetKind = "planet"
	TargetMoon   TargetKind = "moon"
	TargetDebris TargetKind = "debris"
	TargetEmpty  TargetKind = "empty"
	TargetSpace  TargetKind = "space"
)

// Valid reports whether the mission is one this build knows.
func (m Mission) Valid() bool {
	switch m {
	case MissionTransport, MissionDeploy, MissionAttack, MissionEspionage, MissionRecycle,
		MissionColonize, MissionHold, MissionExpedition:
		return true
	default:
		return false
	}
}

// Returns reports whether the fleet flies home once the mission is resolved.
// Only a deployment settles on its destination.
func (m Mission) Returns() bool {
	return m != MissionDeploy
}

// SpeedClass returns the universe speed the mission travels at.
func (m Mission) SpeedClass() SpeedClass {
	switch m {
	case MissionAttack:
		return Hostile
	case MissionHold:
		return Stationary
	case MissionExpedition:
		return Expeditionary
	default:
		return Peaceful
	}
}

// TargetsForeignBody reports whether the destination must belong to somebody
// else, which is what makes a mission hostile or intrusive.
func (m Mission) TargetsForeignBody() bool {
	return m == MissionAttack || m == MissionEspionage
}

// Target reports the sort of body the mission aims at.
func (m Mission) Target() TargetKind {
	switch m {
	case MissionRecycle:
		return TargetDebris
	case MissionColonize:
		return TargetEmpty
	case MissionExpedition:
		return TargetSpace
	default:
		return TargetPlanet
	}
}

// Defends reports whether the fleet fights for the body it reaches instead of
// against it, and therefore waits there until its holding time is over.
func (m Mission) Defends() bool {
	return m == MissionHold
}

// Waits reports whether the fleet stays where it lands before coming home. A
// stationing waits to defend, an expedition waits to see what it finds.
func (m Mission) Waits() bool {
	return m == MissionHold || m == MissionExpedition
}

// TargetsOwnBody reports whether the destination must belong to the player.
func (m Mission) TargetsOwnBody() bool {
	return m == MissionDeploy
}

// State is the persisted lifecycle of a fleet.
type State string

const (
	Outbound  State = "outbound"
	Holding   State = "holding"
	Returning State = "returning"
	Recalled  State = "recalled"
	Completed State = "completed"
	Destroyed State = "destroyed"
)

// CanTransition reports whether a fleet may move from one state to another.
func CanTransition(from, to State) bool {
	switch from {
	case Outbound:
		return to == Holding || to == Returning || to == Completed || to == Recalled || to == Destroyed
	case Holding:
		return to == Returning || to == Destroyed
	case Returning, Recalled:
		return to == Completed || to == Destroyed
	default:
		return false
	}
}

// Terminal reports whether the fleet has finished its life.
func (s State) Terminal() bool {
	return s == Completed || s == Destroyed
}

// InFlight reports whether the fleet still occupies a fleet slot.
func (s State) InFlight() bool {
	return s == Outbound || s == Holding || s == Returning || s == Recalled
}

// Recallable reports whether the fleet may still be called back.
func (s State) Recallable() bool {
	return s == Outbound
}

// RecallReturn is the instant a recalled fleet reaches its origin: it flies back
// exactly the time it has already travelled.
func RecallReturn(departedAt, now time.Time) time.Time {
	if now.Before(departedAt) {
		return departedAt
	}
	return now.Add(now.Sub(departedAt))
}
