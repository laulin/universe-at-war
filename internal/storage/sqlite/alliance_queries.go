package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	appalliance "universeatwar/internal/app/alliance"
	appeconomy "universeatwar/internal/app/economy"
	domainalliance "universeatwar/internal/domain/alliance"
	"universeatwar/internal/domain/rules"
)

// playerByAccount resolves the player of an account, refusing an account that
// has not founded an empire yet.
func playerByAccount(ctx context.Context, tx *sql.Tx, accountID int64) (int64, error) {
	var playerID int64
	err := tx.QueryRowContext(ctx, "SELECT id FROM players WHERE account_id = ?", accountID).Scan(&playerID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, appeconomy.ErrNoEmpire
	}
	if err != nil {
		return 0, fmt.Errorf("alliance repository: read player: %w", err)
	}
	return playerID, nil
}

// membership returns the alliance and the rank of a player.
func membership(ctx context.Context, tx *sql.Tx, playerID int64) (int64, domainalliance.Role, error) {
	var allianceID int64
	var role string
	err := tx.QueryRowContext(ctx,
		"SELECT alliance_id, role FROM alliance_members WHERE player_id = ?", playerID).Scan(&allianceID, &role)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", appalliance.ErrNotAMember
	}
	if err != nil {
		return 0, "", fmt.Errorf("alliance repository: read membership: %w", err)
	}
	return allianceID, domainalliance.Role(role), nil
}

// actingMember resolves who is acting, from which alliance and under which
// rules, which every mutation needs before it decides anything.
func actingMember(ctx context.Context, tx *sql.Tx, accountID int64) (int64, domainalliance.Role, int64, rules.Ruleset, error) {
	playerID, err := playerByAccount(ctx, tx, accountID)
	if err != nil {
		return 0, "", 0, rules.Ruleset{}, err
	}
	allianceID, role, err := membership(ctx, tx, playerID)
	if err != nil {
		return 0, "", 0, rules.Ruleset{}, err
	}
	configured, _, err := activeRuleset(ctx, tx)
	if err != nil {
		return 0, "", 0, rules.Ruleset{}, err
	}
	return allianceID, role, playerID, configured, nil
}

