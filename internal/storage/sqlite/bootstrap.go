package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	appbootstrap "universeatwar/internal/app/bootstrap"
	"universeatwar/internal/domain/server"
)

// BootstrapRepository persists the initial administrator and server state.
type BootstrapRepository struct {
	database *sql.DB
}

// NewBootstrapRepository creates a bootstrap repository on the serialized
// write handle.
func NewBootstrapRepository(database *sql.DB) *BootstrapRepository {
	return &BootstrapRepository{database: database}
}

// Initialize creates all bootstrap records in one transaction. Existing
// server state makes the operation an idempotent no-op.
func (r *BootstrapRepository) Initialize(ctx context.Context, record appbootstrap.Record) (bool, error) {
	transaction, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("bootstrap repository: begin: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	var stateCount int
	if err := transaction.QueryRowContext(ctx, "SELECT COUNT(*) FROM server_state").Scan(&stateCount); err != nil {
		return false, fmt.Errorf("bootstrap repository: inspect server state: %w", err)
	}
	if stateCount != 0 {
		return false, nil
	}

	var accountCount int
	if err := transaction.QueryRowContext(ctx, "SELECT COUNT(*) FROM accounts").Scan(&accountCount); err != nil {
		return false, fmt.Errorf("bootstrap repository: inspect accounts: %w", err)
	}
	if accountCount != 0 {
		return false, errors.New("bootstrap repository: accounts exist without server state")
	}

	timestamp := record.OccurredAt.UTC().Format(time.RFC3339Nano)
	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO app_metadata(
			id, created_by_version, last_opened_by_version, created_at, updated_at
		) VALUES (1, ?, ?, ?, ?)
	`, record.Application, record.Application, timestamp, timestamp); err != nil {
		return false, fmt.Errorf("bootstrap repository: create metadata: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO server_state(id, state, previous_state, version, updated_at)
		VALUES (1, ?, NULL, 1, ?)
	`, string(server.BootstrapPending), timestamp); err != nil {
		return false, fmt.Errorf("bootstrap repository: create server state: %w", err)
	}

	accountResult, err := transaction.ExecContext(ctx, `
		INSERT INTO accounts(username, username_normalized, status, version, created_at, updated_at)
		VALUES (?, ?, 'active', 1, ?, ?)
	`, record.Username, record.NormalizedName, timestamp, timestamp)
	if err != nil {
		return false, fmt.Errorf("bootstrap repository: create account: %w", err)
	}
	accountID, err := accountResult.LastInsertId()
	if err != nil {
		return false, fmt.Errorf("bootstrap repository: read account id: %w", err)
	}

	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO account_roles(account_id, role, granted_at, granted_by_account_id)
		VALUES (?, 'ADMIN', ?, ?)
	`, accountID, timestamp, accountID); err != nil {
		return false, fmt.Errorf("bootstrap repository: grant admin role: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO password_credentials(account_id, encoded_hash, must_change_password, updated_at)
		VALUES (?, ?, 1, ?)
	`, accountID, record.EncodedHash, timestamp); err != nil {
		return false, fmt.Errorf("bootstrap repository: create credential: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO audit_log(actor_account_id, action, target_type, target_id, occurred_at, details)
		VALUES (?, 'bootstrap_admin_created', 'account', ?, ?, json_object('username', ?))
	`, accountID, accountID, timestamp, record.Username); err != nil {
		return false, fmt.Errorf("bootstrap repository: append audit: %w", err)
	}

	if err := transaction.Commit(); err != nil {
		return false, fmt.Errorf("bootstrap repository: commit: %w", err)
	}
	return true, nil
}
