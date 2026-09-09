// Package research orchestrates the technology progression of a player.
package research

import (
	"context"
	"errors"
	"strings"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appeconomy "universeatwar/internal/app/economy"
	"universeatwar/internal/domain/building"
	"universeatwar/internal/domain/catalogue"
	domainclock "universeatwar/internal/domain/clock"
	"universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/prerequisite"
	"universeatwar/internal/domain/research"
)

var (
	ErrForbidden          = errors.New("research: authenticated account required")
	ErrQueueBusy          = errors.New("research: the research queue changed under this order")
	ErrQueueFull          = errors.New("research: the research queue is full")
	ErrLaboratoryBusy     = errors.New("research: the laboratory is being upgraded")
	ErrInsufficientEnergy = errors.New("research: available energy is too low")
	ErrInvalidRequest     = errors.New("research: invalid research request")
	ErrQueueEntryNotFound = errors.New("research: no such research in the queue")
)

// Queue is one entry of a player's research queue. The entry at position zero
// is the one running and carries a real schedule; the ones behind it only carry
// the forecast of when their turn comes.
type Queue struct {
	ID                   int64
	PlanetID             int64
	Research             research.ID
	TargetLevel          int
	Cost                 economy.Resources
	Energy               int64
	Position             int
	StartedAt            time.Time
	CompletesAt          time.Time
	EstimatedStartAt     time.Time
	EstimatedCompletesAt time.Time
	State                string
}

// Waiting reports an entry that has not started yet.
func (q Queue) Waiting() bool { return q.State == "queued" }

// State is everything the repository knows about a player's research.
type State struct {
	Planet       appeconomy.Planet
	Levels       research.Levels
	Laboratories research.Laboratories
	// Queue holds every research ordered by the player and not yet finished,
	// head first. It belongs to the empire, not to one planet.
	Queue []Queue
}

// ProjectedLevels applies the whole queue to the known levels, so the page
// offers the level after the queue rather than one the queue already reaches.
func (s State) ProjectedLevels() research.Levels {
	projected := research.Levels{}
	for id, level := range s.Levels {
		projected[id] = level
	}
	for _, entry := range s.Queue {
		projected[entry.Research] = entry.TargetLevel
	}
	return projected
}

// Choice is one catalogue entry enriched for the player.
type Choice struct {
	Definition research.Definition
	Level      int
	Plan       research.Plan
	Available  bool
	Affordable bool
	// EnergyShort separates the two ways of not affording a research. A store
	// fills on its own and a page may wait for it; the energy balance does not.
	EnergyShort bool
	// Requirements lists every prerequisite of the entry, met or not, against
	// the levels the queue is going to reach.
	Requirements []prerequisite.Resolved
	Missing      []prerequisite.Requirement
	// LaboratoryBusy says the laboratory of this planet sits in its building
	// queue. The order refuses a research while it does, so a page that ignored
	// it would offer a button whose only outcome is a refusal.
	LaboratoryBusy bool
	// Refusal is why the plan could not be calculated at all, kept as the error
	// it is rather than as its text: only the caller can say it in the player's
	// language, and only from a value it can recognise.
	Refusal error
}

// Overview is the research page projection.
type Overview struct {
	State
	Choices []Choice
}

// Repository is the atomic persistence boundary for research use cases.
type Repository interface {
	State(context.Context, int64, int64, time.Time) (State, error)
	EnqueueResearch(context.Context, int64, int64, research.ID, string, time.Time) (Queue, error)
	CancelResearch(context.Context, int64, int64, int64, time.Time) (appeconomy.Cancellation, error)
}

// Service runs the research use cases of one player.
type Service struct {
	Clock      domainclock.Clock
	Repository Repository
	Catalogues catalogue.Set
	Completer  appeconomy.Completer
	Wake       func()
}

