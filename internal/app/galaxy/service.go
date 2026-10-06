// Package galaxy exposes the public map of the universe. It never reads the
// resources, the buildings or the fleets of another player.
package galaxy

import (
	"context"
	"errors"

	appauth "universeatwar/internal/app/authentication"
	"universeatwar/internal/domain/debris"
	"universeatwar/internal/domain/universe"
)

var (
	ErrForbidden       = errors.New("galaxy: authenticated account required")
	ErrOutsideUniverse = errors.New("galaxy: this system does not exist")
)

// Row is one position of a system, as everybody may see it.
type Row struct {
	Position      int
	PlanetID      int64
	PlanetName    string
	OwnerPlayerID int64
	OwnerName     string
	Own           bool
	Debris        *debris.Field
}

// View is one system of the map.
type View struct {
	Galaxy      int
	System      int
	Limits      universe.Limits
	Rows        []Row
	HomePlanets []int64
}

// Repository reads the public map.
type Repository interface {
	System(context.Context, int64, int, int) (View, error)
}

// Service serves the public map to one player.
type Service struct {
	Repository Repository
}

// System returns one system of the map.
func (s Service) System(ctx context.Context, principal appauth.Principal, galaxy, system int) (View, error) {
	if s.Repository == nil {
		return View{}, errors.New("galaxy: incomplete service dependencies")
	}
	if principal.AccountID <= 0 || principal.MustChangePassword {
		return View{}, ErrForbidden
	}
	return s.Repository.System(ctx, principal.AccountID, galaxy, system)
}
