// Package acs orchestrates grouped operations: several fleets of one alliance
// striking the same target at the same second.
package acs

import (
	"context"
	"errors"
	"strings"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appeconomy "universeatwar/internal/app/economy"
	appfleet "universeatwar/internal/app/fleet"
	domainacs "universeatwar/internal/domain/acs"
	domainclock "universeatwar/internal/domain/clock"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/universe"
)

var (
	ErrForbidden      = errors.New("acs: this account cannot do that")
	ErrDisabled       = errors.New("acs: grouped operations are disabled in this universe")
	ErrNotFound       = errors.New("acs: no such operation")
	ErrInvalidRequest = errors.New("acs: invalid operation request")
)

// Participant is one fleet engaged in the operation.
type Participant struct {
	FleetID     int64
	PlayerName  string
	Own         bool
	Origin      universe.Coordinate
	Composition domainfleet.Composition
	Ships       int64
	JoinedAt    time.Time
}

// Group is one grouped operation.
type Group struct {
	ID           int64
	OwnerName    string
	Kind         domainacs.Kind
	Target       universe.Coordinate
	State        domainacs.State
	ArrivesAt    time.Time
	Participants []Participant
	MaximumSize  int
	Own          bool
}

// Preview is what joining an operation would do to its schedule, shown before
// the player commits to it.
type Preview struct {
	Group         Group
	Plan          domainfleet.Plan
	GroupArrival  time.Time
	DelaysTheTeam bool
}

// Repository is the atomic persistence boundary of grouped operations.
type Repository interface {
	Groups(context.Context, int64, time.Time) ([]Group, error)
	Group(context.Context, int64, int64, time.Time) (Group, error)
	Create(context.Context, int64, int64, appfleet.LaunchRequest, int64, string, time.Time) (Group, error)
	Preview(context.Context, int64, int64, int64, appfleet.LaunchRequest, time.Time) (Preview, error)
	Join(context.Context, int64, int64, int64, appfleet.LaunchRequest, string, time.Time) (Group, error)
	Withdraw(context.Context, int64, int64, string, time.Time) error
}

// Service runs the grouped operation use cases of one account.
type Service struct {
	Clock      domainclock.Clock
	Repository Repository
	Seeds      appfleet.Seeds
	Completer  appeconomy.Completer
	Wake       func()
}

// Groups lists the operations of the alliance of the account.
func (s Service) Groups(ctx context.Context, principal appauth.Principal) ([]Group, error) {
	if err := s.validate(principal); err != nil {
		return nil, err
	}
	if s.Completer != nil {
		if _, err := s.Completer.CompleteDue(ctx, 100); err != nil {
			return nil, err
		}
	}
	return s.Repository.Groups(ctx, principal.AccountID, s.Clock.Now().UTC())
}

// Group returns one operation of the alliance of the account.
func (s Service) Group(ctx context.Context, principal appauth.Principal, groupID int64) (Group, error) {
	if err := s.validate(principal); err != nil {
		return Group{}, err
	}
	if groupID <= 0 {
		return Group{}, ErrNotFound
	}
	return s.Repository.Group(ctx, principal.AccountID, groupID, s.Clock.Now().UTC())
}

// Create opens an operation and engages its first fleet.
func (s Service) Create(ctx context.Context, principal appauth.Principal, planetID int64, request appfleet.LaunchRequest, idempotencyKey string) (Group, error) {
	if err := s.validate(principal); err != nil {
		return Group{}, err
	}
	if planetID <= 0 || strings.TrimSpace(idempotencyKey) == "" || len(idempotencyKey) > 128 {
		return Group{}, ErrInvalidRequest
	}
	if s.Seeds == nil {
		return Group{}, errors.New("acs: no seed source")
	}
	seed, err := s.Seeds.Seed()
	if err != nil {
		return Group{}, err
	}
	group, err := s.Repository.Create(ctx, principal.AccountID, planetID, request, seed, idempotencyKey, s.Clock.Now().UTC())
	if err == nil && s.Wake != nil {
		s.Wake()
	}
	return group, err
}

// Preview shows what a fleet would do to the schedule of an operation.
func (s Service) Preview(ctx context.Context, principal appauth.Principal, groupID, planetID int64, request appfleet.LaunchRequest) (Preview, error) {
	if err := s.validate(principal); err != nil {
		return Preview{}, err
	}
	if groupID <= 0 || planetID <= 0 {
		return Preview{}, ErrInvalidRequest
	}
	return s.Repository.Preview(ctx, principal.AccountID, groupID, planetID, request, s.Clock.Now().UTC())
}

// Join engages one more fleet, delaying the operation if it is slower.
func (s Service) Join(ctx context.Context, principal appauth.Principal, groupID, planetID int64, request appfleet.LaunchRequest, idempotencyKey string) (Group, error) {
	if err := s.validate(principal); err != nil {
		return Group{}, err
	}
	if groupID <= 0 || planetID <= 0 || strings.TrimSpace(idempotencyKey) == "" || len(idempotencyKey) > 128 {
		return Group{}, ErrInvalidRequest
	}
	group, err := s.Repository.Join(ctx, principal.AccountID, groupID, planetID, request, idempotencyKey, s.Clock.Now().UTC())
	if err == nil && s.Wake != nil {
		s.Wake()
	}
	return group, err
}

// Withdraw recalls one's own fleet out of an operation.
func (s Service) Withdraw(ctx context.Context, principal appauth.Principal, fleetID int64, idempotencyKey string) error {
	if err := s.validate(principal); err != nil {
		return err
	}
	if fleetID <= 0 || strings.TrimSpace(idempotencyKey) == "" || len(idempotencyKey) > 128 {
		return ErrInvalidRequest
	}
	err := s.Repository.Withdraw(ctx, principal.AccountID, fleetID, idempotencyKey, s.Clock.Now().UTC())
	if err == nil && s.Wake != nil {
		s.Wake()
	}
	return err
}

func (s Service) validate(principal appauth.Principal) error {
	if s.Clock == nil || s.Repository == nil {
		return errors.New("acs: incomplete service dependencies")
	}
	if principal.AccountID <= 0 || principal.MustChangePassword {
		return ErrForbidden
	}
	return nil
}
