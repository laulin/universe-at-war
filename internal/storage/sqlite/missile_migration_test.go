package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"universeatwar/migrations"
)

func TestMissileMigrationPreservesReportsAndAllianceMemory(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "missile-upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if _, err := database.Write().ExecContext(ctx, migrationTable); err != nil {
		t.Fatal(err)
	}
	available, err := loadMigrations(migrations.Files)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range available {
		if candidate.version >= 25 {
			break
		}
		if err := applyMigration(ctx, database.Write(), candidate); err != nil {
			t.Fatal(err)
		}
	}
	for _, statement := range []string{
		`INSERT INTO accounts(id,username,username_normalized,created_at,updated_at) VALUES (1,'alice','alice','2042-09-10T11:00:00Z','2042-09-10T11:00:00Z')`,
		`INSERT INTO players(id,account_id,display_name,created_at) VALUES (1,1,'Alice','2042-09-10T11:00:00Z')`,
		`INSERT INTO alliances(id,name,name_normalized,tag,founder_player_id,created_at) VALUES (1,'Alliance','alliance','TEST',1,'2042-09-10T11:00:00Z')`,
		`INSERT INTO reports(id,recipient_player_id,kind,subject_type,subject_id,galaxy,system,position,occurred_at,read_at,payload_version,payload,shared_alliance_id,created_at)
		 VALUES (1,1,'espionage','planet',9,1,1,1,'2042-09-10T11:00:00Z','2042-09-10T11:01:00Z',1,'{"probes":5}',1,'2042-09-10T11:00:00Z')`,
		`INSERT INTO ai_alliance_memory(alliance_id,author_player_id,kind,galaxy,system,position,source_report_id,observed_at,expires_at,confidence,payload_version,payload)
		 VALUES (1,1,'target',1,1,1,1,'2042-09-10T11:00:00Z','2042-09-11T11:00:00Z',1,1,'{}')`,
	} {
		if _, err := database.Write().ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.Read().QueryRowContext(ctx, `SELECT COUNT(*) FROM reports r JOIN ai_alliance_memory m ON m.source_report_id=r.id
		WHERE r.id=1 AND r.kind='espionage' AND r.shared_alliance_id=1 AND r.read_at='2042-09-10T11:01:00Z' AND json_extract(r.payload,'$.probes')=5`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("lost report or memory: %d, %v", count, err)
	}
	if err := database.CheckIntegrity(ctx); err != nil {
		t.Fatal(err)
	}
}
