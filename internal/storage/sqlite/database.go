// Package sqlite implements persistent infrastructure using database/sql and
// modernc.org/sqlite.
package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"universeatwar/internal/domain/rules"
	"universeatwar/migrations"

	_ "modernc.org/sqlite"
)

const migrationTable = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY CHECK (version > 0),
    name TEXT NOT NULL UNIQUE,
    checksum TEXT NOT NULL,
    applied_at TEXT NOT NULL
) STRICT`

// ErrFutureSchema indicates that the database was opened by an application
// that does not know its most recent migration.
var ErrFutureSchema = errors.New("sqlite: database schema is newer than this application")

// Database owns separate read and write handles to the same SQLite file.
type Database struct {
	read  *sql.DB
	write *sql.DB
}

// Open opens a SQLite database with the production connection policy.
func Open(ctx context.Context, path string) (*Database, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("sqlite: database path is empty")
	}

	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("sqlite: resolve database path: %w", err)
	}
	dsn := dataSourceName(absolutePath)

	write, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open write handle: %w", err)
	}
	write.SetMaxOpenConns(1)
	write.SetMaxIdleConns(1)

	if err := write.PingContext(ctx); err != nil {
		_ = write.Close()
		return nil, fmt.Errorf("sqlite: connect write handle: %w", err)
	}

	read, err := sql.Open("sqlite", dsn)
	if err != nil {
		_ = write.Close()
		return nil, fmt.Errorf("sqlite: open read handle: %w", err)
	}
	read.SetMaxOpenConns(max(4, runtime.GOMAXPROCS(0)))
	read.SetMaxIdleConns(4)
	if err := read.PingContext(ctx); err != nil {
		_ = read.Close()
		_ = write.Close()
		return nil, fmt.Errorf("sqlite: connect read handle: %w", err)
	}

	return &Database{read: read, write: write}, nil
}

func dataSourceName(path string) string {
	location := &url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	query := location.Query()
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", "journal_mode(WAL)")
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "synchronous(NORMAL)")
	query.Set("_txlock", "immediate")
	location.RawQuery = query.Encode()
	return location.String()
}

// Read returns the concurrent read handle.
func (d *Database) Read() *sql.DB { return d.read }

// Write returns the serialized write handle.
func (d *Database) Write() *sql.DB { return d.write }

// Close closes both handles.
func (d *Database) Close() error {
	readErr := d.read.Close()
	writeErr := d.write.Close()
	return errors.Join(readErr, writeErr)
}

// Migrate validates previously applied migrations and applies every missing
// embedded migration in order.
func (d *Database) Migrate(ctx context.Context) error {
	available, err := loadMigrations(migrations.Files)
	if err != nil {
		return err
	}
	if len(available) == 0 {
		return errors.New("sqlite: no embedded migrations")
	}

	if _, err := d.write.ExecContext(ctx, migrationTable); err != nil {
		return fmt.Errorf("sqlite: create migration table: %w", err)
	}

	applied, err := readAppliedMigrations(ctx, d.write)
	if err != nil {
		return err
	}
	latest := available[len(available)-1].version
	for version, record := range applied {
		if version > latest {
			return fmt.Errorf("%w: found version %d, latest known is %d", ErrFutureSchema, version, latest)
		}
		migration, ok := findMigration(available, version)
		if !ok {
			return fmt.Errorf("sqlite: applied migration %d is not embedded", version)
		}
		if migration.checksum != record.checksum || migration.name != record.name {
			return fmt.Errorf("sqlite: migration %d checksum or name changed", version)
		}
	}

	for _, migration := range available {
		if _, ok := applied[migration.version]; ok {
			continue
		}
		if err := applyMigration(ctx, d.write, migration); err != nil {
			return err
		}
	}
	return nil
}

// SchemaVersion returns the most recent applied migration version.
func (d *Database) SchemaVersion(ctx context.Context) (int, error) {
	var version int
	if err := d.write.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&version); err != nil {
		return 0, fmt.Errorf("sqlite: read schema version: %w", err)
	}
	return version, nil
}

// CheckIntegrity executes SQLite's physical and foreign-key integrity checks.
func (d *Database) CheckIntegrity(ctx context.Context) error {
	rows, err := d.read.QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		return fmt.Errorf("sqlite: integrity check: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var result string
		if err := rows.Scan(&result); err != nil {
			return fmt.Errorf("sqlite: scan integrity result: %w", err)
		}
		if result != "ok" {
			return fmt.Errorf("sqlite: integrity check failed: %s", result)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("sqlite: read integrity results: %w", err)
	}

	foreignKeys, err := d.read.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("sqlite: foreign key check: %w", err)
	}
	defer foreignKeys.Close()
	if foreignKeys.Next() {
		return errors.New("sqlite: foreign key check failed")
	}
	return foreignKeys.Err()
}

type migration struct {
	version  int
	name     string
	checksum string
	sql      string
}

// rebuildMarker opens a migration that rewrites a table other tables point at.
// SQLite can only drop such a table with foreign key enforcement off, so the
// runner turns it off around the migration and checks every key before the
// commit. The rewrite itself stays inside the transaction.
const rebuildMarker = "-- migration: rebuild referenced table"

func (m migration) rebuildsReferencedTable() bool {
	return strings.HasPrefix(m.sql, rebuildMarker)
}

type appliedMigration struct {
	name     string
	checksum string
}

func loadMigrations(files fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		return nil, fmt.Errorf("sqlite: list migrations: %w", err)
	}

	var result []migration
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		prefix, _, ok := strings.Cut(entry.Name(), "_")
		if !ok {
			return nil, fmt.Errorf("sqlite: invalid migration name %q", entry.Name())
		}
		version, err := strconv.Atoi(prefix)
		if err != nil || version <= 0 {
			return nil, fmt.Errorf("sqlite: invalid migration version in %q", entry.Name())
		}
		contents, err := fs.ReadFile(files, entry.Name())
		if err != nil {
			return nil, fmt.Errorf("sqlite: read migration %q: %w", entry.Name(), err)
		}
		digest := sha256.Sum256(contents)
		result = append(result, migration{
			version:  version,
			name:     entry.Name(),
			checksum: hex.EncodeToString(digest[:]),
			sql:      string(contents),
		})
	}

	sort.Slice(result, func(i, j int) bool { return result[i].version < result[j].version })
	for index := 1; index < len(result); index++ {
		if result[index-1].version == result[index].version {
			return nil, fmt.Errorf("sqlite: duplicate migration version %d", result[index].version)
		}
	}
	return result, nil
}

func readAppliedMigrations(ctx context.Context, database *sql.DB) (map[int]appliedMigration, error) {
	rows, err := database.QueryContext(ctx, "SELECT version, name, checksum FROM schema_migrations ORDER BY version")
	if err != nil {
		return nil, fmt.Errorf("sqlite: list applied migrations: %w", err)
	}
	defer rows.Close()

	result := make(map[int]appliedMigration)
	for rows.Next() {
		var version int
		var record appliedMigration
		if err := rows.Scan(&version, &record.name, &record.checksum); err != nil {
			return nil, fmt.Errorf("sqlite: scan applied migration: %w", err)
		}
		result[version] = record
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: read applied migrations: %w", err)
	}
	return result, nil
}

func findMigration(migrations []migration, version int) (migration, bool) {
	index := sort.Search(len(migrations), func(index int) bool {
		return migrations[index].version >= version
	})
	if index == len(migrations) || migrations[index].version != version {
		return migration{}, false
	}
	return migrations[index], true
}

func applyMigration(ctx context.Context, database *sql.DB, migration migration) error {
	// The pragma only takes effect outside a transaction, so the migration runs
	// on one pinned connection: disable, rewrite, verify, commit, re-enable.
	connection, err := database.Conn(ctx)
	if err != nil {
		return fmt.Errorf("sqlite: pin connection for migration %d: %w", migration.version, err)
	}
	defer func() { _ = connection.Close() }()

	if migration.rebuildsReferencedTable() {
		if _, err := connection.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
			return fmt.Errorf("sqlite: relax foreign keys for migration %d: %w", migration.version, err)
		}
		defer func() { _, _ = connection.ExecContext(ctx, "PRAGMA foreign_keys = ON") }()
	}

	transaction, err := connection.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: begin migration %d: %w", migration.version, err)
	}
	defer func() { _ = transaction.Rollback() }()

	if _, err := transaction.ExecContext(ctx, migration.sql); err != nil {
		return fmt.Errorf("sqlite: apply migration %d: %w", migration.version, err)
	}
	if migration.rebuildsReferencedTable() {
		if err := checkForeignKeys(ctx, transaction); err != nil {
			return fmt.Errorf("sqlite: migration %d broke a foreign key: %w", migration.version, err)
		}
	}
	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO schema_migrations(version, name, checksum, applied_at)
		VALUES (?, ?, ?, ?)
	`, migration.version, migration.name, migration.checksum, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("sqlite: record migration %d: %w", migration.version, err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("sqlite: commit migration %d: %w", migration.version, err)
	}
	return nil
}

