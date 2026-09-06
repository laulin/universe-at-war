// Package jumpgate moves ships between two moons of the same player without a
// journey, which a durable cooldown keeps exceptional.
package jumpgate

import (
	"context"
	"errors"
	"strings"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appeconomy "universeatwar/internal/app/economy"
	domainclock "universeatwar/internal/domain/clock"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/unit"
	"universeatwar/internal/domain/universe"
)

var (
	ErrForbidden      = errors.New("jump gate: authenticated account required")
	ErrNotAMoon       = errors.New("jump gate: a gate only stands on a moon")
	ErrNoGate         = errors.New("jump gate: this moon carries no jump gate")
	ErrSameMoon       = errors.New("jump gate: a gate cannot jump to itself")
	ErrCoolingDown    = errors.New("jump gate: a gate is still cooling down")
	ErrInvalidRequest = errors.New("jump gate: invalid jump request")
)

// Gate is one moon a jump may reach.
type Gate struct {
	MoonID     int64
	Name       string
	Coordinate universe.Coordinate
	Level      int
	ReadyAt    time.Time
	Ready      bool
}

// Overview is the jump gate page of one moon.
type Overview struct {
	Moon         appeconomy.Planet
	Level        int
	ReadyAt      time.Time
	Ready        bool
	Stationed    unit.Inventory
	Destinations []Gate
}

// Transfer is what a jump moved.
type Transfer struct {
	FromMoonID  int64
	ToMoonID    int64
	Composition domainfleet.Composition
	ReadyAt     time.Time
}

// Repository performs the jump atomically.
type Repository interface {
	Overview(context.Context, int64, int64, time.Time) (Overview, error)
	Jump(context.Context, int64, int64, int64, domainfleet.Composition, string, time.Time) (Transfer, error)
}

// Service runs the jump gate use cases of one player.
type Service struct {
	Clock      domainclock.Clock
	Repository Repository
	Completer  appeconomy.Completer
}

// Overview returns the gate of one moon and the moons it may reach.
func (s Service) Overview(ctx context.Context, principal appauth.Principal, moonID int64) (Overview, error) {
	if err := s.validate(principal); err != nil {
		return Overview{}, err
	}
	if moonID <= 0 {
		return Overview{}, appeconomy.ErrPlanetNotFound
	}
	if s.Completer != nil {
		if _, err := s.Completer.CompleteDue(ctx, 100); err != nil {
			return Overview{}, err
		}
	}
	return s.Repository.Overview(ctx, principal.AccountID, moonID, s.Clock.Now().UTC())
}

// Jump moves ships from one moon to another. Nothing else travels: a jump is
// not a transport.
func (s Service) Jump(ctx context.Context, principal appauth.Principal, fromMoonID, toMoonID int64, composition domainfleet.Composition, idempotencyKey string) (Transfer, error) {
	if err := s.validate(principal); err != nil {
		return Transfer{}, err
	}
	if fromMoonID <= 0 || toMoonID <= 0 || strings.TrimSpace(idempotencyKey) == "" || len(idempotencyKey) > 128 {
		return Transfer{}, ErrInvalidRequest
	}
	if fromMoonID == toMoonID {
		return Transfer{}, ErrSameMoon
	}
	if len(composition) == 0 {
		return Transfer{}, domainfleet.ErrEmptyComposition
	}
	return s.Repository.Jump(ctx, principal.AccountID, fromMoonID, toMoonID, composition, idempotencyKey, s.Clock.Now().UTC())
}

func (s Service) validate(principal appauth.Principal) error {
	if s.Clock == nil || s.Repository == nil {
		return errors.New("jump gate: incomplete service dependencies")
	}
	if principal.AccountID <= 0 || principal.MustChangePassword {
		return ErrForbidden
	}
	return nil
}
