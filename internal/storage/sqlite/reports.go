package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	appalliance "universeatwar/internal/app/alliance"
	appreports "universeatwar/internal/app/reports"
	"universeatwar/internal/domain/report"
	"universeatwar/internal/domain/universe"
)

// insertReport stores one immutable report. The payload has already been
// filtered for its recipient: nothing is hidden at display time.
func insertReport(ctx context.Context, tx *sql.Tx, recipientPlayerID int64, kind report.Kind,
	subjectType string, subjectID int64, at universe.Coordinate, occurredAt time.Time, payload any) (int64, error) {
	version, document, err := report.Marshal(kind, payload)
	if err != nil {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO reports(recipient_player_id, kind, subject_type, subject_id, galaxy, system, position,
			occurred_at, payload_version, payload, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, recipientPlayerID, string(kind), subjectType, subjectID, at.Galaxy, at.System, at.Position,
		timestamp(occurredAt), version, string(document), timestamp(occurredAt))
	if err != nil {
		return 0, fmt.Errorf("reports repository: insert %s: %w", kind, err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("reports repository: report id: %w", err)
	}
	return id, nil
}

// ReportsRepository reads the reports of one player and never anybody else's.
type ReportsRepository struct {
	read  *sql.DB
	write *sql.DB
}

func NewReportsRepository(read, write *sql.DB) *ReportsRepository {
	return &ReportsRepository{read: read, write: write}
}

// List returns one page of reports, newest first.
func (r *ReportsRepository) List(ctx context.Context, accountID int64, filter appreports.Filter, now time.Time) ([]appreports.Summary, error) {
	query := `
		SELECT p.id, p.kind, p.galaxy, p.system, p.position, p.occurred_at, p.read_at,
			p.shared_alliance_id IS NOT NULL, pl.display_name
		FROM reports p
		JOIN players pl ON pl.id = p.recipient_player_id
		WHERE pl.account_id = ?
	`
	arguments := []any{accountID}
	if filter.Kind != "" {
		query += " AND p.kind = ?"
		arguments = append(arguments, string(filter.Kind))
	}
	if filter.UnreadOnly {
		query += " AND p.read_at IS NULL"
	}
	query += " ORDER BY p.occurred_at DESC, p.id DESC LIMIT ? OFFSET ?"
	arguments = append(arguments, appreports.PageSize, (filter.Page-1)*appreports.PageSize)

	rows, err := r.read.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("reports repository: list: %w", err)
	}
	defer rows.Close()
	recent, err := reportFreshness(ctx, r.read)
	if err != nil {
		return nil, err
	}
	var summaries []appreports.Summary
	for rows.Next() {
		summary, err := scanSummary(rows, now, recent)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reports repository: iterate: %w", err)
	}
	return summaries, nil
}

// Get returns one report of the player, decoded.
func (r *ReportsRepository) Get(ctx context.Context, accountID, reportID int64, now time.Time) (appreports.Detail, error) {
	var kind, occurredText string
	var summary appreports.Summary
	var readAt sql.NullString
	var version int
	var document string
	// A report is readable by its recipient, or by the alliance it was shared
	// with, and by nobody else.
	err := r.read.QueryRowContext(ctx, `
		SELECT p.id, p.kind, p.galaxy, p.system, p.position, p.occurred_at, p.read_at, p.payload_version, p.payload,
			p.shared_alliance_id IS NOT NULL, owner.display_name, owner.account_id = ?
		FROM reports p
		JOIN players owner ON owner.id = p.recipient_player_id
		LEFT JOIN players viewer ON viewer.account_id = ?
		LEFT JOIN alliance_members membership ON membership.player_id = viewer.id
		WHERE p.id = ?
		  AND (owner.account_id = ? OR (p.shared_alliance_id IS NOT NULL AND p.shared_alliance_id = membership.alliance_id))
	`, accountID, accountID, reportID, accountID).Scan(&summary.ID, &kind, &summary.Coordinate.Galaxy, &summary.Coordinate.System,
		&summary.Coordinate.Position, &occurredText, &readAt, &version, &document, &summary.Shared,
		&summary.OwnerName, &summary.Own)
	if errors.Is(err, sql.ErrNoRows) {
		return appreports.Detail{}, appreports.ErrNotFound
	}
	if err != nil {
		return appreports.Detail{}, fmt.Errorf("reports repository: read: %w", err)
	}
	summary.Kind = report.Kind(kind)
	summary.Read = readAt.Valid
	summary.OccurredAt, err = time.Parse(time.RFC3339Nano, occurredText)
	if err != nil {
		return appreports.Detail{}, fmt.Errorf("reports repository: parse date: %w", err)
	}
	recent, err := reportFreshness(ctx, r.read)
	if err != nil {
		return appreports.Detail{}, err
	}
	summary.Freshness = report.FreshnessOf(summary.OccurredAt, now, recent)
	payload, err := report.Unmarshal(summary.Kind, version, []byte(document))
	if err != nil {
		return appreports.Detail{}, err
	}
	return appreports.Detail{Summary: summary, Payload: payload}, nil
}

// MarkRead is idempotent and only ever touches a report of the player.
func (r *ReportsRepository) MarkRead(ctx context.Context, accountID, reportID int64, now time.Time) error {
	return withWriteTx(ctx, r.write, "reports repository: mark read", func(tx *sql.Tx) error {
		var exists bool
		if err := tx.QueryRowContext(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM reports p JOIN players pl ON pl.id = p.recipient_player_id
				WHERE p.id = ? AND pl.account_id = ?
			)
		`, reportID, accountID).Scan(&exists); err != nil {
			return fmt.Errorf("reports repository: inspect report: %w", err)
		}
		if !exists {
			return appreports.ErrNotFound
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE reports SET read_at = ?
			WHERE id = ? AND read_at IS NULL
			  AND recipient_player_id IN (SELECT id FROM players WHERE account_id = ?)
		`, timestamp(now), reportID, accountID); err != nil {
			return fmt.Errorf("reports repository: mark read: %w", err)
		}
		return nil
	})
}