func addMember(ctx context.Context, tx *sql.Tx, allianceID, playerID int64, role domainalliance.Role, now time.Time) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO alliance_members(player_id, alliance_id, role, joined_at) VALUES (?, ?, ?, ?)
	`, playerID, allianceID, string(role), timestamp(now)); err != nil {
		return fmt.Errorf("alliance repository: add member: %w", err)
	}
	return nil
}

// removeMember drops a membership and cuts the player off from the reports they
// had shared with the team.
func removeMember(ctx context.Context, tx *sql.Tx, playerID int64) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM alliance_members WHERE player_id = ?", playerID); err != nil {
		return fmt.Errorf("alliance repository: remove member: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE reports SET shared_alliance_id = NULL WHERE recipient_player_id = ?", playerID); err != nil {
		return fmt.Errorf("alliance repository: unshare reports: %w", err)
	}
	return nil
}

func memberCount(ctx context.Context, tx *sql.Tx, allianceID int64) (int, error) {
	var count int
	if err := tx.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM alliance_members WHERE alliance_id = ?", allianceID).Scan(&count); err != nil {
		return 0, fmt.Errorf("alliance repository: count members: %w", err)
	}
	return count, nil
}

func allianceHasRoom(ctx context.Context, tx *sql.Tx, allianceID int64, maximum int) error {
	count, err := memberCount(ctx, tx, allianceID)
	if err != nil {
		return err
	}
	if maximum > 0 && count >= maximum {
		return appalliance.ErrFull
	}
	return nil
}

// ownInvitation returns the pending invitation of a player. A zero alliance
// means the invitation was already answered, which makes a replay a no-op.
func ownInvitation(ctx context.Context, tx *sql.Tx, invitationID, playerID int64) (int64, time.Time, error) {
	var allianceID int64
	var status, expiresText string
	err := tx.QueryRowContext(ctx,
		"SELECT alliance_id, status, expires_at FROM alliance_invitations WHERE id = ? AND player_id = ?",
		invitationID, playerID).Scan(&allianceID, &status, &expiresText)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, time.Time{}, appalliance.ErrNoInvitation
	}
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("alliance repository: read invitation: %w", err)
	}
	if status != "pending" {
		return 0, time.Time{}, nil
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, expiresText)
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("alliance repository: parse expiry: %w", err)
	}
	return allianceID, expiresAt, nil
}

func resolveInvitation(ctx context.Context, tx *sql.Tx, invitationID int64, status string, now time.Time) error {
	if _, err := tx.ExecContext(ctx,
		"UPDATE alliance_invitations SET status = ?, resolved_at = ? WHERE id = ? AND status = 'pending'",
		status, timestamp(now), invitationID); err != nil {
		return fmt.Errorf("alliance repository: resolve invitation: %w", err)
	}
	return nil
}

// recordHistory appends one line to the append-only story of the alliance.
func recordHistory(ctx context.Context, tx *sql.Tx, allianceID, actorID int64, action string, targetID int64, now time.Time, details string) error {
	var target any
	if targetID > 0 {
		target = targetID
	}
	query := fmt.Sprintf(`
		INSERT INTO alliance_history(alliance_id, actor_player_id, action, target_player_id, occurred_at, details)
		VALUES (?, ?, ?, ?, ?, %s)
	`, details)
	if _, err := tx.ExecContext(ctx, query, allianceID, actorID, action, target, timestamp(now)); err != nil {
		return fmt.Errorf("alliance repository: record history: %w", err)
	}
	return nil
}

// loadProfile gathers everything a member may read about their alliance.
func loadProfile(ctx context.Context, tx *sql.Tx, allianceID int64, now time.Time) (appalliance.Profile, error) {
	var profile appalliance.Profile
	if err := tx.QueryRowContext(ctx,
		"SELECT id, name, tag, description FROM alliances WHERE id = ?", allianceID).
		Scan(&profile.ID, &profile.Name, &profile.Tag, &profile.Description); err != nil {
		return appalliance.Profile{}, fmt.Errorf("alliance repository: read alliance: %w", err)
	}
	members, err := loadMembers(ctx, tx, allianceID)
	if err != nil {
		return appalliance.Profile{}, err
	}
	profile.Members = members
	profile.Invitations, err = pendingInvitations(ctx, tx, "i.alliance_id = ?", allianceID, now)
	if err != nil {
		return appalliance.Profile{}, err
	}
	profile.Relations, err = loadRelations(ctx, tx, allianceID)
	if err != nil {
		return appalliance.Profile{}, err
	}
	profile.Received, err = loadReceivedRelations(ctx, tx, allianceID)
	if err != nil {
		return appalliance.Profile{}, err
	}
	profile.History, err = loadHistory(ctx, tx, allianceID)
	if err != nil {
		return appalliance.Profile{}, err
	}
	return profile, nil
}

func loadMembers(ctx context.Context, tx *sql.Tx, allianceID int64) ([]appalliance.Member, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT m.player_id, p.display_name, m.role, m.joined_at
		FROM alliance_members m JOIN players p ON p.id = m.player_id
		WHERE m.alliance_id = ?
		ORDER BY CASE m.role WHEN 'founder' THEN 0 WHEN 'officer' THEN 1 ELSE 2 END, p.display_name
	`, allianceID)
	if err != nil {
		return nil, fmt.Errorf("alliance repository: read members: %w", err)
	}
	defer rows.Close()
	var members []appalliance.Member
	for rows.Next() {
		var member appalliance.Member
		var role, joinedText string
		if err := rows.Scan(&member.PlayerID, &member.Name, &role, &joinedText); err != nil {
			return nil, fmt.Errorf("alliance repository: scan member: %w", err)
		}
		member.Role = domainalliance.Role(role)
		member.JoinedAt, err = time.Parse(time.RFC3339Nano, joinedText)
		if err != nil {
			return nil, fmt.Errorf("alliance repository: parse join date: %w", err)
		}
		members = append(members, member)
	}
	return members, rows.Err()
}

