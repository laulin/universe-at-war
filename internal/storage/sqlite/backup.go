package sqlite

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	appadmin "universeatwar/internal/app/administration"
)

// BackupPrefix is how every snapshot this build takes is named, so retention
// never touches a file it did not write.
const BackupPrefix = "universe-at-war-"

// Backup is one snapshot on disk.
type Backup struct {
	Name          string
	Path          string
	TakenAt       time.Time
	Bytes         int64
	SchemaVersion int
	Verified      bool
}

// BackupName is the timestamped name of a snapshot taken at that instant.
func BackupName(at time.Time) string {
	return BackupPrefix + at.UTC().Format("20060102T150405Z") + ".db"
}

// Backup writes a consistent snapshot of the database into a directory, then
// opens it again to check it. SQLite takes the snapshot under a read
// transaction, so writers may keep working while it runs.
func (d *Database) Backup(ctx context.Context, directory string, at time.Time) (Backup, error) {
	if strings.TrimSpace(directory) == "" {
		return Backup{}, errors.New("sqlite: a backup needs a directory")
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return Backup{}, fmt.Errorf("sqlite: prepare backup directory: %w", err)
	}
	snapshot := Backup{Name: BackupName(at), TakenAt: at.UTC()}
	snapshot.Path = filepath.Join(directory, snapshot.Name)
	if _, err := os.Stat(snapshot.Path); err == nil {
		return Backup{}, fmt.Errorf("sqlite: a backup named %s already exists", snapshot.Name)
	}
	// VACUUM INTO is the snapshot facility of SQLite itself: it writes a
	// complete, consistent copy without stopping the server.
	if _, err := d.write.ExecContext(ctx, "VACUUM INTO ?", snapshot.Path); err != nil {
		return Backup{}, fmt.Errorf("sqlite: write backup: %w", err)
	}
	information, err := os.Stat(snapshot.Path)
	if err != nil {
		return Backup{}, fmt.Errorf("sqlite: inspect backup: %w", err)
	}
	snapshot.Bytes = information.Size()
	version, err := VerifyBackup(ctx, snapshot.Path)
	if err != nil {
		return Backup{}, err
	}
	snapshot.SchemaVersion = version
	snapshot.Verified = true
	return snapshot, nil
}

// VerifyBackup opens a snapshot and reports the schema it holds. A copy that
// does not open, does not pass its integrity check or carries a schema this
// build cannot read is refused with a plain reason.
func VerifyBackup(ctx context.Context, path string) (int, error) {
	copied, err := Open(ctx, path)
	if err != nil {
		return 0, fmt.Errorf("sqlite: open backup: %w", err)
	}
	defer func() { _ = copied.Close() }()
	if err := copied.CheckIntegrity(ctx); err != nil {
		return 0, fmt.Errorf("sqlite: verify backup: %w", err)
	}
	version, err := copied.SchemaVersion(ctx)
	if err != nil {
		return 0, fmt.Errorf("sqlite: read backup schema: %w", err)
	}
	if version > LatestSchemaVersion() {
		return 0, fmt.Errorf("sqlite: this backup carries schema %d, newer than the %d this build knows",
			version, LatestSchemaVersion())
	}
	return version, nil
}

// PruneBackups keeps the newest snapshots and removes the rest. It only ever
// deletes files this build wrote, and keeping zero means keeping everything.
func PruneBackups(directory string, keep int) ([]string, error) {
	if keep <= 0 {
		return nil, nil
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("sqlite: read backup directory: %w", err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), BackupPrefix) || !strings.HasSuffix(entry.Name(), ".db") {
			continue
		}
		names = append(names, entry.Name())
	}
	// The names carry their instant, so sorting them sorts them by age.
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	if len(names) <= keep {
		return nil, nil
	}
	var removed []string
	for _, name := range names[keep:] {
		if err := os.Remove(filepath.Join(directory, name)); err != nil {
			return removed, fmt.Errorf("sqlite: remove old backup: %w", err)
		}
		removed = append(removed, name)
	}
	return removed, nil
}

// BackupRepository takes snapshots on behalf of an administrator and keeps a
// record of them, so the dashboard can say when the universe was last saved.
type BackupRepository struct {
	database  *Database
	directory string
}

func NewBackupRepository(database *Database, directory string) *BackupRepository {
	return &BackupRepository{database: database, directory: directory}
}

// TakeBackup writes a verified snapshot, records it and prunes the old ones.
func (r *BackupRepository) TakeBackup(ctx context.Context, authorID int64, now time.Time,
	keep int) (appadmin.Snapshot, error) {
	snapshot, err := r.database.Backup(ctx, r.directory, now)
	if err != nil {
		return appadmin.Snapshot{}, err
	}
	if _, err := r.database.Write().ExecContext(ctx, `
		INSERT INTO backups(name, path, taken_at, bytes, schema_version, verified, author_account_id)
		VALUES (?, ?, ?, ?, ?, 1, ?)
	`, snapshot.Name, snapshot.Path, timestamp(snapshot.TakenAt), snapshot.Bytes,
		snapshot.SchemaVersion, authorID); err != nil {
		return appadmin.Snapshot{}, fmt.Errorf("sqlite: record backup: %w", err)
	}
	if _, err := PruneBackups(r.directory, keep); err != nil {
		return appadmin.Snapshot{}, err
	}
	return appadmin.Snapshot{
		Name: snapshot.Name, Path: snapshot.Path, TakenAt: snapshot.TakenAt,
		Bytes: snapshot.Bytes, SchemaVersion: snapshot.SchemaVersion,
	}, nil
}
