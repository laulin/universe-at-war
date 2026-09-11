// Package activity exposes the compact empire status used by persistent game
// chrome and by pages that need to warn a player about hostile traffic.
package activity

import (
	"context"
	"errors"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appeconomy "universeatwar/internal/app/economy"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/universe"
)

var ErrForbidden = errors.New("activity: authenticated account required")

// Body is the set of compact counters displayed next to one celestial body.
// Production counts are remaining units rather than queue rows, so a batch of
// ten fighters is shown as ten and keeps decreasing as units are delivered.
type Body struct {
	PlanetID        int64
	Buildings       int64
	Researches      int64
	Ships           int64
	Defenses        int64
	OutboundFleets  int64
	IncomingAttacks int64
}

// Approach is one enemy attack currently flying towards a body of the player.
// Its composition is intentionally visible to the defender: the fleet page is
// the tactical warning surface, not an intelligence report about a third party.
type Approach struct {
	FleetID        int64
	TargetPlanetID int64
	AttackerName   string
	Origin         universe.Coordinate
	Target         universe.Coordinate
	Composition    domainfleet.Composition
	ArrivesAt      time.Time
}

// Snapshot is one consistent reading of all activity visible to an account.
type Snapshot struct {
	Bodies   map[int64]Body
	Incoming []Approach
}

// Repository is the read boundary for the empire activity projection.
type Repository interface {
	Snapshot(context.Context, int64) (Snapshot, error)
}

// Service settles due events before exposing the current activity counters.
type Service struct {
	Repository Repository
	Completer  appeconomy.Completer
}

// Snapshot returns activity for the authenticated player's bodies only.
func (s Service) Snapshot(ctx context.Context, principal appauth.Principal) (Snapshot, error) {
	if s.Repository == nil {
		return Snapshot{}, errors.New("activity: incomplete service dependencies")
	}
	if principal.AccountID <= 0 || principal.MustChangePassword {
		return Snapshot{}, ErrForbidden
	}
	if s.Completer != nil {
		if _, err := s.Completer.CompleteDue(ctx, 100); err != nil {
			return Snapshot{}, err
		}
	}
	return s.Repository.Snapshot(ctx, principal.AccountID)
}
