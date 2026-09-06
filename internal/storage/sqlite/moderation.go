package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	appmoderation "universeatwar/internal/app/moderation"
)

// ModerationRepository persists the sanctions of a universe.
type ModerationRepository struct {
	write *sql.DB
}

func NewModerationRepository(write *sql.DB) *ModerationRepository {
	return &ModerationRepository{write: write}
}

// IsPrivileged reports whether an account carries a role a moderator may not
// touch.
func (r *ModerationRepository) IsPrivileged(ctx context.Context, accountID int64) (bool, error) {
	var exists bool
	if err := r.write.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM accounts WHERE id = ?)
	`, accountID).Scan(&exists); err != nil {
		return false, fmt.Errorf("moderation repository: read account: %w", err)
	}
	if !exists {
		return false, appmoderation.ErrNotFound
	}
	var privileged bool
	if err := r.write.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM account_roles WHERE account_id = ? AND role IN ('ADMIN', 'MODERATOR'))
	`, accountID).Scan(&privileged); err != nil {
		return false, fmt.Errorf("moderation repository: read roles: %w", err)
	}
	return privileged, nil
}

// Ban records a sanction and closes every session the account holds. The empire
// is left exactly as it was: nothing of the game is touched here.
func (r *ModerationRepository) Ban(ctx context.Context, authorID, accountID int64, justification string,
	now time.Time, endsAt *time.Time) (appmoderation.Ban, error) {
	var ban appmoderation.Ban
	err := withWriteTx(ctx, r.write, "moderation repository: ban", func(tx *sql.Tx) error {
		var ends any
		if endsAt != nil {
			ends = timestamp(*endsAt)
		}
		result, err := tx.ExecContext(ctx, `
			INSERT INTO bans(account_id, author_account_id, starts_at, ends_at, justification, created_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, accountID, authorID, timestamp(now), ends, justification, timestamp(now))
		if err != nil {
			return fmt.Errorf("moderation repository: ban: %w", err)
		}
		ban.ID, err = result.LastInsertId()
		if err != nil {
			return fmt.Errorf("moderation repository: ban id: %w", err)
		}
		ban.AccountID, ban.Justification, ban.StartsAt, ban.EndsAt = accountID, justification, now, endsAt
		// A banned account is shown the door right away.
		if _, err := tx.ExecContext(ctx,
			"UPDATE sessions SET revoked_at = ? WHERE account_id = ? AND revoked_at IS NULL",
			timestamp(now), accountID); err != nil {
			return fmt.Errorf("moderation repository: close sessions: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO audit_log(actor_account_id, action, target_type, target_id, occurred_at, details)
			VALUES (?, 'account_banned', 'account', ?, ?, json_object('ban_id', ?, 'justification', ?))
		`, authorID, accountID, timestamp(now), ban.ID, justification); err != nil {
			return fmt.Errorf("moderation repository: audit ban: %w", err)
		}
		return nil
	})
	if err != nil {
		return appmoderation.Ban{}, err
	}
	return ban, nil
}

// Lift ends a sanction early.
func (r *ModerationRepository) Lift(ctx context.Context, authorID, banID int64, now time.Time) error {
	return withWriteTx(ctx, r.write, "moderation repository: lift", func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `
			UPDATE bans SET status = 'lifted', lifted_at = ?, lifted_by_account_id = ?
			WHERE id = ? AND status = 'active'
		`, timestamp(now), authorID, banID)
		if err != nil {
			return fmt.Errorf("moderation repository: lift: %w", err)
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return appmoderation.ErrBanNotFound
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO audit_log(actor_account_id, action, target_type, target_id, occurred_at, details)
			SELECT ?, 'account_unbanned', 'account', account_id, ?, json_object('ban_id', ?)
			FROM bans WHERE id = ?
		`, authorID, timestamp(now), banID, banID); err != nil {
			return fmt.Errorf("moderation repository: audit lift: %w", err)
		}
		return nil
	})
}

// Bans reports on every sanction, newest first.
func (r *ModerationRepository) Bans(ctx context.Context, now time.Time) ([]appmoderation.Ban, error) {
	rows, err := r.write.QueryContext(ctx, `
		SELECT b.id, b.account_id, a.username, COALESCE(author.username, ''), b.justification,
			b.starts_at, b.ends_at, b.lifted_at,
			EXISTS(SELECT 1 FROM account_roles r WHERE r.account_id = b.account_id AND r.role IN ('ADMIN', 'MODERATOR'))
		FROM bans b
		JOIN accounts a ON a.id = b.account_id
		LEFT JOIN accounts author ON author.id = b.author_account_id
		ORDER BY b.id DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("moderation repository: list bans: %w", err)
	}
	defer rows.Close()
	var bans []appmoderation.Ban
	for rows.Next() {
		var ban appmoderation.Ban
		var startsText string
		var endsText, liftedText sql.NullString
		if err := rows.Scan(&ban.ID, &ban.AccountID, &ban.Username, &ban.AuthorName, &ban.Justification,
			&startsText, &endsText, &liftedText, &ban.Administrator); err != nil {
			return nil, fmt.Errorf("moderation repository: scan ban: %w", err)
		}
		var parseErr error
		if ban.StartsAt, parseErr = time.Parse(time.RFC3339Nano, startsText); parseErr != nil {
			return nil, fmt.Errorf("moderation repository: parse start: %w", parseErr)
		}
		for _, moment := range []struct {
			text  sql.NullString
			field **time.Time
		}{{endsText, &ban.EndsAt}, {liftedText, &ban.LiftedAt}} {
			if !moment.text.Valid {
				continue
			}
			parsed, err := time.Parse(time.RFC3339Nano, moment.text.String)
			if err != nil {
				return nil, fmt.Errorf("moderation repository: parse date: %w", err)
			}
			*moment.field = &parsed
		}
		bans = append(bans, ban)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("moderation repository: iterate bans: %w", err)
	}
	return bans, nil
}
