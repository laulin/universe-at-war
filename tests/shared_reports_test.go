package tests

import (
	"context"
	"errors"
	"testing"

	appfleet "universeatwar/internal/app/fleet"
	appreports "universeatwar/internal/app/reports"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/unit"
)

func TestSharedReportsReachTheAllianceAndNobodyElse(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, players := allianceUniverse(t, 3)
	spy, ally, stranger := players[0], players[1], players[2]
	joinAlliance(t, ctx, universeWorld, spy, ally, "player2")

	// The spy earns a report of their own by looking at the stranger's planet.
	setUnits(t, ctx, database, 1, "espionage_probe", 3)
	setResources(t, ctx, database, 1, 5000, 5000, 5000)
	launched, err := universeWorld.Fleet.Launch(ctx, spy, 1, appfleet.LaunchRequest{
		Target: coordinateOf(t, 1, 1, 2), TargetKind: domainfleet.TargetPlanet, Mission: domainfleet.MissionEspionage,
		Composition: domainfleet.Composition{unit.EspionageProbe: 3}, Percent: 100,
	}, "spy")
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	universeWorld.Clock.Set(launched.ArrivesAt)
	if _, err := universeWorld.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}
	var reportID int64
	if err := database.Read().QueryRowContext(ctx,
		"SELECT id FROM reports WHERE kind = 'espionage'").Scan(&reportID); err != nil {
		t.Fatal(err)
	}

	// Before sharing, the ally sees nothing of it.
	if _, err := universeWorld.Reports.Get(ctx, ally, reportID); !errors.Is(err, appreports.ErrNotFound) {
		t.Fatalf("an unshared report was readable: %v", err)
	}
	shared, err := universeWorld.Reports.SharedWithAlliance(ctx, ally)
	if err != nil || len(shared) != 0 {
		t.Fatalf("SharedWithAlliance() = %d %v", len(shared), err)
	}

	if err := universeWorld.Reports.Share(ctx, spy, reportID, true); err != nil {
		t.Fatalf("Share() error = %v", err)
	}
	detail, err := universeWorld.Reports.Get(ctx, ally, reportID)
	if err != nil {
		t.Fatalf("Get() of a shared report error = %v", err)
	}
	if !detail.Shared || detail.OwnerName != "player1" {
		t.Fatalf("shared report = %+v", detail.Summary)
	}
	shared, err = universeWorld.Reports.SharedWithAlliance(ctx, ally)
	if err != nil || len(shared) != 1 {
		t.Fatalf("SharedWithAlliance() = %d %v", len(shared), err)
	}

	// A player outside the alliance still sees nothing.
	if _, err := universeWorld.Reports.Get(ctx, stranger, reportID); !errors.Is(err, appreports.ErrNotFound) {
		t.Fatalf("a stranger read a shared report: %v", err)
	}
	if outside, err := universeWorld.Reports.SharedWithAlliance(ctx, stranger); err != nil || len(outside) != 0 {
		t.Fatalf("a stranger listed %d shared reports (%v)", len(outside), err)
	}

	// Only the owner decides, and taking it back closes the door again.
	if err := universeWorld.Reports.Share(ctx, ally, reportID, false); !errors.Is(err, appreports.ErrNotTheOwner) {
		t.Fatalf("a reader unshared a report: %v", err)
	}
	if err := universeWorld.Reports.Share(ctx, spy, reportID, false); err != nil {
		t.Fatalf("Share(false) error = %v", err)
	}
	if _, err := universeWorld.Reports.Get(ctx, ally, reportID); !errors.Is(err, appreports.ErrNotFound) {
		t.Fatalf("an unshared report stayed readable: %v", err)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM alliance_history WHERE action IN ('report_shared', 'report_unshared')", 2)
}

func TestLeavingTheAllianceCutsTheSharedReports(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, players := allianceUniverse(t, 2)
	spy, ally := players[0], players[1]
	joinAlliance(t, ctx, universeWorld, spy, ally, "player2")

	if _, err := database.Write().ExecContext(ctx, `
		INSERT INTO reports(recipient_player_id, kind, subject_type, subject_id, galaxy, system, position,
			occurred_at, payload_version, payload, created_at)
		VALUES (1, 'espionage', 'planet', 1, 1, 1, 1, '2042-09-10T11:12:13Z', 1, '{}', '2042-09-10T11:12:13Z')
	`); err != nil {
		t.Fatal(err)
	}
	if err := universeWorld.Reports.Share(ctx, spy, 1, true); err != nil {
		t.Fatalf("Share() error = %v", err)
	}
	if _, err := universeWorld.Reports.Get(ctx, ally, 1); err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if err := universeWorld.Alliance.Promote(ctx, spy, 2, "founder"); err != nil {
		t.Fatalf("Promote() error = %v", err)
	}
	if err := universeWorld.Alliance.Leave(ctx, spy); err != nil {
		t.Fatalf("Leave() error = %v", err)
	}
	if _, err := universeWorld.Reports.Get(ctx, ally, 1); !errors.Is(err, appreports.ErrNotFound) {
		t.Fatalf("a report survived its owner leaving the alliance: %v", err)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM reports WHERE shared_alliance_id IS NOT NULL", 0)
}

func TestSharingNeedsAnAlliance(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, players := allianceUniverse(t, 1)
	lonely := players[0]
	if _, err := database.Write().ExecContext(ctx, `
		INSERT INTO reports(recipient_player_id, kind, subject_type, subject_id, galaxy, system, position,
			occurred_at, payload_version, payload, created_at)
		VALUES (1, 'espionage', 'planet', 1, 1, 1, 1, '2042-09-10T11:12:13Z', 1, '{}', '2042-09-10T11:12:13Z')
	`); err != nil {
		t.Fatal(err)
	}
	if err := universeWorld.Reports.Share(ctx, lonely, 1, true); !errors.Is(err, appreports.ErrNotInAlliance) {
		t.Fatalf("sharing without an alliance error = %v", err)
	}
}
