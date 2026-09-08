// Package shipyard orchestrates ship and defense production on a planet.
package shipyard

import (
	"context"
	"errors"
	"strings"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appeconomy "universeatwar/internal/app/economy"
	"universeatwar/internal/domain/catalogue"
	domainclock "universeatwar/internal/domain/clock"
	"universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/prerequisite"
	"universeatwar/internal/domain/unit"
)

var (
	ErrForbidden       = errors.New("shipyard: authenticated account required")
	ErrQueueBusy       = errors.New("shipyard: the production queue changed under this order")
	ErrQueueFull       = errors.New("shipyard: the production queue is full")
	ErrFacilityBusy    = errors.New("shipyard: the shipyard is being upgraded")
	ErrInvalidQuantity = errors.New("shipyard: quantity must be positive and within the order limit")
	ErrInvalidRequest  = errors.New("shipyard: invalid production request")
	ErrWrongFamily     = errors.New("shipyard: unit does not belong to this page")

	ErrQueueEntryNotFound = errors.New("shipyard: no such batch in the queue")
)

// Order is one entry of a production queue: a batch of one model. The entry at
// position zero is the batch being built and carries a real schedule; the ones
// behind it only carry the forecast of when their turn comes.
type Order struct {
	ID           int64
	PlanetID     int64
	Unit         unit.ID
	Family       unit.Family
	Quantity     int64
	Delivered    int64
	UnitCost     economy.Resources
	TotalCost    economy.Resources
	UnitDuration time.Duration
	Position     int
	StartedAt    time.Time
	CompletesAt  time.Time

	EstimatedStartAt     time.Time
	EstimatedCompletesAt time.Time
	State                string
}

// Waiting reports a batch that has not started yet.
func (o Order) Waiting() bool { return o.State == "queued" }

// State is everything the repository knows about a planet's shipyard. The yard
// and the defences hold one queue each and advance side by side.
type State struct {
	Planet    appeconomy.Planet
	Inventory unit.Inventory
	SiloUsed  int
	Ships     []Order
	Defenses  []Order
}

// QueueOf returns the queue of one family.
func (s State) QueueOf(family unit.Family) []Order {
	if family == unit.Defense {
		return s.Defenses
	}
	return s.Ships
}

// Choice is one catalogue entry enriched for the planet.
type Choice struct {
	Definition        unit.Definition
	Owned             int64
	UnitCost          economy.Resources
	UnitDuration      time.Duration
	MaximumAffordable int64
	Available         bool
	Missing           []prerequisite.Requirement
	Reason            string
}

// Overview is the shipyard or defense page projection.
type Overview struct {
	State
	Family unit.Family
	// Queue is the queue of the family this page shows.
	Queue   []Order
	Choices []Choice
}

// Repository is the atomic persistence boundary for production use cases.
type Repository interface {
	State(context.Context, int64, int64, time.Time) (State, error)
	Order(context.Context, int64, int64, unit.ID, int64, string, time.Time) (Order, error)
	CancelOrder(context.Context, int64, int64, int64, time.Time) (appeconomy.Cancellation, error)
}

// Service runs the production use cases of one planet.
type Service struct {
	Clock      domainclock.Clock
	Repository Repository
	Catalogues catalogue.Set
	Completer  appeconomy.Completer
	Wake       func()
}

// Ships returns the shipyard page of one planet.
func (s Service) Ships(ctx context.Context, principal appauth.Principal, planetID int64) (Overview, error) {
	return s.overview(ctx, principal, planetID, unit.Ship)
}

// Defenses returns the defense page of one planet.
func (s Service) Defenses(ctx context.Context, principal appauth.Principal, planetID int64) (Overview, error) {
	return s.overview(ctx, principal, planetID, unit.Defense)
}

