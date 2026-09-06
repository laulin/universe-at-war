package fleet

import (
	"time"

	"universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/research"
	"universeatwar/internal/domain/rules"
	"universeatwar/internal/domain/unit"
	"universeatwar/internal/domain/universe"
)

// LaunchRequest is what the player asks for.
type LaunchRequest struct {
	Origin      universe.Coordinate
	Target      universe.Coordinate
	TargetKind  TargetKind
	Mission     Mission
	Composition Composition
	Cargo       economy.Resources
	Percent     int
	HoldUntil   time.Time
}

// Context is the state the launch is validated against.
type Context struct {
	Catalogue    unit.Catalogue
	Levels       research.Levels
	Inventory    unit.Inventory
	Stock        economy.Resources
	ActiveFleets int
	Rules        rules.Ruleset
	Now          time.Time
}

// Plan is the immutable calculation captured when a fleet leaves.
type Plan struct {
	Distance   int64
	Speed      int64
	Fuel       int64
	Capacity   int64
	Duration   time.Duration
	DepartsAt  time.Time
	ArrivesAt  time.Time
	HoldsUntil *time.Time
	ReturnsAt  *time.Time
	Debit      economy.Resources
}

// PlanLaunch validates a launch and captures its timings, fuel and debit. It
// mutates nothing: the caller applies the plan inside its own transaction.
func PlanLaunch(request LaunchRequest, context Context) (Plan, error) {
	if err := context.Rules.Validate(); err != nil {
		return Plan{}, err
	}
	if !request.Mission.Valid() {
		return Plan{}, ErrInvalidMission
	}
	switch request.TargetKind {
	case TargetPlanet, TargetMoon, TargetDebris, TargetEmpty:
	default:
		return Plan{}, ErrInvalidTarget
	}
	if err := request.Composition.Validate(context.Catalogue); err != nil {
		return Plan{}, err
	}
	if err := ValidateComposition(request.Mission, request.Composition, context.Catalogue); err != nil {
		return Plan{}, err
	}
	if request.TargetKind != request.Mission.Target() {
		return Plan{}, ErrInvalidTarget
	}
	for id, quantity := range request.Composition {
		if context.Inventory[id] < quantity {
			return Plan{}, ErrInsufficientUnits
		}
	}
	if err := request.Cargo.Validate(); err != nil {
		return Plan{}, err
	}
	slots := context.Levels.FleetSlots()
	if context.ActiveFleets >= slots {
		return Plan{}, ErrNoFleetSlot
	}
	distance, err := Distance(request.Origin, request.Target, context.Rules.Topology)
	if err != nil {
		return Plan{}, err
	}
	speed, err := FleetSpeed(request.Composition, context.Catalogue, context.Levels)
	if err != nil {
		return Plan{}, err
	}
	universeSpeed := context.Rules.Time.PeacefulFleetSpeed
	switch request.Mission.SpeedClass() {
	case Hostile:
		universeSpeed = context.Rules.Time.HostileFleetSpeed
	case Stationary:
		universeSpeed = context.Rules.Time.HoldingFleetSpeed
	}
	minimum := time.Duration(context.Rules.Time.MinimumMissionSeconds) * time.Second
	duration, err := Duration(distance, speed, request.Percent, universeSpeed, minimum)
	if err != nil {
		return Plan{}, err
	}
	fuel, err := Fuel(request.Composition, context.Catalogue, context.Levels, distance, request.Percent, context.Rules.Economy.DeuteriumConsumption)
	if err != nil {
		return Plan{}, err
	}
	capacity, err := Capacity(request.Composition, context.Catalogue)
	if err != nil {
		return Plan{}, err
	}
	remaining := capacity - fuel
	if remaining < 0 {
		return Plan{}, ErrInsufficientFuel
	}
	if request.Cargo.Metal+request.Cargo.Crystal+request.Cargo.Deuterium > remaining {
		return Plan{}, ErrCargoExceedsCapacity
	}
	debit := economy.Resources{
		Metal:     request.Cargo.Metal,
		Crystal:   request.Cargo.Crystal,
		Deuterium: request.Cargo.Deuterium + fuel,
	}
	if context.Stock.Deuterium < fuel {
		return Plan{}, ErrInsufficientFuel
	}
	if !context.Stock.Covers(debit) {
		return Plan{}, economy.ErrInsufficientResources
	}
	departsAt := context.Now.UTC().Truncate(time.Second)
	arrivesAt := departsAt.Add(duration)
	plan := Plan{
		Distance: distance, Speed: speed, Fuel: fuel, Capacity: remaining, Duration: duration,
		DepartsAt: departsAt, ArrivesAt: arrivesAt, Debit: debit,
	}
	if request.Mission.Returns() {
		returnsAt := arrivesAt.Add(duration)
		plan.ReturnsAt = &returnsAt
	}
	if request.Mission.Defends() {
		holdsUntil := request.HoldUntil.UTC().Truncate(time.Second)
		maximum := arrivesAt.Add(time.Duration(context.Rules.Team.MaximumHoldHours) * time.Hour)
		if !holdsUntil.After(arrivesAt) || holdsUntil.After(maximum) {
			return Plan{}, ErrInvalidHold
		}
		returnsAt := holdsUntil.Add(duration)
		plan.HoldsUntil = &holdsUntil
		plan.ReturnsAt = &returnsAt
	}
	return plan, nil
}
