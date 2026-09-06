// Package moderation holds the sanctions a universe can apply to an account. A
// ban stops somebody from playing; it never touches what they have built.
package moderation

import (
	"context"
	"errors"
	"strings"
	"time"

	appauth "universeatwar/internal/app/authentication"
	domainclock "universeatwar/internal/domain/clock"
)

var (
	ErrForbidden      = errors.New("moderation: this account cannot do that")
	ErrNotFound       = errors.New("moderation: no such account")
	ErrProtected      = errors.New("moderation: this account is out of reach of a moderator")
	ErrInvalidRequest = errors.New("moderation: a ban needs a justification")
	ErrBanNotFound    = errors.New("moderation: no such ban")
)

// MaximumJustification bounds what goes into the record.
const MaximumJustification = 500

// Ban is one sanction, as the moderation page reads it.
type Ban struct {
	ID            int64
	AccountID     int64
	Username      string
	AuthorName    string
	Justification string
	StartsAt      time.Time
	EndsAt        *time.Time
	LiftedAt      *time.Time
	Administrator bool
}

// Active reports whether the ban still keeps somebody out at that instant.
func (b Ban) Active(now time.Time) bool {
	if b.LiftedAt != nil {
		return false
	}
	if b.StartsAt.After(now) {
		return false
	}
	return b.EndsAt == nil || b.EndsAt.After(now)
}

// Request is what a moderator asks for.
type Request struct {
	AccountID     int64
	Hours         int
	Justification string
}

// Repository is the atomic persistence boundary of the sanctions.
type Repository interface {
	Ban(context.Context, int64, int64, string, time.Time, *time.Time) (Ban, error)
	Lift(context.Context, int64, int64, time.Time) error
	Bans(context.Context, time.Time) ([]Ban, error)
	IsPrivileged(context.Context, int64) (bool, error)
}

// Service applies the sanctions of a universe.
type Service struct {
	Clock      domainclock.Clock
	Repository Repository
}

// Apply bans an account. A moderator reaches players only; an administrator
// reaches anybody but themselves. The empire keeps producing either way: a ban
// closes a door, it does not freeze a world.
func (s Service) Apply(ctx context.Context, principal appauth.Principal, request Request) (Ban, error) {
	if err := s.authorize(principal); err != nil {
		return Ban{}, err
	}
	justification := strings.TrimSpace(request.Justification)
	if request.AccountID <= 0 || justification == "" || len(justification) > MaximumJustification {
		return Ban{}, ErrInvalidRequest
	}
	if request.AccountID == principal.AccountID {
		return Ban{}, ErrProtected
	}
	privileged, err := s.Repository.IsPrivileged(ctx, request.AccountID)
	if err != nil {
		return Ban{}, err
	}
	if privileged && !principal.HasRole(appauth.RoleAdmin) {
		return Ban{}, ErrProtected
	}
	now := s.Clock.Now().UTC()
	var endsAt *time.Time
	if request.Hours > 0 {
		until := now.Add(time.Duration(request.Hours) * time.Hour)
		endsAt = &until
	}
	return s.Repository.Ban(ctx, principal.AccountID, request.AccountID, justification, now, endsAt)
}

// Lift ends a sanction early and records who did it.
func (s Service) Lift(ctx context.Context, principal appauth.Principal, banID int64) error {
	if err := s.authorize(principal); err != nil {
		return err
	}
	if banID <= 0 {
		return ErrBanNotFound
	}
	return s.Repository.Lift(ctx, principal.AccountID, banID, s.Clock.Now().UTC())
}

// List reports on every sanction, active or spent.
func (s Service) List(ctx context.Context, principal appauth.Principal) ([]Ban, error) {
	if err := s.authorize(principal); err != nil {
		return nil, err
	}
	return s.Repository.Bans(ctx, s.Clock.Now().UTC())
}

func (s Service) authorize(principal appauth.Principal) error {
	if s.Clock == nil || s.Repository == nil {
		return errors.New("moderation: incomplete service dependencies")
	}
	if principal.AccountID <= 0 || principal.MustChangePassword {
		return ErrForbidden
	}
	if !principal.HasRole(appauth.RoleAdmin) && !principal.HasRole(appauth.RoleModerator) {
		return ErrForbidden
	}
	return nil
}
