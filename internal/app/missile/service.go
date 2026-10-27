// Package missile orchestrates missile strikes independently of fleet slots.
package missile

import (
	"context"
	"errors"
	"strings"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appeconomy "universeatwar/internal/app/economy"
	domainclock "universeatwar/internal/domain/clock"
	"universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/unit"
	"universeatwar/internal/domain/universe"
)

var (
	ErrForbidden = errors.New("missile: authenticated account required")
	ErrDisabled  = errors.New("missile: strikes disabled")
	ErrOrigin    = errors.New("missile: launch requires a planet with a missile silo")
	ErrTarget    = errors.New("missile: target must be another player's planet or moon")
	ErrStock     = errors.New("missile: insufficient missiles")
	ErrInvalid   = errors.New("missile: invalid request or idempotency conflict")
)

type Request struct {
	Target     universe.Coordinate
	TargetKind fleet.TargetKind
	Count      int64
	Priority   unit.ID
}

type Plan struct {
	TargetName string
	Duration   time.Duration
	ArrivesAt  time.Time
}

type Strike struct {
	ID         int64
	Origin     universe.Coordinate
	Target     universe.Coordinate
	TargetKind fleet.TargetKind
	Count      int64
	Priority   unit.ID
	ArrivesAt  time.Time
	State      string
	Incoming   bool
}

type Overview struct {
	Planet  appeconomy.Planet
	Strikes []Strike
}

type Repository interface {
	Overview(context.Context, int64, int64, time.Time) (Overview, error)
	Preview(context.Context, int64, int64, Request, time.Time) (Plan, error)
	Launch(context.Context, int64, int64, Request, int64, string, time.Time) (Strike, error)
}

type Seeds interface{ Seed() (int64, error) }

type Service struct {
	Clock      domainclock.Clock
	Repository Repository
	Completer  appeconomy.Completer
	Seeds      Seeds
	Wake       func()
}

func (s Service) prepare(ctx context.Context, principal appauth.Principal) error {
	if principal.AccountID <= 0 || principal.MustChangePassword {
		return ErrForbidden
	}
	if s.Clock == nil || s.Repository == nil {
		return errors.New("missile: incomplete dependencies")
	}
	if s.Completer != nil {
		if _, err := s.Completer.CompleteDue(ctx, 100); err != nil {
			return err
		}
	}
	return nil
}

func (s Service) Overview(ctx context.Context, principal appauth.Principal, planetID int64) (Overview, error) {
	if err := s.prepare(ctx, principal); err != nil {
		return Overview{}, err
	}
	if planetID <= 0 {
		return Overview{}, appeconomy.ErrPlanetNotFound
	}
	return s.Repository.Overview(ctx, principal.AccountID, planetID, s.Clock.Now().UTC())
}

func (s Service) Preview(ctx context.Context, principal appauth.Principal, planetID int64, request Request) (Plan, error) {
	if err := s.prepare(ctx, principal); err != nil {
		return Plan{}, err
	}
	if planetID <= 0 {
		return Plan{}, ErrInvalid
	}
	return s.Repository.Preview(ctx, principal.AccountID, planetID, request, s.Clock.Now().UTC())
}

func (s Service) Launch(ctx context.Context, principal appauth.Principal, planetID int64, request Request, key string) (Strike, error) {
	if err := s.prepare(ctx, principal); err != nil {
		return Strike{}, err
	}
	if planetID <= 0 || strings.TrimSpace(key) == "" || len(key) > 128 {
		return Strike{}, ErrInvalid
	}
	if s.Seeds == nil {
		return Strike{}, errors.New("missile: no seed source")
	}
	seed, err := s.Seeds.Seed()
	if err != nil {
		return Strike{}, err
	}
	strike, err := s.Repository.Launch(ctx, principal.AccountID, planetID, request, seed, key, s.Clock.Now().UTC())
	if err == nil && s.Wake != nil {
		s.Wake()
	}
	return strike, err
}
