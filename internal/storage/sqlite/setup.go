package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	appsetup "universeatwar/internal/app/setup"
	"universeatwar/internal/domain/server"
)

// SetupRepository persists the initial ruleset draft and activation.
type SetupRepository struct {
	write *sql.DB
}

func NewSetupRepository(write *sql.DB) *SetupRepository {
	return &SetupRepository{write: write}
}

func (r *SetupRepository) LoadOrCreate(ctx context.Context, actorID int64, defaultDocument []byte, now time.Time) (appsetup.StoredDraft, error) {
	transaction, err := r.write.BeginTx(ctx, nil)
	if err != nil {
		return appsetup.StoredDraft{}, fmt.Errorf("setup repository: begin load: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	var state server.State
	var stateVersion int64
	if err := transaction.QueryRowContext(ctx, "SELECT state, version FROM server_state WHERE id = 1").Scan(&state, &stateVersion); err != nil {
		return appsetup.StoredDraft{}, fmt.Errorf("setup repository: read server state: %w", err)
	}
	if state == server.Running {
		return appsetup.StoredDraft{}, appsetup.ErrAlreadyCompleted
	}
	if state != server.BootstrapPending && state != server.SetupInProgress {
		return appsetup.StoredDraft{}, fmt.Errorf("setup repository: server state %q does not allow setup", state)
	}

	timestamp := timestamp(now)
	if state == server.BootstrapPending {
		result, err := transaction.ExecContext(ctx, `
			UPDATE server_state SET state = ?, version = version + 1, updated_at = ?
			WHERE id = 1 AND state = ? AND version = ?
		`, string(server.SetupInProgress), timestamp, string(server.BootstrapPending), stateVersion)
		if err != nil {
			return appsetup.StoredDraft{}, fmt.Errorf("setup repository: start setup: %w", err)
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return appsetup.StoredDraft{}, appsetup.ErrConflict
		}
		if _, err := transaction.ExecContext(ctx, `
			INSERT INTO setup_drafts(
				id, document, current_step, status, version,
				created_by_account_id, created_at, updated_at
			) VALUES (1, ?, 1, 'draft', 1, ?, ?, ?)
		`, string(defaultDocument), actorID, timestamp, timestamp); err != nil {
			return appsetup.StoredDraft{}, fmt.Errorf("setup repository: create draft: %w", err)
		}
		if _, err := transaction.ExecContext(ctx, `
			INSERT INTO audit_log(actor_account_id, action, target_type, target_id, occurred_at)
			VALUES (?, 'setup_started', 'server', '1', ?)
		`, actorID, timestamp); err != nil {
			return appsetup.StoredDraft{}, fmt.Errorf("setup repository: audit start: %w", err)
		}
	}

	stored, err := scanSetupDraft(transaction.QueryRowContext(ctx, `
		SELECT document, current_step, version FROM setup_drafts WHERE id = 1 AND status = 'draft'
	`))
	if err != nil {
		return appsetup.StoredDraft{}, err
	}
	if err := transaction.Commit(); err != nil {
		return appsetup.StoredDraft{}, fmt.Errorf("setup repository: commit load: %w", err)
	}
	return stored, nil
}

func (r *SetupRepository) Save(ctx context.Context, actorID int64, step int, expectedVersion int64, document []byte, now time.Time) (appsetup.StoredDraft, error) {
	transaction, err := r.write.BeginTx(ctx, nil)
	if err != nil {
		return appsetup.StoredDraft{}, fmt.Errorf("setup repository: begin save: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	result, err := transaction.ExecContext(ctx, `
		UPDATE setup_drafts
		SET document = ?, current_step = ?, version = version + 1, updated_at = ?
		WHERE id = 1 AND status = 'draft' AND current_step = ? AND version = ?
	`, string(document), step+1, timestamp(now), step, expectedVersion)
	if err != nil {
		return appsetup.StoredDraft{}, fmt.Errorf("setup repository: save draft: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return appsetup.StoredDraft{}, fmt.Errorf("setup repository: inspect save: %w", err)
	}
	if affected != 1 {
		return appsetup.StoredDraft{}, appsetup.ErrConflict
	}
	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO audit_log(actor_account_id, action, target_type, target_id, occurred_at, details)
		VALUES (?, 'setup_step_saved', 'setup', '1', ?, json_object('step', ?))
	`, actorID, timestamp(now), step); err != nil {
		return appsetup.StoredDraft{}, fmt.Errorf("setup repository: audit save: %w", err)
	}
	stored, err := scanSetupDraft(transaction.QueryRowContext(ctx, `
		SELECT document, current_step, version FROM setup_drafts WHERE id = 1
	`))
	if err != nil {
		return appsetup.StoredDraft{}, err
	}
	if err := transaction.Commit(); err != nil {
		return appsetup.StoredDraft{}, fmt.Errorf("setup repository: commit save: %w", err)
	}
	return stored, nil
}

func (r *SetupRepository) Activate(ctx context.Context, actorID, expectedVersion int64, document []byte, checksum string, now time.Time) error {
	transaction, err := r.write.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("setup repository: begin activation: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	var state server.State
	if err := transaction.QueryRowContext(ctx, "SELECT state FROM server_state WHERE id = 1").Scan(&state); err != nil {
		return fmt.Errorf("setup repository: read activation state: %w", err)
	}
	if state == server.Running {
		return appsetup.ErrAlreadyCompleted
	}
	if state != server.SetupInProgress {
		return fmt.Errorf("setup repository: state %q cannot be activated", state)
	}
	var storedDocument []byte
	var storedVersion int64
	var currentStep int
	if err := transaction.QueryRowContext(ctx, `
		SELECT document, version, current_step FROM setup_drafts WHERE id = 1 AND status = 'draft'
	`).Scan(&storedDocument, &storedVersion, &currentStep); err != nil {
		return fmt.Errorf("setup repository: read activation draft: %w", err)
	}
	if storedVersion != expectedVersion || currentStep != 10 || !bytes.Equal(storedDocument, document) {
		return appsetup.ErrConflict
	}

	var rulesetVersion int64
	if err := transaction.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) + 1 FROM ruleset_versions").Scan(&rulesetVersion); err != nil {
		return fmt.Errorf("setup repository: select ruleset version: %w", err)
	}
	timestamp := timestamp(now)
	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO ruleset_versions(
			version, status, document, checksum, author_account_id, effective_at, created_at
		) VALUES (?, 'active', ?, ?, ?, ?, ?)
	`, rulesetVersion, string(document), checksum, actorID, timestamp, timestamp); err != nil {
		return fmt.Errorf("setup repository: create ruleset: %w", err)
	}
	draftResult, err := transaction.ExecContext(ctx, `
		UPDATE setup_drafts SET status = 'completed', version = version + 1, updated_at = ?
		WHERE id = 1 AND version = ? AND status = 'draft'
	`, timestamp, expectedVersion)
	if err != nil {
		return fmt.Errorf("setup repository: complete draft: %w", err)
	}
	if affected, _ := draftResult.RowsAffected(); affected != 1 {
		return appsetup.ErrConflict
	}
	stateResult, err := transaction.ExecContext(ctx, `
		UPDATE server_state SET state = ?, previous_state = NULL, version = version + 1, updated_at = ?
		WHERE id = 1 AND state = ?
	`, string(server.Running), timestamp, string(server.SetupInProgress))
	if err != nil {
		return fmt.Errorf("setup repository: start universe: %w", err)
	}
	if affected, _ := stateResult.RowsAffected(); affected != 1 {
		return appsetup.ErrConflict
	}
	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO audit_log(actor_account_id, action, target_type, target_id, occurred_at, details)
		VALUES (?, 'setup_completed', 'ruleset', ?, ?, json_object('version', ?))
	`, actorID, rulesetVersion, timestamp, rulesetVersion); err != nil {
		return fmt.Errorf("setup repository: audit activation: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("setup repository: commit activation: %w", err)
	}
	return nil
}

func scanSetupDraft(row rowScanner) (appsetup.StoredDraft, error) {
	var stored appsetup.StoredDraft
	if err := row.Scan(&stored.Document, &stored.CurrentStep, &stored.Version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return appsetup.StoredDraft{}, errors.New("setup repository: draft is missing")
		}
		return appsetup.StoredDraft{}, fmt.Errorf("setup repository: read draft: %w", err)
	}
	return stored, nil
}
