// Package economy orchestrates player planets, lazy production and construction.
package economy

import (
	"context"
	"errors"
	"strings"
	"time"

	appauth "universeatwar/internal/app/authentication"
	"universeatwar/internal/domain/building"
	domainclock "universeatwar/internal/domain/clock"
	"universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/prerequisite"
	"universeatwar/internal/domain/research"
	"universeatwar/internal/domain/rules"
	"universeatwar/internal/domain/unit"
	"universeatwar/internal/domain/universe"
)

var (
	ErrForbidden      = errors.New("economy: authenticated account required")
	ErrEmpireExists   = errors.New("economy: account already owns an empire")
	ErrNameTaken      = errors.New("economy: this player name is already used")
	ErrNoEmpire       = errors.New("economy: account has no empire")
	ErrPlanetNotFound = errors.New("economy: planet does not belong to this account")
	ErrUniverseFull   = errors.New("economy: universe has no free position")
	ErrQueueBusy      = errors.New("economy: the construction queue changed under this order")
	ErrQueueFull      = errors.New("economy: the construction queue is full")
	ErrFacilityBusy   = errors.New("economy: the facility is in use by another activity")
	ErrInvalidName    = errors.New("economy: player name must contain 3 to 32 characters")
	ErrInvalidRequest = errors.New("economy: invalid construction request")
	// ErrQueueEntryNotFound covers an order that never existed, belongs to
	// somebody else, or has already left the queue.
	ErrQueueEntryNotFound = errors.New("economy: no such construction in the queue")
)

// Queue is one entry of a body's construction queue. The entry at position
// zero is the one being built and carries a real schedule; the entries behind
// it only carry an estimate, because their duration is decided when their turn
// comes.
type Queue struct {
	ID          int64
	Building    building.ID
	TargetLevel int
	Cost        economy.Resources
	Position    int
	StartedAt   time.Time
	CompletesAt time.Time
	// EstimatedStartAt and EstimatedCompletesAt are only filled for an entry
	// that is still waiting, and are never persisted.
	EstimatedStartAt     time.Time
	EstimatedCompletesAt time.Time
	State                string
}

// Waiting reports an entry that has not started yet.
func (q Queue) Waiting() bool { return q.State == "queued" }

// Planet is the complete economic projection returned after lazy settlement.
type Planet struct {
	ID                 int64
	Kind               building.Placement
	ParentID           int64
	Name               string
	PlayerName         string
	Coordinate         universe.Coordinate
	TotalFields        int
	UsedFields         int
	MinimumTemperature int
	MaximumTemperature int
	Stock              economy.Resources
	Capacity           economy.Resources
	Rates              economy.Rates
	Energy             economy.Energy
	Levels             building.Levels
	Researches         research.Levels
	Units              unit.Inventory
	// Queue holds every construction ordered on this body and not yet finished,
	// head first.
	Queue []Queue
	// BusyFacilities names the installations another queue is counting on, which
	// therefore cannot be upgraded. The rule is enforced when an order is taken;
	// carrying it here is what lets a page stop offering what will be refused.
	BusyFacilities []building.ID
	Rules          rules.Ruleset
}

// Cancellation is what dropping orders from a queue gave back. Lost is the part
// the stores were too full to hold, which the player is told about rather than
// letting it vanish quietly.
type Cancellation struct {
	Cancelled int
	Refunded  economy.Resources
	Lost      economy.Resources
}

// ProjectedLevels applies the whole queue to the built levels. Every reader
// plans against it, so a card offers the level after the queue rather than one
// the queue already reaches — and its price, its duration and the idempotency
// key of its form move with the queue.
func (p Planet) ProjectedLevels() building.Levels {
	projected := building.Levels{}
	for id, level := range p.Levels {
		projected[id] = level
	}
	for _, entry := range p.Queue {
		projected[entry.Building] = entry.TargetLevel
	}
	return projected
}

// BookedFields counts the fields the queue has already spoken for. They are
// only consumed at completion, so a queue would otherwise overrun the body.
func (p Planet) BookedFields() int { return p.UsedFields + len(p.Queue) }

// Requirement is one prerequisite of an entry together with the level the body
// actually reaches, so a locked card can show what is done as well as what is
// left instead of naming only the gap.
type Requirement struct {
	prerequisite.Requirement
	Reached int
}