func (s Service) overview(ctx context.Context, principal appauth.Principal, planetID int64, family unit.Family) (Overview, error) {
	if err := s.validate(principal); err != nil {
		return Overview{}, err
	}
	if s.Completer != nil {
		if _, err := s.Completer.CompleteDue(ctx, 100); err != nil {
			return Overview{}, err
		}
	}
	state, err := s.Repository.State(ctx, principal.AccountID, planetID, s.Clock.Now().UTC())
	if err != nil {
		return Overview{}, err
	}
	queue := state.QueueOf(family)
	room := len(queue) < state.Planet.Rules.Progression.QueueLength
	definitions := s.Catalogues.Units.Definitions(family)
	choices := make([]Choice, 0, len(definitions))
	requirements := prerequisite.State{
		Buildings:  state.Planet.Levels.Generic(),
		Researches: state.Planet.Researches.Generic(),
	}
	for _, definition := range definitions {
		choice := Choice{Definition: definition, Owned: state.Inventory[definition.ID]}
		choice.Missing = prerequisite.Unmet(definition.Prerequisites, requirements)
		unitCost, err := s.Catalogues.Units.UnitCost(definition.ID, state.Planet.Rules)
		if err != nil {
			choice.Reason = err.Error()
			choices = append(choices, choice)
			continue
		}
		choice.UnitCost = unitCost
		speed := state.Planet.Rules.Time.ShipyardSpeed
		if definition.Family == unit.Defense {
			speed = state.Planet.Rules.Time.DefenseSpeed
		}
		duration, err := s.Catalogues.Units.UnitDuration(unitCost,
			state.Planet.Levels[shipyardBuilding], state.Planet.Levels[naniteBuilding], speed)
		if err != nil {
			choice.Reason = err.Error()
			choices = append(choices, choice)
			continue
		}
		choice.UnitDuration = duration
		choice.MaximumAffordable = affordable(state.Planet.Stock, unitCost)
		choice.Available = room && len(choice.Missing) == 0
		choices = append(choices, choice)
	}
	return Overview{State: state, Family: family, Queue: queue, Choices: choices}, nil
}

// Order launches a production of one unit on the planet.
func (s Service) Order(ctx context.Context, principal appauth.Principal, planetID int64, id unit.ID, quantity int64, idempotencyKey string) (Order, error) {
	if err := s.validate(principal); err != nil {
		return Order{}, err
	}
	if planetID <= 0 || strings.TrimSpace(idempotencyKey) == "" || len(idempotencyKey) > 128 {
		return Order{}, ErrInvalidRequest
	}
	if quantity <= 0 || quantity > unit.MaximumOrderQuantity {
		return Order{}, ErrInvalidQuantity
	}
	order, err := s.Repository.Order(ctx, principal.AccountID, planetID, id, quantity, idempotencyKey, s.Clock.Now().UTC())
	if err == nil && s.Wake != nil {
		s.Wake()
	}
	return order, err
}

// OrderFamily refuses a unit that does not belong to the page it came from.
func (s Service) OrderFamily(ctx context.Context, principal appauth.Principal, planetID int64, id unit.ID, family unit.Family, quantity int64, idempotencyKey string) (Order, error) {
	definition, known := s.Catalogues.Units.Definition(id)
	if !known {
		return Order{}, unit.ErrUnknownUnit
	}
	if definition.Family != family {
		return Order{}, ErrWrongFamily
	}
	return s.Order(ctx, principal, planetID, id, quantity, idempotencyKey)
}

// CancelOrder drops one batch and refunds the units the yard still owed. The
// ones already delivered are the player's to keep.
func (s Service) CancelOrder(ctx context.Context, principal appauth.Principal, planetID, orderID int64) (appeconomy.Cancellation, error) {
	if err := s.validate(principal); err != nil {
		return appeconomy.Cancellation{}, err
	}
	if planetID <= 0 || orderID <= 0 {
		return appeconomy.Cancellation{}, ErrInvalidRequest
	}
	if s.Completer != nil {
		if _, err := s.Completer.CompleteDue(ctx, 100); err != nil {
			return appeconomy.Cancellation{}, err
		}
	}
	cancellation, err := s.Repository.CancelOrder(ctx, principal.AccountID, planetID, orderID, s.Clock.Now().UTC())
	if err == nil && s.Wake != nil {
		s.Wake()
	}
	return cancellation, err
}

// affordable is how many units the current stock could pay for.
func affordable(stock, unitCost economy.Resources) int64 {
	maximum := int64(unit.MaximumOrderQuantity)
	for _, pair := range [][2]int64{
		{stock.Metal, unitCost.Metal},
		{stock.Crystal, unitCost.Crystal},
		{stock.Deuterium, unitCost.Deuterium},
	} {
		if pair[1] <= 0 {
			continue
		}
		if count := pair[0] / pair[1]; count < maximum {
			maximum = count
		}
	}
	if maximum < 0 {
		return 0
	}
	return maximum
}

const (
	shipyardBuilding = "shipyard"
	naniteBuilding   = "nanite_factory"
)

func (s Service) validate(principal appauth.Principal) error {
	if s.Clock == nil || s.Repository == nil {
		return errors.New("shipyard: incomplete service dependencies")
	}
	if principal.AccountID <= 0 || principal.MustChangePassword {
		return ErrForbidden
	}
	return nil
}
