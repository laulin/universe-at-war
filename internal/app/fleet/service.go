// Package fleet orchestrates fleet missions: launch, arrival, return and recall.
package fleet

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
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/unit"
	"universeatwar/internal/domain/universe"
)

var (
	ErrForbidden      = errors.New("fleet: authenticated account required")
	ErrInvalidRequest = errors.New("fleet: invalid fleet request")
	ErrNotRecallable  = errors.New("fleet: this fleet can no longer be recalled")
	ErrConflict       = errors.New("fleet: the fleet changed before the command was applied")
	ErrNotFound       = errors.New("fleet: no such fleet for this account")
)

// LaunchRequest is a mission a player wants to start.
type LaunchRequest struct {
	Target      universe.Coordinate
	TargetKind  domainfleet.TargetKind
	Mission     domainfleet.Mission
	Composition domainfleet.Composition
	Cargo       economy.Resources
	Percent     int
	HoldUntil   time.Time
}

// Fleet is the projection of one mission.
type Fleet struct {
	ID           int64
	Mission      domainfleet.Mission
	State        domainfleet.State
	Origin       universe.Coordinate
	Target       universe.Coordinate
	TargetKind   domainfleet.TargetKind
	OriginID     int64
	Composition  domainfleet.Composition
	Cargo        economy.Resources
	SpeedPercent int
	Fuel         int64
	DepartedAt   time.Time
	ArrivesAt    time.Time
	HoldsUntil   *time.Time
	ReturnsAt    *time.Time
	RecalledAt   *time.Time
	Recallable   bool
}

// Overview is the fleet page projection of one planet.
type Overview struct {
	Planet    appeconomy.Planet
	Stationed unit.Inventory
	Slots     int
	Used      int
	Fleets    []Fleet
}

// Seeds draws the seed persisted with a mission, which makes every later
// probabilistic resolution replayable.
type Seeds interface {
	Seed() (int64, error)
}

// Repository is the atomic persistence boundary for fleet use cases.
type Repository interface {
	Overview(context.Context, int64, int64, time.Time) (Overview, error)
	Preview(context.Context, int64, int64, LaunchRequest, time.Time) (domainfleet.Plan, error)
	Launch(context.Context, int64, int64, LaunchRequest, int64, string, time.Time) (Fleet, error)
	Recall(context.Context, int64, int64, string, time.Time) (Fleet, error)
}

// Service runs the fleet use cases of one player.
type Service struct {
	Clock      domainclock.Clock
	Repository Repository
	Catalogues catalogue.Set
	Seeds      Seeds
	Completer  appeconomy.Completer
	Wake       func()
}

// Overview returns the fleet page of one planet.
func (s Service) Overview(ctx context.Context, principal appauth.Principal, planetID int64) (Overview, error) {
	if err := s.validate(principal); err != nil {
		return Overview{}, err
	}
	if s.Completer != nil {
		if _, err := s.Completer.CompleteDue(ctx, 100); err != nil {
			return Overview{}, err
		}
	}
	return s.Repository.Overview(ctx, principal.AccountID, planetID, s.Clock.Now().UTC())
}

// Preview calculates a mission without launching it.
func (s Service) Preview(ctx context.Context, principal appauth.Principal, planetID int64, request LaunchRequest) (domainfleet.Plan, error) {
	if err := s.validate(principal); err != nil {
		return domainfleet.Plan{}, err
	}
	if planetID <= 0 {
		return domainfleet.Plan{}, ErrInvalidRequest
	}
	return s.Repository.Preview(ctx, principal.AccountID, planetID, request, s.Clock.Now().UTC())
}

// Launch sends a fleet. A fleet that has left is committed.
func (s Service) Launch(ctx context.Context, principal appauth.Principal, planetID int64, request LaunchRequest, idempotencyKey string) (Fleet, error) {
	if err := s.validate(principal); err != nil {
		return Fleet{}, err
	}
	if planetID <= 0 || strings.TrimSpace(idempotencyKey) == "" || len(idempotencyKey) > 128 {
		return Fleet{}, ErrInvalidRequest
	}
	if s.Seeds == nil {
		return Fleet{}, errors.New("fleet: no seed source")
	}
	seed, err := s.Seeds.Seed()
	if err != nil {
		return Fleet{}, err
	}
	fleet, err := s.Repository.Launch(ctx, principal.AccountID, planetID, request, seed, idempotencyKey, s.Clock.Now().UTC())
	if err == nil && s.Wake != nil {
		s.Wake()
	}
	return fleet, err
}

// Recall calls a fleet back home when its mission still allows it.
func (s Service) Recall(ctx context.Context, principal appauth.Principal, fleetID int64, idempotencyKey string) (Fleet, error) {
	if err := s.validate(principal); err != nil {
		return Fleet{}, err
	}
	if fleetID <= 0 || strings.TrimSpace(idempotencyKey) == "" || len(idempotencyKey) > 128 {
		return Fleet{}, ErrInvalidRequest
	}
	fleet, err := s.Repository.Recall(ctx, principal.AccountID, fleetID, idempotencyKey, s.Clock.Now().UTC())
	if err == nil && s.Wake != nil {
		s.Wake()
	}
	return fleet, err
}

func (s Service) validate(principal appauth.Principal) error {
	if s.Clock == nil || s.Repository == nil {
		return errors.New("fleet: incomplete service dependencies")
	}
	if principal.AccountID <= 0 || principal.MustChangePassword {
		return ErrForbidden
	}
	return nil
}
