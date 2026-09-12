package administration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	appauth "universeatwar/internal/app/authentication"
	"universeatwar/internal/domain/catalogue"
	"universeatwar/internal/domain/rules"
	"universeatwar/internal/domain/server"
)

var (
	// ErrRulesConflict prevents a stale administration form from replacing a
	// newer ruleset version published by another administrator.
	ErrRulesConflict = errors.New("administration: ruleset was modified concurrently")
	// ErrInvalidRulesUpdate reports an incomplete or unsafe live update.
	ErrInvalidRulesUpdate = errors.New("administration: invalid ruleset update")
	// ErrImmutableRules reports a setting that defines the persisted universe
	// itself and therefore cannot safely be changed after activation.
	ErrImmutableRules = errors.New("administration: immutable live rule")
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

// GameSettings is the active, immutable ruleset document projected for live
// administration. Updating it publishes another version instead of rewriting
// this one in place.
type GameSettings struct {
	Rules       rules.Ruleset
	Version     int64
	EffectiveAt time.Time
}

// StoredGameSettings is the persistence representation of an active version.
type StoredGameSettings struct {
	Document    []byte
	Version     int64
	EffectiveAt time.Time
}

// HealthRepository reads the state of the universe.
type HealthRepository interface {
	Health(context.Context, time.Time) (Health, error)
	Accounts(context.Context, time.Time) ([]Account, error)
	SetRole(context.Context, int64, int64, string, bool, time.Time) error
	SetStatus(context.Context, int64, int64, string, time.Time) error
	ActiveGameSettings(context.Context) (StoredGameSettings, error)
	ReplaceGameSettings(context.Context, int64, int64, []byte, string, string, time.Time) (StoredGameSettings, error)
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

// GameSettings returns the parameters currently used by the universe.
func (s DashboardService) GameSettings(ctx context.Context, principal appauth.Principal) (GameSettings, error) {
	if err := s.authorize(principal); err != nil {
		return GameSettings{}, err
	}
	stored, err := s.Repository.ActiveGameSettings(ctx)
	if err != nil {
		return GameSettings{}, err
	}
	return decodeGameSettings(stored)
}

// UpdateGameSettings validates a complete ruleset and publishes it as a new
// active version. Topology, catalogue and network exposure define external or
// persisted structures, so they remain fixed after the universe has started.
func (s DashboardService) UpdateGameSettings(ctx context.Context, principal appauth.Principal,
	expectedVersion int64, updated rules.Ruleset, justification string) (GameSettings, error) {
	if err := s.authorize(principal); err != nil {
		return GameSettings{}, err
	}
	justification = strings.TrimSpace(justification)
	if expectedVersion <= 0 || justification == "" {
		return GameSettings{}, ErrInvalidRulesUpdate
	}
	current, err := s.GameSettings(ctx, principal)
	if err != nil {
		return GameSettings{}, err
	}
	if current.Version != expectedVersion {
		return GameSettings{}, ErrRulesConflict
	}
	if updated.Topology != current.Rules.Topology ||
		updated.Progression.CatalogueVersion != current.Rules.Progression.CatalogueVersion ||
		updated.Identity.NetworkVisibility != current.Rules.Identity.NetworkVisibility {
		return GameSettings{}, ErrImmutableRules
	}
	if err := catalogue.ValidateRuleset(updated); err != nil {
		return GameSettings{}, errors.Join(ErrInvalidRulesUpdate, err)
	}
	document, err := rules.Encode(updated)
	if err != nil {
		return GameSettings{}, errors.Join(ErrInvalidRulesUpdate, err)
	}
	digest := sha256.Sum256(document)
	stored, err := s.Repository.ReplaceGameSettings(ctx, principal.AccountID, expectedVersion,
		document, hex.EncodeToString(digest[:]), justification, s.Clock.Now().UTC())
	if err != nil {
		return GameSettings{}, err
	}
	return decodeGameSettings(stored)
}

func decodeGameSettings(stored StoredGameSettings) (GameSettings, error) {
	configured, err := rules.Decode(stored.Document)
	if err != nil {
		return GameSettings{}, err
	}
	return GameSettings{Rules: configured, Version: stored.Version, EffectiveAt: stored.EffectiveAt}, nil
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