// Met reports a requirement the body already satisfies.
func (r Requirement) Met() bool { return r.Reached >= r.Level }

// BuildingChoice is one catalogue entry enriched for a planet.
type BuildingChoice struct {
	Definition building.Definition
	Level      int
	Plan       building.Plan
	Available  bool
	Affordable bool
	// Requirements lists every prerequisite of the entry, met or not, against
	// the levels the queue is going to reach.
	Requirements []Requirement
	// Missing names the prerequisites the planet has not met, so the caller can
	// say so in its own words instead of showing a raw error.
	Missing []prerequisite.Requirement
	// FacilityBusy says another queue is counting on this installation. The
	// wording belongs to the caller, as every other reason does.
	FacilityBusy bool
	// Refusal is why the plan could not be calculated at all, kept as the error
	// it is rather than as its text: only the caller can say it in the player's
	// language, and only from a value it can recognise.
	Refusal error
}

// Repository is the atomic persistence boundary for economic use cases.
type Repository interface {
	CreateEmpireNear(context.Context, int64, string, universe.Coordinate, time.Time) (Planet, error)
	Planet(context.Context, int64, int64, time.Time, building.Catalogue) (Planet, error)
	Planets(context.Context, int64, time.Time, building.Catalogue) ([]Planet, error)
	EnqueueBuilding(context.Context, int64, int64, building.ID, string, time.Time, building.Catalogue) (Queue, error)
	CancelBuilding(context.Context, int64, int64, int64, time.Time, building.Catalogue) (Cancellation, error)
}

// Completer settles the scheduled events that are already due, so an
// authoritative read never shows a stale queue.
type Completer interface {
	CompleteDue(context.Context, int) (int, error)
}

type Service struct {
	Clock      domainclock.Clock
	Repository Repository
	Catalogue  building.Catalogue
	Completer  Completer
	Wake       func()
}

// CreateEmpire founds the empire of an account at the first free position of
// the universe, which is what a registration asks for.
func (s Service) CreateEmpire(ctx context.Context, principal appauth.Principal, name string) (Planet, error) {
	return s.createEmpire(ctx, principal, name, universe.Coordinate{})
}

// CreateEmpireNear founds an empire aiming at a given corner of the map, and
// settles it at the first free position from there. It is how a population
// founded in one go is spread out; a registration asks for no corner in
// particular and takes the first free position of the universe, as it always
// has.
func (s Service) CreateEmpireNear(ctx context.Context, principal appauth.Principal, name string, near universe.Coordinate) (Planet, error) {
	return s.createEmpire(ctx, principal, name, near)
}

func (s Service) createEmpire(ctx context.Context, principal appauth.Principal, name string, near universe.Coordinate) (Planet, error) {
	if err := s.validatePrincipal(principal); err != nil {
		return Planet{}, err
	}
	name = strings.TrimSpace(name)
	if length := len([]rune(name)); length < 3 || length > 32 {
		return Planet{}, ErrInvalidName
	}
	return s.Repository.CreateEmpireNear(ctx, principal.AccountID, name, near, s.Clock.Now().UTC())
}

// Planet returns one settled planet of the account. A zero identifier selects
// the oldest planet, which is the home world until colonisation exists.
func (s Service) Planet(ctx context.Context, principal appauth.Principal, planetID int64) (Planet, error) {
	if err := s.validatePrincipal(principal); err != nil {
		return Planet{}, err
	}
	if err := s.settleDueEvents(ctx); err != nil {
		return Planet{}, err
	}
	return s.Repository.Planet(ctx, principal.AccountID, planetID, s.Clock.Now().UTC(), s.Catalogue)
}

// Planets returns every settled body of the account, oldest first.
func (s Service) Planets(ctx context.Context, principal appauth.Principal) ([]Planet, error) {
	if err := s.validatePrincipal(principal); err != nil {
		return nil, err
	}
	if err := s.settleDueEvents(ctx); err != nil {
		return nil, err
	}
	return s.Repository.Planets(ctx, principal.AccountID, s.Clock.Now().UTC(), s.Catalogue)
}

func (s Service) settleDueEvents(ctx context.Context) error {
	if s.Completer == nil {
		return nil
	}
	_, err := s.Completer.CompleteDue(ctx, 100)
	return err
}

