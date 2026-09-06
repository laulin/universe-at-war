package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

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
