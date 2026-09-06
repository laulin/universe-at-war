package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	appadmin "universeatwar/internal/app/administration"
)

// InvitationRepository persists the tickets into a closed universe. It only
// ever stores digests: reading the table gives nobody a way in.
type InvitationRepository struct {
	write *sql.DB
}

func NewInvitationRepository(write *sql.DB) *InvitationRepository {
	return &InvitationRepository{write: write}
}

// CreateInvitation mints one ticket.
func (r *InvitationRepository) CreateInvitation(ctx context.Context, authorID int64, digest, label string,
	now, expiresAt time.Time) (appadmin.Invitation, error) {
	var invitation appadmin.Invitation
	err := withWriteTx(ctx, r.write, "invitation repository: create", func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `
			INSERT INTO invitations(code_digest, label, created_by_account_id, created_at, expires_at)
			VALUES (?, ?, ?, ?, ?)
		`, digest, label, authorID, timestamp(now), timestamp(expiresAt))
		if err != nil {
			return fmt.Errorf("invitation repository: create: %w", err)
		}
		invitation.ID, err = result.LastInsertId()
		if err != nil {
			return fmt.Errorf("invitation repository: invitation id: %w", err)
		}
		invitation.Label = label
		invitation.CreatedAt = now
		invitation.ExpiresAt = expiresAt
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO audit_log(actor_account_id, action, target_type, target_id, occurred_at, details)
			VALUES (?, 'invitation_created', 'invitation', ?, ?, json_object('label', ?))
		`, authorID, invitation.ID, timestamp(now), label); err != nil {
			return fmt.Errorf("invitation repository: audit creation: %w", err)
		}
		return nil
	})
	if err != nil {
		return appadmin.Invitation{}, err
	}
	return invitation, nil
}

// Invitations reports on every ticket, newest first.
func (r *InvitationRepository) Invitations(ctx context.Context) ([]appadmin.Invitation, error) {
	rows, err := r.write.QueryContext(ctx, `
		SELECT i.id, i.label, i.created_at, i.expires_at, i.used_at, i.revoked_at,
			COALESCE(a.username, '')
		FROM invitations i
		LEFT JOIN accounts a ON a.id = i.used_by_account_id
		ORDER BY i.id DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("invitation repository: list: %w", err)
	}
	defer rows.Close()
	var invitations []appadmin.Invitation
	for rows.Next() {
		var invitation appadmin.Invitation
		var createdText, expiresText string
		var usedText, revokedText sql.NullString
		if err := rows.Scan(&invitation.ID, &invitation.Label, &createdText, &expiresText,
			&usedText, &revokedText, &invitation.UsedBy); err != nil {
			return nil, fmt.Errorf("invitation repository: scan: %w", err)
		}
		var parseErr error
		if invitation.CreatedAt, parseErr = time.Parse(time.RFC3339Nano, createdText); parseErr != nil {
			return nil, fmt.Errorf("invitation repository: parse creation: %w", parseErr)
		}
		if invitation.ExpiresAt, parseErr = time.Parse(time.RFC3339Nano, expiresText); parseErr != nil {
			return nil, fmt.Errorf("invitation repository: parse expiry: %w", parseErr)
		}
		if usedText.Valid {
			used, parseErr := time.Parse(time.RFC3339Nano, usedText.String)
			if parseErr != nil {
				return nil, fmt.Errorf("invitation repository: parse use: %w", parseErr)
			}
			invitation.UsedAt = &used
		}
		invitation.Revoked = revokedText.Valid
		invitations = append(invitations, invitation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("invitation repository: iterate: %w", err)
	}
	return invitations, nil
}

// RevokeInvitation withdraws a ticket nobody has used yet.
func (r *InvitationRepository) RevokeInvitation(ctx context.Context, invitationID int64, now time.Time) error {
	return withWriteTx(ctx, r.write, "invitation repository: revoke", func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `
			UPDATE invitations SET revoked_at = ?
			WHERE id = ? AND used_at IS NULL AND revoked_at IS NULL
		`, timestamp(now), invitationID)
		if err != nil {
			return fmt.Errorf("invitation repository: revoke: %w", err)
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return appadmin.ErrInvitationNotFound
		}
		return nil
	})
}