func pendingInvitations(ctx context.Context, tx *sql.Tx, condition string, argument any, now time.Time) ([]appalliance.Invitation, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT i.id, i.alliance_id, a.name, a.tag, i.player_id, guest.display_name, host.display_name, i.expires_at
		FROM alliance_invitations i
		JOIN alliances a ON a.id = i.alliance_id
		JOIN players guest ON guest.id = i.player_id
		JOIN players host ON host.id = i.invited_by_player_id
		WHERE i.status = 'pending' AND `+condition+`
		ORDER BY i.expires_at, i.id
	`, argument)
	if err != nil {
		return nil, fmt.Errorf("alliance repository: read invitations: %w", err)
	}
	defer rows.Close()
	var invitations []appalliance.Invitation
	for rows.Next() {
		var invitation appalliance.Invitation
		var expiresText string
		if err := rows.Scan(&invitation.ID, &invitation.AllianceID, &invitation.AllianceName, &invitation.AllianceTag,
			&invitation.PlayerID, &invitation.PlayerName, &invitation.InvitedByName, &expiresText); err != nil {
			return nil, fmt.Errorf("alliance repository: scan invitation: %w", err)
		}
		invitation.ExpiresAt, err = time.Parse(time.RFC3339Nano, expiresText)
		if err != nil {
			return nil, fmt.Errorf("alliance repository: parse expiry: %w", err)
		}
		invitation.Expired = !invitation.ExpiresAt.After(now)
		invitations = append(invitations, invitation)
	}
	return invitations, rows.Err()
}

// loadRelations reads what an alliance has declared about others.
func loadRelations(ctx context.Context, tx *sql.Tx, allianceID int64) ([]appalliance.Relation, error) {
	return readRelations(ctx, tx, `
		SELECT r.other_alliance_id, a.name, a.tag, r.relation, r.declared_at
		FROM alliance_relations r JOIN alliances a ON a.id = r.other_alliance_id
		WHERE r.alliance_id = ?
		ORDER BY a.tag
	`, allianceID)
}

// loadReceivedRelations reads what others have declared about this alliance.
// Being told is legitimate knowledge: a declaration is an announcement, and
// answering one is the whole of what diplomacy between alliances rests on.
func loadReceivedRelations(ctx context.Context, tx *sql.Tx, allianceID int64) ([]appalliance.Relation, error) {
	return readRelations(ctx, tx, `
		SELECT r.alliance_id, a.name, a.tag, r.relation, r.declared_at
		FROM alliance_relations r JOIN alliances a ON a.id = r.alliance_id
		WHERE r.other_alliance_id = ?
		ORDER BY a.tag
	`, allianceID)
}

func readRelations(ctx context.Context, tx *sql.Tx, query string, allianceID int64) ([]appalliance.Relation, error) {
	rows, err := tx.QueryContext(ctx, query, allianceID)
	if err != nil {
		return nil, fmt.Errorf("alliance repository: read relations: %w", err)
	}
	defer rows.Close()
	var relations []appalliance.Relation
	for rows.Next() {
		var relation appalliance.Relation
		var kind, declaredText string
		if err := rows.Scan(&relation.OtherAllianceID, &relation.OtherName, &relation.OtherTag, &kind, &declaredText); err != nil {
			return nil, fmt.Errorf("alliance repository: scan relation: %w", err)
		}
		relation.Kind = domainalliance.Relation(kind)
		relation.DeclaredAt, err = time.Parse(time.RFC3339Nano, declaredText)
		if err != nil {
			return nil, fmt.Errorf("alliance repository: parse declaration date: %w", err)
		}
		relations = append(relations, relation)
	}
	return relations, rows.Err()
}

func loadHistory(ctx context.Context, tx *sql.Tx, allianceID int64) ([]appalliance.Entry, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT h.action, COALESCE(actor.display_name, ''), COALESCE(target.display_name, ''), h.occurred_at, h.details
		FROM alliance_history h
		LEFT JOIN players actor ON actor.id = h.actor_player_id
		LEFT JOIN players target ON target.id = h.target_player_id
		WHERE h.alliance_id = ?
		ORDER BY h.id DESC LIMIT 50
	`, allianceID)
	if err != nil {
		return nil, fmt.Errorf("alliance repository: read history: %w", err)
	}
	defer rows.Close()
	var entries []appalliance.Entry
	for rows.Next() {
		var entry appalliance.Entry
		var occurredText string
		if err := rows.Scan(&entry.Action, &entry.ActorName, &entry.TargetName, &occurredText, &entry.Details); err != nil {
			return nil, fmt.Errorf("alliance repository: scan history: %w", err)
		}
		entry.OccurredAt, err = time.Parse(time.RFC3339Nano, occurredText)
		if err != nil {
			return nil, fmt.Errorf("alliance repository: parse history date: %w", err)
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

// alliesOf reports whether two players answer to the same alliance, which is
// what makes one fleet allowed to defend the planet of another.
func alliesOf(ctx context.Context, tx *sql.Tx, first, second int64) (bool, error) {
	var shared bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM alliance_members a
			JOIN alliance_members b ON b.alliance_id = a.alliance_id
			WHERE a.player_id = ? AND b.player_id = ?
		)
	`, first, second).Scan(&shared); err != nil {
		return false, fmt.Errorf("alliance repository: compare memberships: %w", err)
	}
	return shared, nil
}
