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
	"universeatwar/internal/domain/rules"
	"universeatwar/internal/domain/universe"
)

var (
	ErrForbidden      = errors.New("economy: authenticated account required")
	ErrEmpireExists   = errors.New("economy: account already owns an empire")
	ErrNoEmpire       = errors.New("economy: account has no empire")
	ErrUniverseFull   = errors.New("economy: universe has no free position")
	ErrQueueBusy      = errors.New("economy: a construction is already active")
	ErrInvalidName    = errors.New("economy: player name must contain 3 to 32 characters")
	ErrInvalidRequest = errors.New("economy: invalid construction request")
)

// Queue is the active or completed construction projection.
type Queue struct {
	ID          int64
	Building    building.ID
	TargetLevel int
	Cost        economy.Resources
	StartedAt   time.Time
	CompletesAt time.Time
	State       string
}

// Planet is the complete economic projection returned after lazy settlement.
type Planet struct {
	ID                 int64
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
	ActiveQueue        *Queue
	Rules              rules.Ruleset
}

// BuildingChoice is one catalogue entry enriched for a planet.
type BuildingChoice struct {
	Definition building.Definition
	Level      int
	Plan       building.Plan
	Available  bool
	Affordable bool
	Reason     string
}

// Repository is the atomic persistence boundary for economic use cases.
type Repository interface {
	CreateEmpire(context.Context, int64, string, time.Time) (Planet, error)
	Planet(context.Context, int64, time.Time, building.Catalogue) (Planet, error)
	StartConstruction(context.Context, int64, int64, building.ID, string, time.Time, building.Catalogue) (Queue, error)
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

func (s Service) Planet(ctx context.Context, principal appauth.Principal) (Planet, error) {
	if err := s.validatePrincipal(principal); err != nil {
		return Planet{}, err
	}
	if s.Completer != nil {
		if _, err := s.Completer.CompleteDue(ctx, 100); err != nil {
			return Planet{}, err
		}
	}
	return s.Repository.Planet(ctx, principal.AccountID, s.Clock.Now().UTC(), s.Catalogue)
}

func (s Service) Buildings(ctx context.Context, principal appauth.Principal) (Planet, []BuildingChoice, error) {
	planet, err := s.Planet(ctx, principal)
	if err != nil {
		return Planet{}, nil, err
	}
	choices := make([]BuildingChoice, 0, len(s.Catalogue.Definitions()))
	for _, definition := range s.Catalogue.Definitions() {
		choice := BuildingChoice{Definition: definition, Level: planet.Levels[definition.ID]}
		plan, planErr := s.Catalogue.Plan(definition.ID, planet.Levels, planet.UsedFields, planet.TotalFields, planet.Rules)
		if planErr == nil {
			choice.Plan = plan
			choice.Available = planet.ActiveQueue == nil
			choice.Affordable = planet.Stock.Covers(plan.Cost)
		} else {
			choice.Reason = planErr.Error()
		}
		choices = append(choices, choice)
	}
	return planet, choices, nil
}

func (s Service) StartConstruction(ctx context.Context, principal appauth.Principal, planetID int64, id building.ID, idempotencyKey string) (Queue, error) {
	if err := s.validatePrincipal(principal); err != nil {
		return Queue{}, err
	}
	if planetID <= 0 || strings.TrimSpace(idempotencyKey) == "" || len(idempotencyKey) > 128 {
		return Queue{}, ErrInvalidRequest
	}
	queue, err := s.Repository.StartConstruction(ctx, principal.AccountID, planetID, id, idempotencyKey, s.Clock.Now().UTC(), s.Catalogue)
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
