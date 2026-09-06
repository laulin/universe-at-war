package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"universeatwar/migrations"
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

func TestRebuildMigrationKeepsEveryPlanetAndItsChildren(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "rebuild.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	available, err := loadMigrations(migrations.Files)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Write().ExecContext(ctx, migrationTable); err != nil {
		t.Fatal(err)
	}
	// Stop just before the rewrite, fill the universe, then rewrite.
	var rebuild migration
	for _, candidate := range available {
		if candidate.rebuildsReferencedTable() {
			rebuild = candidate
			break
		}
		if err := applyMigration(ctx, database.Write(), candidate); err != nil {
			t.Fatalf("apply migration %d: %v", candidate.version, err)
		}
	}
	if rebuild.version == 0 {
		t.Fatal("no migration rebuilds a referenced table")
	}
	seedUniverse(t, ctx, database)

	if err := applyMigration(ctx, database.Write(), rebuild); err != nil {
		t.Fatalf("apply the rebuild: %v", err)
	}

	for query, want := range map[string]int{
		"SELECT COUNT(*) FROM planets":                                    1,
		"SELECT COUNT(*) FROM planet_resources":                           1,
		"SELECT COUNT(*) FROM planet_buildings":                           1,
		"SELECT COUNT(*) FROM planet_units":                               1,
		"SELECT COUNT(*) FROM planets WHERE kind = 'planet'":              1,
		"SELECT COUNT(*) FROM planets WHERE parent_planet_id IS NOT NULL": 0,
	} {
		var got int
		if err := database.Read().QueryRowContext(ctx, query).Scan(&got); err != nil {
			t.Fatalf("query %q: %v", query, err)
		}
		if got != want {
			t.Fatalf("query %q = %d, want %d", query, got, want)
		}
	}

	// A moon may now share the position of its planet, but only one.
	if _, err := database.Write().ExecContext(ctx, `
		INSERT INTO planets(owner_player_id, kind, parent_planet_id, name, galaxy, system, position,
			total_fields, minimum_temperature, maximum_temperature, created_at)
		VALUES (1, 'moon', 1, 'Lune', 1, 1, 8, 1, 10, 50, '2042-09-10T11:12:13Z')
	`); err != nil {
		t.Fatalf("insert a moon: %v", err)
	}
	if _, err := database.Write().ExecContext(ctx, `
		INSERT INTO planets(owner_player_id, kind, parent_planet_id, name, galaxy, system, position,
			total_fields, minimum_temperature, maximum_temperature, created_at)
		VALUES (1, 'moon', 1, 'Seconde lune', 1, 1, 8, 1, 10, 50, '2042-09-10T11:12:13Z')
	`); err == nil {
		t.Fatal("a second moon was accepted on the same position")
	}
	if _, err := database.Write().ExecContext(ctx, `
		INSERT INTO planets(owner_player_id, kind, name, galaxy, system, position,
			total_fields, minimum_temperature, maximum_temperature, created_at)
		VALUES (1, 'moon', 'Lune orpheline', 1, 1, 9, 1, 10, 50, '2042-09-10T11:12:13Z')
	`); err == nil {
		t.Fatal("a moon without a planet was accepted")
	}
}

// seedUniverse fills the tables a planet is the parent of, so the rewrite has
// something to preserve.
func seedUniverse(t *testing.T, ctx context.Context, database *Database) {
	t.Helper()
	statements := []string{
		`INSERT INTO accounts(id, username, username_normalized, created_at, updated_at)
		 VALUES (1, 'player1', 'player1', '2042-09-10T11:12:13Z', '2042-09-10T11:12:13Z')`,
		`INSERT INTO players(id, account_id, display_name, created_at)
		 VALUES (1, 1, 'Alice', '2042-09-10T11:12:13Z')`,
		`INSERT INTO planets(id, owner_player_id, name, galaxy, system, position, total_fields,
			minimum_temperature, maximum_temperature, created_at)
		 VALUES (1, 1, 'Planète mère', 1, 1, 8, 163, 10, 50, '2042-09-10T11:12:13Z')`,
		`INSERT INTO planet_resources(planet_id, produced_at) VALUES (1, '2042-09-10T11:12:13Z')`,
		`INSERT INTO planet_buildings(planet_id, building_id, level) VALUES (1, 'metal_mine', 3)`,
		`INSERT INTO planet_units(planet_id, unit_id, quantity) VALUES (1, 'light_fighter', 7)`,
	}
	for _, statement := range statements {
		if _, err := database.Write().ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed universe: %v", err)
		}
	}
}
