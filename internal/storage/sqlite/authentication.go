package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	appauth "universeatwar/internal/app/authentication"
)

// AuthenticationRepository persists credentials and opaque sessions.
type AuthenticationRepository struct {
	read  *sql.DB
	write *sql.DB
}

// NewAuthenticationRepository creates a repository with the configured read
// and serialized write handles.
func NewAuthenticationRepository(read, write *sql.DB) *AuthenticationRepository {
	return &AuthenticationRepository{read: read, write: write}
}

func (r *AuthenticationRepository) FindCredentialByUsername(ctx context.Context, normalized string, now time.Time) (appauth.Credential, error) {
	return scanCredential(r.read.QueryRowContext(ctx, credentialQuery("a.username_normalized = ?1"), normalized, timestamp(now), timestamp(now)))
}

func (r *AuthenticationRepository) FindCredentialByID(ctx context.Context, accountID int64, now time.Time) (appauth.Credential, error) {
	return scanCredential(r.read.QueryRowContext(ctx, credentialQuery("a.id = ?1"), accountID, timestamp(now), timestamp(now)))
}

func credentialQuery(predicate string) string {
	return `
		SELECT a.id, a.username, c.encoded_hash, c.must_change_password, a.version,
		       (a.status <> 'active' OR EXISTS (
		           SELECT 1 FROM bans b
		           WHERE b.account_id = a.id
		             AND b.status = 'active'
		             AND b.starts_at <= ?2
		             AND (b.ends_at IS NULL OR b.ends_at > ?3)
		       )) AS unavailable
		FROM accounts a
		JOIN password_credentials c ON c.account_id = a.id
		WHERE ` + predicate
}

type rowScanner interface {
	Scan(...any) error
}

func scanCredential(row rowScanner) (appauth.Credential, error) {
	var credential appauth.Credential
	if err := row.Scan(
		&credential.AccountID,
		&credential.Username,
		&credential.EncodedHash,
		&credential.MustChangePassword,
		&credential.Version,
		&credential.Unavailable,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return appauth.Credential{}, appauth.ErrCredentialNotFound
		}
		return appauth.Credential{}, fmt.Errorf("authentication repository: find credential: %w", err)
	}
	return credential, nil
}

func (r *AuthenticationRepository) RecordFailedLogin(ctx context.Context, normalized string, now time.Time) error {
	_, err := r.write.ExecContext(ctx, `
		INSERT INTO audit_log(actor_account_id, action, target_type, target_id, occurred_at, details)
		VALUES (
			(SELECT id FROM accounts WHERE username_normalized = ?),
			'login_failed', 'account', NULL, ?, json_object('username', ?)
		)
	`, normalized, timestamp(now), normalized)
	if err != nil {
		return fmt.Errorf("authentication repository: audit failed login: %w", err)
	}
	return nil
}

