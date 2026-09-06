// Package phalanx turns a lunar sensor into the timing information a player can
// act on, and nothing more.
package phalanx

import (
	"context"
	"errors"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appeconomy "universeatwar/internal/app/economy"
	domainclock "universeatwar/internal/domain/clock"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/universe"
)

var (
	ErrForbidden = errors.New("phalanx: authenticated account required")
	ErrNotAMoon  = errors.New("phalanx: a phalanx only stands on a moon")
)

// Sighting is one mission the sensor can time. It deliberately holds no
// composition, no cargo and no owner: a phalanx reads masses, not manifests.
type Sighting struct {
	Mission   domainfleet.Mission
	Origin    universe.Coordinate
	Target    universe.Coordinate
	ArrivesAt time.Time
	ReturnsAt *time.Time
	Ships     int64
}

// Scan is the result of one sweep.
type Scan struct {
	Moon      appeconomy.Planet
	Level     int
	Radius    int
	Cost      int64
	Target    universe.Coordinate
	Sightings []Sighting
}

// Repository performs the sweep and its payment atomically.
type Repository interface {
	Scan(context.Context, int64, int64, universe.Coordinate, time.Time) (Scan, error)
}

// Service runs a sweep for one player.
type Service struct {
	Clock      domainclock.Clock
	Repository Repository
	Completer  appeconomy.Completer
}

// Scan sweeps one position from one moon. The deuterium is spent whether or not
// the sweep finds anything.
func (s Service) Scan(ctx context.Context, principal appauth.Principal, moonID int64, target universe.Coordinate) (Scan, error) {
	if s.Clock == nil || s.Repository == nil {
		return Scan{}, errors.New("phalanx: incomplete service dependencies")
	}
	if principal.AccountID <= 0 || principal.MustChangePassword {
		return Scan{}, ErrForbidden
	}
	if moonID <= 0 {
		return Scan{}, appeconomy.ErrPlanetNotFound
	}
	if s.Completer != nil {
		if _, err := s.Completer.CompleteDue(ctx, 100); err != nil {
			return Scan{}, err
		}
	}
	return s.Repository.Scan(ctx, principal.AccountID, moonID, target, s.Clock.Now().UTC())
}
