package tests

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appclock "universeatwar/internal/clock"
	storagesqlite "universeatwar/internal/storage/sqlite"
)

// TestBackupProducesAVerifiedRestorableCopy proves a snapshot really is the
// universe: it opens, it passes its own integrity check, and it still holds
// everything that was there.
func TestBackupProducesAVerifiedRestorableCopy(t *testing.T) {
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC))
	database := economyDatabase(t, ctx, 1)
	universeWorld := newWorld(t, database, clock)
	home, err := universeWorld.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: 1}, "Alice")
	if err != nil {
		t.Fatal(err)
	}
	setResources(t, ctx, database, home.ID, 4242, 0, 0)

	directory := t.TempDir()
	snapshot, err := database.Backup(ctx, directory, clock.Now().UTC())
	if err != nil {
		t.Fatalf("Backup() error = %v", err)
	}
	if !strings.HasPrefix(snapshot.Name, storagesqlite.BackupPrefix) || !strings.HasSuffix(snapshot.Name, ".db") {
		t.Fatalf("the snapshot is named %q", snapshot.Name)
	}
	if !strings.Contains(snapshot.Name, "20420910T120000Z") {
		t.Fatalf("the snapshot does not carry its instant: %q", snapshot.Name)
	}
	if !snapshot.Verified || snapshot.Bytes <= 0 || snapshot.SchemaVersion <= 0 {
		t.Fatalf("the snapshot was not checked: %+v", snapshot)
	}

	// Writes keep flowing while a snapshot is taken, and the copy is complete.
	setResources(t, ctx, database, home.ID, 9999, 0, 0)
	restored, err := storagesqlite.Open(ctx, snapshot.Path)
	if err != nil {
		t.Fatalf("Open(backup) error = %v", err)
	}
	defer restored.Close()
	var metal int64
	if err := restored.Read().QueryRowContext(ctx,
		"SELECT metal FROM planet_resources WHERE planet_id = ?", home.ID).Scan(&metal); err != nil {
		t.Fatal(err)
	}
	if metal != 4242 {
		t.Fatalf("the snapshot holds %d metal, want the 4242 of its instant", metal)
	}
	// The copy passes the same checks as the original.
	if err := restored.CheckIntegrity(ctx); err != nil {
		t.Fatalf("the snapshot fails its integrity check: %v", err)
	}
	report, err := restored.Diagnose(ctx)
	if err != nil || !report.Writable || report.Ruleset != "ok" {
		t.Fatalf("Diagnose(backup) = %+v %v", report, err)
	}

	// Taking one twice at the same instant is refused rather than silently
	// overwriting what is already there.
	if _, err := database.Backup(ctx, directory, clock.Now().UTC()); err == nil {
		t.Fatal("a second snapshot overwrote the first")
	}
}

// TestBackupRetentionKeepsTheNewest proves retention only ever removes
// snapshots this build wrote, oldest first.
func TestBackupRetentionKeepsTheNewest(t *testing.T) {
	ctx := context.Background()
	database := economyDatabase(t, ctx, 1)
	directory := t.TempDir()
	stranger := filepath.Join(directory, "notes.txt")
	if err := os.WriteFile(stranger, []byte("not a backup"), 0o600); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC)
	var names []string
	for hour := 0; hour < 5; hour++ {
		snapshot, err := database.Backup(ctx, directory, at.Add(time.Duration(hour)*time.Hour))
		if err != nil {
			t.Fatalf("Backup(%d) error = %v", hour, err)
		}
		names = append(names, snapshot.Name)
	}

	// Keeping nothing in particular keeps everything.
	if removed, err := storagesqlite.PruneBackups(directory, 0); err != nil || len(removed) != 0 {
		t.Fatalf("PruneBackups(0) = %v %v", removed, err)
	}
	removed, err := storagesqlite.PruneBackups(directory, 2)
	if err != nil || len(removed) != 3 {
		t.Fatalf("PruneBackups(2) = %v %v", removed, err)
	}
	for _, name := range names[:3] {
		if _, err := os.Stat(filepath.Join(directory, name)); !os.IsNotExist(err) {
			t.Fatalf("the oldest snapshot %s survived", name)
		}
	}
	for _, name := range names[3:] {
		if _, err := os.Stat(filepath.Join(directory, name)); err != nil {
			t.Fatalf("the newest snapshot %s was removed", name)
		}
	}
	// A file this build did not write is never touched.
	if _, err := os.Stat(stranger); err != nil {
		t.Fatal("retention removed a file it did not write")
	}
}

// TestABackupFromAFutureSchemaIsRefused proves a copy from a newer version is
// turned away with a plain reason rather than half restored.
func TestABackupFromAFutureSchemaIsRefused(t *testing.T) {
	ctx := context.Background()
	database := economyDatabase(t, ctx, 1)
	directory := t.TempDir()
	snapshot, err := database.Backup(ctx, directory, time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	// Somebody restores a copy taken by a newer build.
	future, err := storagesqlite.Open(ctx, snapshot.Path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := future.Write().ExecContext(ctx, `
		INSERT INTO schema_migrations(version, name, checksum, applied_at)
		VALUES (9999, '9999_from_the_future.sql', 'unknown', '2042-09-10T12:00:00Z')
	`); err != nil {
		t.Fatal(err)
	}
	if err := future.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := storagesqlite.VerifyBackup(ctx, snapshot.Path); err == nil ||
		!strings.Contains(err.Error(), "newer than") {
		t.Fatalf("a future snapshot was accepted: %v", err)
	}
}
