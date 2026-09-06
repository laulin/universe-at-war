package administration

import (
	"context"
	"errors"
	"strings"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appregistration "universeatwar/internal/app/registration"
	domainclock "universeatwar/internal/domain/clock"
)

var (
	// ErrForbidden hides every privileged surface behind the same answer.
	ErrForbidden = errors.New("administration: administrator role required")
	// ErrInvitationNotFound is returned for an invitation that is not there or
	// has already been spent.
	ErrInvitationNotFound = errors.New("administration: no such invitation")
)

// InvitationLifetime is how long a ticket stays valid unless told otherwise.
const InvitationLifetime = 7 * 24 * time.Hour

// Invitation is one ticket into a closed universe, as an administrator sees it.
// The code itself is shown once, when it is created, and never again.
type Invitation struct {
	ID        int64
	Label     string
	CreatedAt time.Time
	ExpiresAt time.Time
	UsedAt    *time.Time
	UsedBy    string
	Revoked   bool
	// Code is filled only on creation: nothing else can ever read it back.
	Code string
}

// Spent reports whether the ticket can no longer let anybody in.
func (i Invitation) Spent(now time.Time) bool {
	return i.UsedAt != nil || i.Revoked || !now.Before(i.ExpiresAt)
}

// InvitationRepository persists the tickets. It only ever sees digests.
type InvitationRepository interface {
	CreateInvitation(context.Context, int64, string, string, time.Time, time.Time) (Invitation, error)
	Invitations(context.Context) ([]Invitation, error)
	RevokeInvitation(context.Context, int64, time.Time) error
}

// InvitationService hands out and withdraws tickets into a closed universe.
type InvitationService struct {
	Clock      domainclock.Clock
	Secrets    Secrets
	Repository InvitationRepository
	Lifetime   time.Duration
}

// Create mints one ticket and hands the code back exactly once.
func (s InvitationService) Create(ctx context.Context, principal appauth.Principal, label string) (Invitation, error) {
	if err := s.authorize(principal); err != nil {
		return Invitation{}, err
	}
	if s.Secrets == nil {
		return Invitation{}, errors.New("administration: no secret source")
	}
	code, err := s.Secrets.Generate()
	if err != nil {
		return Invitation{}, err
	}
	now := s.Clock.Now().UTC()
	lifetime := s.Lifetime
	if lifetime <= 0 {
		lifetime = InvitationLifetime
	}
	created, err := s.Repository.CreateInvitation(ctx, principal.AccountID, appregistration.Digest(code),
		strings.TrimSpace(label), now, now.Add(lifetime))
	if err != nil {
		return Invitation{}, err
	}
	created.Code = code
	return created, nil
}

// List reports on every ticket, spent or not. No code is ever shown again.
func (s InvitationService) List(ctx context.Context, principal appauth.Principal) ([]Invitation, error) {
	if err := s.authorize(principal); err != nil {
		return nil, err
	}
	return s.Repository.Invitations(ctx)
}

// Revoke withdraws a ticket that has not been used.
func (s InvitationService) Revoke(ctx context.Context, principal appauth.Principal, invitationID int64) error {
	if err := s.authorize(principal); err != nil {
		return err
	}
	if invitationID <= 0 {
		return ErrInvitationNotFound
	}
	return s.Repository.RevokeInvitation(ctx, invitationID, s.Clock.Now().UTC())
}

func (s InvitationService) authorize(principal appauth.Principal) error {
	if s.Clock == nil || s.Repository == nil {
		return errors.New("administration: incomplete service dependencies")
	}
	if principal.AccountID <= 0 || principal.MustChangePassword || !principal.HasRole(appauth.RoleAdmin) {
		return ErrForbidden
	}
	return nil
}
