package administration

import (
	"context"
	"errors"
	"time"

	appauth "universeatwar/internal/app/authentication"
)

// Snapshot is one verified copy of the universe, as the administration reads it.
type Snapshot struct {
	Name          string
	Path          string
	TakenAt       time.Time
	Bytes         int64
	SchemaVersion int
}

// BackupRepository takes and records the snapshots.
type BackupRepository interface {
	TakeBackup(context.Context, int64, time.Time, int) (Snapshot, error)
}

// BackupService lets an administrator put the universe somewhere safe without
// leaving the browser.
type BackupService struct {
	Clock      Clock
	Repository BackupRepository
	// Keep bounds how many snapshots are kept on disk; zero keeps them all.
	Keep int
}

// Take writes a verified snapshot and records it.
func (s BackupService) Take(ctx context.Context, principal appauth.Principal) (Snapshot, error) {
	if s.Clock == nil || s.Repository == nil {
		return Snapshot{}, errors.New("administration: incomplete service dependencies")
	}
	if principal.AccountID <= 0 || principal.MustChangePassword || !principal.HasRole(appauth.RoleAdmin) {
		return Snapshot{}, ErrForbidden
	}
	return s.Repository.TakeBackup(ctx, principal.AccountID, s.Clock.Now().UTC(), s.Keep)
}