func (s Service) Buildings(ctx context.Context, principal appauth.Principal, planetID int64) (Planet, []BuildingChoice, error) {
	planet, err := s.Planet(ctx, principal, planetID)
	if err != nil {
		return Planet{}, nil, err
	}
	choices := make([]BuildingChoice, 0, len(s.Catalogue.DefinitionsFor(planet.Kind)))
	// The page plans exactly as the order will: on top of what the queue
	// already reaches, and on the fields it has already booked.
	projected := planet.ProjectedLevels()
	requirements := prerequisite.State{
		Buildings:  projected.Generic(),
		Researches: planet.Researches.Generic(),
	}
	for _, definition := range s.Catalogue.DefinitionsFor(planet.Kind) {
		choice := BuildingChoice{Definition: definition, Level: planet.Levels[definition.ID]}
		choice.Requirements = resolveRequirements(definition.Prerequisites, requirements)
		choice.Missing = prerequisite.Unmet(definition.Prerequisites, requirements)
		choice.FacilityBusy = busy(planet.BusyFacilities, definition.ID)
		plan, planErr := s.Catalogue.Plan(definition.ID, planet.Kind, projected, planet.Researches.Generic(), planet.BookedFields(), planet.TotalFields, planet.Rules)
		if planErr == nil {
			choice.Plan = plan
			// The order will be refused for a facility another queue is using, so
			// the card must not offer it. Everything else about the plan stands:
			// the cost and the duration are what the level will ask once it is free.
			choice.Available = !choice.FacilityBusy && len(planet.Queue) < planet.Rules.Progression.QueueLength
			choice.Affordable = planet.Stock.Covers(plan.Cost)
		} else if len(choice.Missing) == 0 {
			choice.Refusal = planErr
		}
		choices = append(choices, choice)
	}
	return planet, choices, nil
}

// resolveRequirements pairs every prerequisite with the level the body reaches,
// which is what tells "one level short" from "not started".
func resolveRequirements(prerequisites []prerequisite.Requirement, state prerequisite.State) []Requirement {
	if len(prerequisites) == 0 {
		return nil
	}
	resolved := make([]Requirement, 0, len(prerequisites))
	for _, requirement := range prerequisites {
		resolved = append(resolved, Requirement{
			Requirement: requirement,
			Reached:     state.Level(requirement.Kind, requirement.ID),
		})
	}
	return resolved
}

func busy(facilities []building.ID, id building.ID) bool {
	for _, facility := range facilities {
		if facility == id {
			return true
		}
	}
	return false
}

// EnqueueBuilding adds one construction at the end of the body's queue. Its
// cost is taken immediately, so an order that reached the queue can never stall
// for want of resources.
func (s Service) EnqueueBuilding(ctx context.Context, principal appauth.Principal, planetID int64, id building.ID, idempotencyKey string) (Queue, error) {
	if err := s.validatePrincipal(principal); err != nil {
		return Queue{}, err
	}
	if planetID <= 0 || strings.TrimSpace(idempotencyKey) == "" || len(idempotencyKey) > 128 {
		return Queue{}, ErrInvalidRequest
	}
	queue, err := s.Repository.EnqueueBuilding(ctx, principal.AccountID, planetID, id, idempotencyKey, s.Clock.Now().UTC(), s.Catalogue)
	if err == nil && s.Wake != nil {
		s.Wake()
	}
	return queue, err
}

// CancelBuilding drops one order and every level of the same building queued
// above it, refunding all of them. The player has already lost the time; taking
// the resources too would only make a queue a trap.
func (s Service) CancelBuilding(ctx context.Context, principal appauth.Principal, planetID, entryID int64) (Cancellation, error) {
	if err := s.validatePrincipal(principal); err != nil {
		return Cancellation{}, err
	}
	if planetID <= 0 || entryID <= 0 {
		return Cancellation{}, ErrInvalidRequest
	}
	if err := s.settleDueEvents(ctx); err != nil {
		return Cancellation{}, err
	}
	cancellation, err := s.Repository.CancelBuilding(ctx, principal.AccountID, planetID, entryID, s.Clock.Now().UTC(), s.Catalogue)
	if err == nil && s.Wake != nil {
		s.Wake()
	}
	return cancellation, err
}

func (s Service) validatePrincipal(principal appauth.Principal) error {
	if s.Clock == nil || s.Repository == nil {
		return errors.New("economy: incomplete service dependencies")
	}
	if principal.AccountID <= 0 || principal.MustChangePassword {
		return ErrForbidden
	}
	return nil
}