// checkForeignKeys refuses to commit a rewrite that left an orphan row behind.
func checkForeignKeys(ctx context.Context, transaction *sql.Tx) error {
	rows, err := transaction.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		return errors.New("a row no longer points at an existing parent")
	}
	return rows.Err()
}

// LatestSchemaVersion is the newest migration this build carries. A database or
// a backup beyond it comes from a newer version of the game and is refused.
func LatestSchemaVersion() int {
	available, err := loadMigrations(migrations.Files)
	if err != nil || len(available) == 0 {
		return 0
	}
	return available[len(available)-1].version
}

// Diagnosis is what `doctor` reports beyond the integrity of the file itself.
type Diagnosis struct {
	JournalMode string
	Writable    bool
	Ruleset     string
}

// Diagnose checks that the database can actually be worked with: that it is in
// write-ahead mode, that a write really goes through, that no foreign key is
// dangling and that the active ruleset still decodes.
func (d *Database) Diagnose(ctx context.Context) (Diagnosis, error) {
	var report Diagnosis
	if err := d.write.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&report.JournalMode); err != nil {
		return Diagnosis{}, fmt.Errorf("sqlite: read journal mode: %w", err)
	}
	// A write is attempted and rolled back: reading alone would not notice a
	// read-only file or a directory the process cannot write into.
	transaction, err := d.write.BeginTx(ctx, nil)
	if err == nil {
		_, err = transaction.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS doctor_probe(id INTEGER PRIMARY KEY)")
		report.Writable = err == nil
		_ = transaction.Rollback()
	}
	rows, err := d.write.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return Diagnosis{}, fmt.Errorf("sqlite: check foreign keys: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		return Diagnosis{}, errors.New("sqlite: the database holds dangling references")
	}
	if err := rows.Err(); err != nil {
		return Diagnosis{}, fmt.Errorf("sqlite: check foreign keys: %w", err)
	}
	report.Ruleset = "none"
	var document string
	err = d.read.QueryRowContext(ctx, "SELECT document FROM ruleset_versions WHERE status = 'active'").Scan(&document)
	if errors.Is(err, sql.ErrNoRows) {
		return report, nil
	}
	if err != nil {
		return Diagnosis{}, fmt.Errorf("sqlite: read ruleset: %w", err)
	}
	if _, err := rules.Decode([]byte(document)); err != nil {
		return Diagnosis{}, fmt.Errorf("sqlite: the active ruleset does not decode: %w", err)
	}
	report.Ruleset = "ok"
	return report, nil
}
