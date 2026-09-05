package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestOpenConfiguresSQLite(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "universe.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	assertPragmaInt(t, database.Write(), "foreign_keys", 1)
	assertPragmaText(t, database.Write(), "journal_mode", "wal")
	assertPragmaInt(t, database.Write(), "busy_timeout", 5000)
	assertPragmaInt(t, database.Write(), "synchronous", 1)

	if got := database.Write().Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("write max open connections = %d, want 1", got)
	}
}

func TestMigrateEmptyDatabaseAndRemainIdempotent(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "universe.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("first Migrate() error = %v", err)
	}
	firstVersion, err := database.SchemaVersion(ctx)
	if err != nil {
		t.Fatalf("SchemaVersion() error = %v", err)
	}
	if firstVersion < 1 {
		t.Fatalf("schema version = %d, want at least 1", firstVersion)
	}

	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("second Migrate() error = %v", err)
	}
	secondVersion, err := database.SchemaVersion(ctx)
	if err != nil {
		t.Fatalf("SchemaVersion() after second migration error = %v", err)
	}
	if secondVersion != firstVersion {
		t.Fatalf("schema version after second migration = %d, want %d", secondVersion, firstVersion)
	}
}

func TestMigrateRejectsFutureSchema(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "universe.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if _, err := database.Write().ExecContext(ctx, `
		INSERT INTO schema_migrations(version, name, checksum, applied_at)
		VALUES (9999, 'future', 'future', '2042-01-01T00:00:00Z')
	`); err != nil {
		t.Fatalf("insert future migration: %v", err)
	}

	if err := database.Migrate(ctx); err == nil {
		t.Fatal("Migrate() error = nil, want future schema error")
	}
}

func TestIntegrityCheck(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "universe.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if err := database.CheckIntegrity(ctx); err != nil {
		t.Fatalf("CheckIntegrity() error = %v", err)
	}
}

func assertPragmaInt(t *testing.T, queryer *sql.DB, name string, want int) {
	t.Helper()
	var got int
	if err := queryer.QueryRowContext(context.Background(), "PRAGMA "+name).Scan(&got); err != nil {
		t.Fatalf("PRAGMA %s: %v", name, err)
	}
	if got != want {
		t.Fatalf("PRAGMA %s = %d, want %d", name, got, want)
	}
}

func assertPragmaText(t *testing.T, queryer *sql.DB, name, want string) {
	t.Helper()
	var got string
	if err := queryer.QueryRowContext(context.Background(), "PRAGMA "+name).Scan(&got); err != nil {
		t.Fatalf("PRAGMA %s: %v", name, err)
	}
	if got != want {
		t.Fatalf("PRAGMA %s = %q, want %q", name, got, want)
	}
}
