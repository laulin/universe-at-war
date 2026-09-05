// Package research orchestrates the technology progression of a player.
package research

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
	"universeatwar/internal/domain/research"
)

var (
	ErrForbidden          = errors.New("research: authenticated account required")
	ErrQueueBusy          = errors.New("research: a research is already running")
	ErrLaboratoryBusy     = errors.New("research: the laboratory is being upgraded")
	ErrInsufficientEnergy = errors.New("research: available energy is too low")
	ErrInvalidRequest     = errors.New("research: invalid research request")
)

// Queue is the active or completed research projection.
type Queue struct {
	ID          int64
	PlanetID    int64
	Research    research.ID
	TargetLevel int
	Cost        economy.Resources
	Energy      int64
	StartedAt   time.Time
	CompletesAt time.Time
	State       string
}

// State is everything the repository knows about a player's research.
type State struct {
	Planet       appeconomy.Planet
	Levels       research.Levels
	Laboratories research.Laboratories
	Active       *Queue
}

// Choice is one catalogue entry enriched for the player.
type Choice struct {
	Definition research.Definition
	Level      int
	Plan       research.Plan
	Available  bool
	Affordable bool
	Missing    []prerequisite.Requirement
	Reason     string
}

// Overview is the research page projection.
type Overview struct {
	State
	Choices []Choice
}

// Repository is the atomic persistence boundary for research use cases.
type Repository interface {
	State(context.Context, int64, int64, time.Time) (State, error)
	Start(context.Context, int64, int64, research.ID, string, time.Time) (Queue, error)
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
	requirements := prerequisite.State{
		Buildings:  state.Planet.Levels.Generic(),
		Researches: state.Levels.Generic(),
	}
	available := state.Active == nil
	for _, definition := range definitions {
		choice := Choice{Definition: definition, Level: state.Levels[definition.ID]}
		choice.Missing = prerequisite.Unmet(definition.Prerequisites, requirements)
		plan, err := s.Catalogues.Research.Plan(definition.ID, requirements, state.Laboratories, state.Planet.Rules)
		if err == nil {
			choice.Plan = plan
			choice.Available = available
			choice.Affordable = state.Planet.Stock.Covers(plan.Cost) &&
				availableEnergy(state.Planet) >= plan.Energy
		} else if len(choice.Missing) == 0 {
			choice.Reason = err.Error()
		}
		choices = append(choices, choice)
	}
	return Overview{State: state, Choices: choices}, nil
}

// Start launches one research for the player owning the planet.
func (s Service) Start(ctx context.Context, principal appauth.Principal, planetID int64, id research.ID, idempotencyKey string) (Queue, error) {
	if err := s.validate(principal); err != nil {
		return Queue{}, err
	}
	if planetID <= 0 || strings.TrimSpace(idempotencyKey) == "" || len(idempotencyKey) > 128 {
		return Queue{}, ErrInvalidRequest
	}
	queue, err := s.Repository.Start(ctx, principal.AccountID, planetID, id, idempotencyKey, s.Clock.Now().UTC())
	if err == nil && s.Wake != nil {
		s.Wake()
	}
	return queue, err
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
