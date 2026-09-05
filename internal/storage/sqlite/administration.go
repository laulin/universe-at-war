package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"universeatwar/internal/app/administration"
)

// AdministrationRepository persists privileged local operations.
type AdministrationRepository struct {
	write *sql.DB
}

func NewAdministrationRepository(write *sql.DB) *AdministrationRepository {
	return &AdministrationRepository{write: write}
}

func (r *AdministrationRepository) ResetAdministratorPassword(ctx context.Context, normalizedUsername, encodedHash string, now time.Time) error {
	transaction, err := r.write.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("administration repository: begin password reset: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	var accountID int64
	err = transaction.QueryRowContext(ctx, `
		SELECT a.id
		FROM accounts a
		JOIN account_roles ar ON ar.account_id = a.id AND ar.role = 'ADMIN'
		WHERE a.username_normalized = ?
	`, normalizedUsername).Scan(&accountID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			var exists bool
			if checkErr := transaction.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM accounts WHERE username_normalized = ?)", normalizedUsername).Scan(&exists); checkErr != nil {
				return fmt.Errorf("administration repository: inspect reset target: %w", checkErr)
			}
			if exists {
				return administration.ErrNotAdministrator
			}
			return administration.ErrAccountNotFound
		}
		return fmt.Errorf("administration repository: find administrator: %w", err)
	}

	timestamp := timestamp(now)
	if _, err := transaction.ExecContext(ctx, `
		UPDATE accounts SET version = version + 1, updated_at = ? WHERE id = ?
	`, timestamp, accountID); err != nil {
		return fmt.Errorf("administration repository: version reset account: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
		UPDATE password_credentials
		SET encoded_hash = ?, must_change_password = 1, updated_at = ?
		WHERE account_id = ?
	`, encodedHash, timestamp, accountID); err != nil {
		return fmt.Errorf("administration repository: replace credential: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
		UPDATE sessions SET revoked_at = ? WHERE account_id = ? AND revoked_at IS NULL
	`, timestamp, accountID); err != nil {
		return fmt.Errorf("administration repository: revoke sessions: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO audit_log(actor_account_id, action, target_type, target_id, occurred_at, details)
		VALUES (NULL, 'admin_password_reset', 'account', ?, ?, json_object('source', 'local_cli'))
	`, accountID, timestamp); err != nil {
		return fmt.Errorf("administration repository: audit password reset: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("administration repository: commit password reset: %w", err)
	}
	return nil
}