// UnreadHostile counts the unread reports that announce a threat.
func (r *ReportsRepository) UnreadHostile(ctx context.Context, accountID int64) (int, error) {
	var count int
	if err := r.read.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM reports p
		JOIN players pl ON pl.id = p.recipient_player_id
		WHERE pl.account_id = ? AND p.read_at IS NULL AND p.kind IN ('combat_defense', 'espionage_detected')
	`, accountID).Scan(&count); err != nil {
		return 0, fmt.Errorf("reports repository: count hostile: %w", err)
	}
	return count, nil
}

func scanSummary(rows *sql.Rows, now time.Time, recent time.Duration) (appreports.Summary, error) {
	var summary appreports.Summary
	var kind, occurredText string
	var readAt sql.NullString
	if err := rows.Scan(&summary.ID, &kind, &summary.Coordinate.Galaxy, &summary.Coordinate.System,
		&summary.Coordinate.Position, &occurredText, &readAt, &summary.Shared, &summary.OwnerName); err != nil {
		return appreports.Summary{}, fmt.Errorf("reports repository: scan: %w", err)
	}
	summary.Kind = report.Kind(kind)
	summary.Read = readAt.Valid
	occurredAt, err := time.Parse(time.RFC3339Nano, occurredText)
	if err != nil {
		return appreports.Summary{}, fmt.Errorf("reports repository: parse date: %w", err)
	}
	summary.OccurredAt = occurredAt
	summary.Freshness = report.FreshnessOf(occurredAt, now, recent)
	return summary, nil
}

// reportFreshness reads how long a report stays trustworthy in this universe.
func reportFreshness(ctx context.Context, database *sql.DB) (time.Duration, error) {
	var seconds int
	err := database.QueryRowContext(ctx, `
		SELECT json_extract(document, '$.espionage.recent_report_seconds')
		FROM ruleset_versions WHERE status = 'active' ORDER BY version DESC LIMIT 1
	`).Scan(&seconds)
	if err != nil {
		return 0, fmt.Errorf("reports repository: read freshness: %w", err)
	}
	if seconds <= 0 {
		seconds = 3600
	}
	return time.Duration(seconds) * time.Second, nil
}

// Share puts one report on the alliance table, or takes it back. Only its
// recipient may decide.
func (r *ReportsRepository) Share(ctx context.Context, accountID, reportID int64, shared bool, now time.Time) error {
	return withWriteTx(ctx, r.write, "reports repository: share", func(tx *sql.Tx) error {
		configured, _, err := activeRuleset(ctx, tx)
		if err != nil {
			return err
		}
		if !configured.Team.ReportSharingEnabled {
			return appreports.ErrSharingOff
		}
		playerID, err := playerByAccount(ctx, tx, accountID)
		if err != nil {
			return err
		}
		var owner int64
		err = tx.QueryRowContext(ctx, "SELECT recipient_player_id FROM reports WHERE id = ?", reportID).Scan(&owner)
		if errors.Is(err, sql.ErrNoRows) {
			return appreports.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("reports repository: read report owner: %w", err)
		}
		if owner != playerID {
			return appreports.ErrNotTheOwner
		}
		allianceID, _, err := membership(ctx, tx, playerID)
		if err != nil {
			if errors.Is(err, appalliance.ErrNotAMember) {
				return appreports.ErrNotInAlliance
			}
			return err
		}
		var value any
		if shared {
			value = allianceID
		}
		if _, err := tx.ExecContext(ctx,
			"UPDATE reports SET shared_alliance_id = ? WHERE id = ? AND recipient_player_id = ?",
			value, reportID, playerID); err != nil {
			return fmt.Errorf("reports repository: share report: %w", err)
		}
		action := "report_shared"
		if !shared {
			action = "report_unshared"
		}
		return recordHistory(ctx, tx, allianceID, playerID, action, 0, now,
			fmt.Sprintf("json_object('report_id', %d)", reportID))
	})
}

// SharedWithAlliance lists what the other members have put on the table.
func (r *ReportsRepository) SharedWithAlliance(ctx context.Context, accountID int64, now time.Time) ([]appreports.Summary, error) {
	rows, err := r.read.QueryContext(ctx, `
		SELECT p.id, p.kind, p.galaxy, p.system, p.position, p.occurred_at, p.read_at, 1, owner.display_name
		FROM reports p
		JOIN players owner ON owner.id = p.recipient_player_id
		JOIN players viewer ON viewer.account_id = ?
		JOIN alliance_members membership ON membership.player_id = viewer.id
		WHERE p.shared_alliance_id = membership.alliance_id
		ORDER BY p.occurred_at DESC, p.id DESC LIMIT ?
	`, accountID, appreports.PageSize)
	if err != nil {
		return nil, fmt.Errorf("reports repository: list shared: %w", err)
	}
	defer rows.Close()
	recent, err := reportFreshness(ctx, r.read)
	if err != nil {
		return nil, err
	}
	var summaries []appreports.Summary
	for rows.Next() {
		summary, err := scanSummary(rows, now, recent)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, summary)
	}
	return summaries, rows.Err()
}
