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
	ErrNoEmpire       = errors.New("economy: account has no empire")
	ErrPlanetNotFound = errors.New("economy: planet does not belong to this account")
	ErrUniverseFull   = errors.New("economy: universe has no free position")
	ErrQueueBusy      = errors.New("economy: the construction queue changed under this order")
	ErrQueueFull      = errors.New("economy: the construction queue is full")
	ErrFacilityBusy   = errors.New("economy: the facility is in use by another activity")
	ErrInvalidName    = errors.New("economy: player name must contain 3 to 32 characters")
	ErrInvalidRequest = errors.New("economy: invalid construction request")
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
	Rules rules.Ruleset
}

// BuildingChoice is one catalogue entry enriched for a planet.
type BuildingChoice struct {
	Definition building.Definition
	Level      int
	Plan       building.Plan
	Available  bool
	Affordable bool
	// Missing names the prerequisites the planet has not met, so the caller can
	// say so in its own words instead of showing a raw error.
	Missing []prerequisite.Requirement
	Reason  string
}

// Repository is the atomic persistence boundary for economic use cases.
type Repository interface {
	CreateEmpire(context.Context, int64, string, time.Time) (Planet, error)
	Planet(context.Context, int64, int64, time.Time, building.Catalogue) (Planet, error)
	Planets(context.Context, int64, time.Time, building.Catalogue) ([]Planet, error)
	EnqueueBuilding(context.Context, int64, int64, building.ID, string, time.Time, building.Catalogue) (Queue, error)
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

func (s Service) CreateEmpire(ctx context.Context, principal appauth.Principal, name string) (Planet, error) {
	if err := s.validatePrincipal(principal); err != nil {
		return Planet{}, err
	}
	name = strings.TrimSpace(name)
	if length := len([]rune(name)); length < 3 || length > 32 {
		return Planet{}, ErrInvalidName
	}
	return s.Repository.CreateEmpire(ctx, principal.AccountID, name, s.Clock.Now().UTC())
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
	requirements := prerequisite.State{
		Buildings:  planet.Levels.Generic(),
		Researches: planet.Researches.Generic(),
	}
	for _, definition := range s.Catalogue.DefinitionsFor(planet.Kind) {
		choice := BuildingChoice{Definition: definition, Level: planet.Levels[definition.ID]}
		choice.Missing = prerequisite.Unmet(definition.Prerequisites, requirements)
		plan, planErr := s.Catalogue.Plan(definition.ID, planet.Kind, planet.Levels, planet.Researches.Generic(), planet.UsedFields, planet.TotalFields, planet.Rules)
		if planErr == nil {
			choice.Plan = plan
			choice.Available = len(planet.Queue) < planet.Rules.Progression.QueueLength
			choice.Affordable = planet.Stock.Covers(plan.Cost)
		} else if len(choice.Missing) == 0 {
			choice.Reason = planErr.Error()
		}
		choices = append(choices, choice)
	}
	return planet, choices, nil
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

func (s Service) validatePrincipal(principal appauth.Principal) error {
	if s.Clock == nil || s.Repository == nil {
		return errors.New("economy: incomplete service dependencies")
	}
	if principal.AccountID <= 0 || principal.MustChangePassword {
		return ErrForbidden
	}
	return nil
}
