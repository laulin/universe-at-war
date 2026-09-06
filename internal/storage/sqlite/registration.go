package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	appregistration "universeatwar/internal/app/registration"
	"universeatwar/internal/domain/server"
)

// RegistrationRepository creates player accounts in one write transaction.
type RegistrationRepository struct {
	read  *sql.DB
	write *sql.DB
}

func NewRegistrationRepository(read, write *sql.DB) *RegistrationRepository {
	return &RegistrationRepository{read: read, write: write}
}

// RegistrationPolicy reads the policy of the active ruleset. Registrations are
// only meaningful once the universe runs.
func (r *RegistrationRepository) RegistrationPolicy(ctx context.Context) (string, error) {
	var state string
	if err := r.read.QueryRowContext(ctx, "SELECT state FROM server_state WHERE id = 1").Scan(&state); err != nil {
		return "", fmt.Errorf("registration repository: read server state: %w", err)
	}
	if server.State(state) != server.Running {
		return "", appregistration.ErrServerNotRunning
	}
	var policy string
	if err := r.read.QueryRowContext(ctx, `
		SELECT json_extract(document, '$.identity.registration_policy')
		FROM ruleset_versions WHERE status = 'active' ORDER BY version DESC LIMIT 1
	`).Scan(&policy); err != nil {
		return "", fmt.Errorf("registration repository: read registration policy: %w", err)
	}
	return policy, nil
}

// CreateAccount inserts the account, its player role, its credential and the
// audit entry together, so a half-created account can never sign in.
func (r *RegistrationRepository) CreateAccount(ctx context.Context, record appregistration.Record) (int64, error) {
	var accountID int64
	err := withWriteTx(ctx, r.write, "registration repository: create account", func(tx *sql.Tx) error {
		moment := timestamp(record.OccurredAt)
		result, err := tx.ExecContext(ctx, `
			INSERT INTO accounts(username, username_normalized, created_at, updated_at) VALUES (?, ?, ?, ?)
		`, record.Username, record.NormalizedName, moment, moment)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return appregistration.ErrUsernameTaken
			}
			return fmt.Errorf("registration repository: insert account: %w", err)
		}
		accountID, err = result.LastInsertId()
		if err != nil {
			return fmt.Errorf("registration repository: account id: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO account_roles(account_id, role, granted_at) VALUES (?, 'PLAYER', ?)
		`, accountID, moment); err != nil {
			return fmt.Errorf("registration repository: grant player role: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO password_credentials(account_id, encoded_hash, must_change_password, updated_at) VALUES (?, ?, 0, ?)
		`, accountID, record.EncodedHash, moment); err != nil {
			return fmt.Errorf("registration repository: store credential: %w", err)
		}
		if record.InvitationDigest != "" {
			// The ticket is spent in the very transaction that creates the
			// account: a code that is used, revoked or out of date lets nobody
			// in, and a refusal here undoes the account with it.
			claimed, err := tx.ExecContext(ctx, `
				UPDATE invitations SET used_at = ?, used_by_account_id = ?
				WHERE code_digest = ? AND used_at IS NULL AND revoked_at IS NULL AND expires_at > ?
			`, moment, accountID, record.InvitationDigest, moment)
			if err != nil {
				return fmt.Errorf("registration repository: claim invitation: %w", err)
			}
			if affected, _ := claimed.RowsAffected(); affected != 1 {
				return appregistration.ErrInvalidInvitation
			}
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO audit_log(actor_account_id, action, target_type, target_id, occurred_at)
			VALUES (?, 'account_registered', 'account', ?, ?)
		`, accountID, accountID, moment); err != nil {
			return fmt.Errorf("registration repository: audit registration: %w", err)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return accountID, nil
}