// Overview returns the research page of one planet.
func (s Service) Overview(ctx context.Context, principal appauth.Principal, planetID int64) (Overview, error) {
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
	definitions := s.Catalogues.Research.Definitions()
	choices := make([]Choice, 0, len(definitions))
	// The page plans exactly as the order will: on top of what the queue
	// already reaches.
	projected := state.ProjectedLevels()
	requirements := prerequisite.State{
		Buildings:  state.Planet.Levels.Generic(),
		Researches: projected.Generic(),
	}
	available := len(state.Queue) < state.Planet.Rules.Progression.QueueLength
	laboratoryBusy := laboratoryIsQueued(state.Planet)
	for _, definition := range definitions {
		choice := Choice{Definition: definition, Level: state.Levels[definition.ID]}
		choice.Requirements = prerequisite.Resolve(definition.Prerequisites, requirements)
		choice.Missing = prerequisite.Unmet(definition.Prerequisites, requirements)
		choice.LaboratoryBusy = laboratoryBusy
		plan, err := s.Catalogues.Research.Plan(definition.ID, requirements, state.Laboratories, state.Planet.Rules)
		if err == nil {
			choice.Plan = plan
			// The order refuses every research while the laboratory is queued,
			// so no card may offer one. The plan itself still stands: its price
			// and its duration are what the research will ask once the
			// laboratory is out of the queue.
			choice.Available = available && !laboratoryBusy
			choice.EnergyShort = availableEnergy(state.Planet) < plan.Energy
			choice.Affordable = state.Planet.Stock.Covers(plan.Cost) && !choice.EnergyShort
		} else if len(choice.Missing) == 0 {
			choice.Refusal = err
		}
		choices = append(choices, choice)
	}
	return Overview{State: state, Choices: choices}, nil
}

// laboratoryIsQueued reports the laboratory of the planet sitting anywhere in
// its building queue, running or waiting, which is the exclusion the order
// applies. The building queue travels with the planet, so the page reads the
// same thing the transaction will.
func laboratoryIsQueued(planet appeconomy.Planet) bool {
	for _, entry := range planet.Queue {
		if entry.Building == building.ResearchLab {
			return true
		}
	}
	return false
}

// EnqueueResearch adds one research at the end of the player's queue. Its cost
// is taken immediately, so an order that reached the queue is already paid for.
func (s Service) EnqueueResearch(ctx context.Context, principal appauth.Principal, planetID int64, id research.ID, idempotencyKey string) (Queue, error) {
	if err := s.validate(principal); err != nil {
		return Queue{}, err
	}
	if planetID <= 0 || strings.TrimSpace(idempotencyKey) == "" || len(idempotencyKey) > 128 {
		return Queue{}, ErrInvalidRequest
	}
	queue, err := s.Repository.EnqueueResearch(ctx, principal.AccountID, planetID, id, idempotencyKey, s.Clock.Now().UTC())
	if err == nil && s.Wake != nil {
		s.Wake()
	}
	return queue, err
}

// CancelResearch drops one order and every level of the same technology queued
// above it, refunding all of them to the planet that paid.
func (s Service) CancelResearch(ctx context.Context, principal appauth.Principal, planetID, entryID int64) (appeconomy.Cancellation, error) {
	if err := s.validate(principal); err != nil {
		return appeconomy.Cancellation{}, err
	}
	if planetID <= 0 || entryID <= 0 {
		return appeconomy.Cancellation{}, ErrInvalidRequest
	}
	if s.Completer != nil {
		if _, err := s.Completer.CompleteDue(ctx, 100); err != nil {
			return appeconomy.Cancellation{}, err
		}
	}
	cancellation, err := s.Repository.CancelResearch(ctx, principal.AccountID, planetID, entryID, s.Clock.Now().UTC())
	if err == nil && s.Wake != nil {
		s.Wake()
	}
	return cancellation, err
}

// availableEnergy is the surplus a planet can lend to a research.
func availableEnergy(planet appeconomy.Planet) int64 {
	surplus := planet.Energy.Produced - planet.Energy.Consumed
	if surplus < 0 {
		return 0
	}
	return surplus
}

func (s Service) validate(principal appauth.Principal) error {
	if s.Clock == nil || s.Repository == nil {
		return errors.New("research: incomplete service dependencies")
	}
	if principal.AccountID <= 0 || principal.MustChangePassword {
		return ErrForbidden
	}
	return nil
}
