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

func TestChatReadMigrationTreatsExistingHistoryAsRead(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "chat-upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	available, err := loadMigrations(migrations.Files)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Write().ExecContext(ctx, migrationTable); err != nil {
		t.Fatal(err)
	}
	var readMigration migration
	for _, candidate := range available {
		if candidate.version == 22 {
			readMigration = candidate
			break
		}
		if err := applyMigration(ctx, database.Write(), candidate); err != nil {
			t.Fatalf("apply migration %d: %v", candidate.version, err)
		}
	}
	if readMigration.version == 0 {
		t.Fatal("chat read migration is missing")
	}
	statements := []string{
		`INSERT INTO accounts(id, username, username_normalized, created_at, updated_at) VALUES
			(1, 'player1', 'player1', '2042-09-10T11:00:00Z', '2042-09-10T11:00:00Z'),
			(2, 'player2', 'player2', '2042-09-10T11:00:00Z', '2042-09-10T11:00:00Z')`,
		`INSERT INTO players(id, account_id, display_name, created_at) VALUES
			(1, 1, 'Alice', '2042-09-10T11:00:00Z'),
			(2, 2, 'Bob', '2042-09-10T11:00:00Z')`,
		`INSERT INTO alliances(id, name, name_normalized, tag, founder_player_id, created_at)
			VALUES (1, 'Les Veilleurs', 'les veilleurs', 'VEIL', 1, '2042-09-10T11:00:00Z')`,
		`INSERT INTO alliance_members(player_id, alliance_id, role, joined_at) VALUES
			(1, 1, 'founder', '2042-09-10T11:00:00Z'),
			(2, 1, 'member', '2042-09-10T11:00:00Z')`,
		`INSERT INTO chat_conversations(id, kind, player_one_id, player_two_id, created_at, updated_at)
			VALUES (1, 'direct', 1, 2, '2042-09-10T11:00:00Z', '2042-09-10T11:02:00Z')`,
		`INSERT INTO chat_conversations(id, kind, alliance_id, created_at, updated_at)
			VALUES (2, 'alliance', 1, '2042-09-10T11:00:00Z', '2042-09-10T11:03:00Z')`,
		`INSERT INTO chat_messages(id, conversation_id, author_player_id, author_name, body, client_key, created_at) VALUES
			(1, 1, 1, 'Alice', 'ancien direct', 'upgrade-message-1', '2042-09-10T11:01:00Z'),
			(2, 1, 2, 'Bob', 'réponse ancienne', 'upgrade-message-2', '2042-09-10T11:02:00Z'),
			(3, 2, 1, 'Alice', 'ancienne alliance', 'upgrade-message-3', '2042-09-10T11:03:00Z')`,
	}
	for _, statement := range statements {
		if _, err := database.Write().ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed chat upgrade: %v", err)
		}
	}
	if err := applyMigration(ctx, database.Write(), readMigration); err != nil {
		t.Fatalf("apply chat read migration: %v", err)
	}
	var states int
	if err := database.Read().QueryRowContext(ctx, "SELECT COUNT(*) FROM chat_read_states").Scan(&states); err != nil || states != 4 {
		t.Fatalf("read states after upgrade = %d, %v; want 4", states, err)
	}
	repository := NewChatRepository(database.Read(), database.Write())
	for _, accountID := range []int64{1, 2} {
		if unread, err := repository.UnreadCount(ctx, accountID); err != nil || unread != 0 {
			t.Fatalf("account %d historical unread = %d, %v; want 0", accountID, unread, err)
		}
	}
	if _, err := database.Write().ExecContext(ctx, `
		INSERT INTO chat_messages(id, conversation_id, author_player_id, author_name, body, client_key, created_at)
		VALUES (4, 1, 1, 'Alice', 'nouveau direct', 'upgrade-message-4', '2042-09-10T11:04:00Z')
	`); err != nil {
		t.Fatal(err)
	}
	if unread, err := repository.UnreadCount(ctx, 2); err != nil || unread != 1 {
		t.Fatalf("new unread after upgrade = %d, %v; want 1", unread, err)
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
