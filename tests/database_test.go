package tests

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"universeatwar/internal/domain/rules"
	storagesqlite "universeatwar/internal/storage/sqlite"
)

// templateDirectory holds the schema template for the life of the test binary.
// It is made before the first test runs and removed after the last one, so no
// test ever races another for it.
var templateDirectory string

func TestMain(m *testing.M) {
	directory, err := os.MkdirTemp("", "universe-at-war-schema-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "tests: no place for the schema template:", err)
		os.Exit(1)
	}
	templateDirectory = directory
	code := m.Run()
	_ = os.RemoveAll(directory)
	os.Exit(code)
}

// schemaTemplate is a database carrying the whole schema and not one row,
// migrated once for the entire binary.
var schemaTemplate = sync.OnceValues(buildSchemaTemplate)

func buildSchemaTemplate() (string, error) {
	ctx := context.Background()
	path := filepath.Join(templateDirectory, "schema.db")
	database, err := storagesqlite.Open(ctx, path)
	if err != nil {
		return "", err
	}
	if err := database.Migrate(ctx); err != nil {
		_ = database.Close()
		return "", err
	}
	// Closing every pool checkpoints the write-ahead log and takes it away, so
	// the single file left behind is a whole database to copy.
	if err := database.Close(); err != nil {
		return "", err
	}
	return path, nil
}

// freshDatabase opens an empty database carrying the current schema. It copies
// the template instead of replaying the nineteen migrations: replaying them
// costs about two seconds under the race detector, and the suite would pay that
// price some seventy times over for a schema that is the same every time.
//
// The copy is still handed to Migrate, which finds every migration already
// applied and only checks that the file and this build agree on them. That is
// what the call it replaces proved too.
func freshDatabase(t testing.TB, ctx context.Context, name string) *storagesqlite.Database {
	t.Helper()
	template, err := schemaTemplate()
	if err != nil {
		t.Fatalf("schema template: %v", err)
	}
	content, err := os.ReadFile(template)
	if err != nil {
		t.Fatalf("read schema template: %v", err)
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write schema template: %v", err)
	}
	database, err := storagesqlite.Open(ctx, path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return database
}

// setRules rewrites the ruleset a test universe runs under. A fixture that
// depends on a setting is better off naming it than inheriting whatever the
// defaults happen to say today.
func setRules(t testing.TB, ctx context.Context, database *storagesqlite.Database, mutate func(*rules.Ruleset)) {
	t.Helper()
	var document string
	if err := database.Read().QueryRowContext(ctx,
		"SELECT document FROM ruleset_versions WHERE status = 'active' ORDER BY version DESC LIMIT 1").Scan(&document); err != nil {
		t.Fatalf("read active ruleset: %v", err)
	}
	configured, err := rules.Decode([]byte(document))
	if err != nil {
		t.Fatalf("decode active ruleset: %v", err)
	}
	mutate(&configured)
	encoded, err := rules.Encode(configured)
	if err != nil {
		t.Fatalf("encode ruleset: %v", err)
	}
	if _, err := database.Write().ExecContext(ctx,
		"UPDATE ruleset_versions SET document = ? WHERE status = 'active'", string(encoded)); err != nil {
		t.Fatalf("write ruleset: %v", err)
	}
}
