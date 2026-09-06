package administration

import (
	"context"
	"errors"
	"time"

	appauth "universeatwar/internal/app/authentication"
	"universeatwar/internal/domain/server"
)

// Health is what an administrator needs to know at a glance about a running
// universe. Everything here is read from the database itself.
type Health struct {
	State          server.State
	StartedAt      time.Time
	Uptime         time.Duration
	SchemaVersion  int
	JournalMode    string
	DatabaseBytes  int64
	RulesetVersion int64
	RulesetSince   time.Time
	NextEventAt    *time.Time
	NextEventType  string
	PendingEvents  int
	FailedEvents   int
	RecentEvents   int
	Accounts       int
	Players        int
	Artificials    int
	ActiveBans     int
	LastBackupAt   *time.Time
	LastBackupName string
}

// Account is one account as the administration page reads it.
type Account struct {
	ID        int64
	Username  string
	Kind      string
	Status    string
	Roles     []string
	CreatedAt time.Time
	HasEmpire bool
	Banned    bool
}

// HealthRepository reads the state of the universe.
type HealthRepository interface {
	Health(context.Context, time.Time) (Health, error)
	Accounts(context.Context, time.Time) ([]Account, error)
	SetRole(context.Context, int64, int64, string, bool, time.Time) error
	SetStatus(context.Context, int64, int64, string, time.Time) error
}

// DashboardService serves the administration view of a universe.
type DashboardService struct {
	Clock      Clock
	Repository HealthRepository
}

// Clock is the instant source, kept as a small contract of its own.
type Clock interface {
	Now() time.Time
}

// Health reports on the universe.
func (s DashboardService) Health(ctx context.Context, principal appauth.Principal) (Health, error) {
	if err := s.authorize(principal); err != nil {
		return Health{}, err
	}
	return s.Repository.Health(ctx, s.Clock.Now().UTC())
}

// Accounts lists every account with its roles and its status.
func (s DashboardService) Accounts(ctx context.Context, principal appauth.Principal) ([]Account, error) {
	if err := s.authorize(principal); err != nil {
		return nil, err
	}
	return s.Repository.Accounts(ctx, s.Clock.Now().UTC())
}

// SetRole grants or withdraws a role. An administrator cannot take their own
// administration away: a universe always keeps somebody who can act.
func (s DashboardService) SetRole(ctx context.Context, principal appauth.Principal,
	accountID int64, role string, granted bool) error {
	if err := s.authorize(principal); err != nil {
		return err
	}
	if accountID <= 0 {
		return ErrAccountNotFound
	}
	switch role {
	case string(appauth.RoleAdmin), string(appauth.RoleModerator), string(appauth.RolePlayer):
	default:
		return errors.New("administration: unknown role")
	}
	if accountID == principal.AccountID && role == string(appauth.RoleAdmin) && !granted {
		return ErrForbidden
	}
	return s.Repository.SetRole(ctx, principal.AccountID, accountID, role, granted, s.Clock.Now().UTC())
}

// SetStatus enables or disables an account. Disabling is not a sanction: it is
// an administrative act, and it leaves the empire untouched just the same.
func (s DashboardService) SetStatus(ctx context.Context, principal appauth.Principal,
	accountID int64, status string) error {
	if err := s.authorize(principal); err != nil {
		return err
	}
	if accountID <= 0 {
		return ErrAccountNotFound
	}
	if status != "active" && status != "disabled" {
		return errors.New("administration: unknown status")
	}
	if accountID == principal.AccountID {
		return ErrForbidden
	}
	return s.Repository.SetStatus(ctx, principal.AccountID, accountID, status, s.Clock.Now().UTC())
}

func (s DashboardService) authorize(principal appauth.Principal) error {
	if s.Clock == nil || s.Repository == nil {
		return errors.New("administration: incomplete service dependencies")
	}
	if principal.AccountID <= 0 || principal.MustChangePassword || !principal.HasRole(appauth.RoleAdmin) {
		return ErrForbidden
	}
	return nil
}