func (r *AuthenticationRepository) CreateSession(ctx context.Context, record appauth.SessionRecord) error {
	transaction, err := r.write.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("authentication repository: begin login: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	var currentVersion int64
	var available bool
	if err := transaction.QueryRowContext(ctx, `
		SELECT a.version,
		       (a.status = 'active' AND NOT EXISTS (
		           SELECT 1 FROM bans b
		           WHERE b.account_id = a.id AND b.status = 'active'
		             AND b.starts_at <= ? AND (b.ends_at IS NULL OR b.ends_at > ?)
		       ))
		FROM accounts a WHERE a.id = ?
	`, timestamp(record.IssuedAt), timestamp(record.IssuedAt), record.AccountID).Scan(&currentVersion, &available); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return appauth.ErrConflict
		}
		return fmt.Errorf("authentication repository: verify login account: %w", err)
	}
	if currentVersion != record.AccountVersion || !available {
		return appauth.ErrConflict
	}
	if record.Rehashed != "" {
		if _, err := transaction.ExecContext(ctx, `
			UPDATE password_credentials SET encoded_hash = ?, updated_at = ? WHERE account_id = ?
		`, record.Rehashed, timestamp(record.IssuedAt), record.AccountID); err != nil {
			return fmt.Errorf("authentication repository: rehash password: %w", err)
		}
	}
	if err := insertSession(ctx, transaction, record.AccountID, record.TokenDigest, record.IssuedAt, record.ExpiresAt); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO audit_log(actor_account_id, action, target_type, target_id, occurred_at)
		VALUES (?, 'login_succeeded', 'account', ?, ?)
	`, record.AccountID, record.AccountID, timestamp(record.IssuedAt)); err != nil {
		return fmt.Errorf("authentication repository: audit login: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("authentication repository: commit login: %w", err)
	}
	return nil
}

func (r *AuthenticationRepository) ResolveSession(ctx context.Context, digest []byte, now time.Time) (appauth.Principal, error) {
	var principal appauth.Principal
	var roles string
	err := r.read.QueryRowContext(ctx, `
		SELECT a.id, a.username, c.must_change_password, group_concat(ar.role, ',')
		FROM sessions s
		JOIN accounts a ON a.id = s.account_id
		JOIN password_credentials c ON c.account_id = a.id
		JOIN account_roles ar ON ar.account_id = a.id
		WHERE s.token_digest = ? AND s.revoked_at IS NULL AND s.expires_at > ?
		  AND a.status = 'active'
		  AND NOT EXISTS (
		      SELECT 1 FROM bans b
		      WHERE b.account_id = a.id AND b.status = 'active'
		        AND b.starts_at <= ? AND (b.ends_at IS NULL OR b.ends_at > ?)
		  )
		GROUP BY a.id, a.username, c.must_change_password
	`, digest, timestamp(now), timestamp(now), timestamp(now)).Scan(
		&principal.AccountID, &principal.Username, &principal.MustChangePassword, &roles,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return appauth.Principal{}, appauth.ErrCredentialNotFound
		}
		return appauth.Principal{}, fmt.Errorf("authentication repository: resolve session: %w", err)
	}
	for _, role := range strings.Split(roles, ",") {
		principal.Roles = append(principal.Roles, appauth.Role(role))
	}
	return principal, nil
}

func (r *AuthenticationRepository) ChangePassword(ctx context.Context, change appauth.PasswordChange) error {
	transaction, err := r.write.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("authentication repository: begin password change: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	var sessionValid bool
	if err := transaction.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM sessions
			WHERE account_id = ? AND token_digest = ? AND revoked_at IS NULL AND expires_at > ?
		)
	`, change.AccountID, change.OldTokenDigest, timestamp(change.IssuedAt)).Scan(&sessionValid); err != nil {
		return fmt.Errorf("authentication repository: verify password-change session: %w", err)
	}
	if !sessionValid {
		return appauth.ErrInvalidSession
	}

	result, err := transaction.ExecContext(ctx, `
		UPDATE accounts SET version = version + 1, updated_at = ?
		WHERE id = ? AND version = ? AND status = 'active'
		  AND NOT EXISTS (
		      SELECT 1 FROM bans b
		      WHERE b.account_id = accounts.id AND b.status = 'active'
		        AND b.starts_at <= ? AND (b.ends_at IS NULL OR b.ends_at > ?)
		  )
	`, timestamp(change.IssuedAt), change.AccountID, change.AccountVersion, timestamp(change.IssuedAt), timestamp(change.IssuedAt))
	if err != nil {
		return fmt.Errorf("authentication repository: version account: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("authentication repository: inspect account update: %w", err)
	}
	if affected != 1 {
		return appauth.ErrConflict
	}
	if _, err := transaction.ExecContext(ctx, `
		UPDATE password_credentials
		SET encoded_hash = ?, must_change_password = 0, updated_at = ?
		WHERE account_id = ?
	`, change.EncodedHash, timestamp(change.IssuedAt), change.AccountID); err != nil {
		return fmt.Errorf("authentication repository: update credential: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
		UPDATE sessions SET revoked_at = ? WHERE account_id = ? AND revoked_at IS NULL
	`, timestamp(change.IssuedAt), change.AccountID); err != nil {
		return fmt.Errorf("authentication repository: revoke sessions: %w", err)
	}
	if err := insertSession(ctx, transaction, change.AccountID, change.NewTokenDigest, change.IssuedAt, change.ExpiresAt); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO audit_log(actor_account_id, action, target_type, target_id, occurred_at)
		VALUES (?, 'password_changed', 'account', ?, ?)
	`, change.AccountID, change.AccountID, timestamp(change.IssuedAt)); err != nil {
		return fmt.Errorf("authentication repository: audit password change: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("authentication repository: commit password change: %w", err)
	}
	return nil
}

type statementExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func insertSession(ctx context.Context, executor statementExecutor, accountID int64, digest []byte, issuedAt, expiresAt time.Time) error {
	_, err := executor.ExecContext(ctx, `
		INSERT INTO sessions(account_id, token_digest, created_at, expires_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?)
	`, accountID, digest, timestamp(issuedAt), timestamp(expiresAt), timestamp(issuedAt))
	if err != nil {
		return fmt.Errorf("authentication repository: create session: %w", err)
	}
	return nil
}

func timestamp(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}
