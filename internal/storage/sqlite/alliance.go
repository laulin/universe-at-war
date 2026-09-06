package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	appalliance "universeatwar/internal/app/alliance"
	domainalliance "universeatwar/internal/domain/alliance"
)

// AllianceRepository keeps every membership change in one write transaction and
// writes the history of the team as it goes.
type AllianceRepository struct {
	write *sql.DB
}

func NewAllianceRepository(write *sql.DB) *AllianceRepository {
	return &AllianceRepository{write: write}
}

// Profile returns the alliance of an account with everything a member may read.
func (r *AllianceRepository) Profile(ctx context.Context, accountID int64, now time.Time) (appalliance.Profile, error) {
	var profile appalliance.Profile
	err := withWriteTx(ctx, r.write, "alliance repository: profile", func(tx *sql.Tx) error {
		playerID, err := playerByAccount(ctx, tx, accountID)
		if err != nil {
			return err
		}
		allianceID, role, err := membership(ctx, tx, playerID)
		if err != nil {
			return err
		}
		configured, _, err := activeRuleset(ctx, tx)
		if err != nil {
			return err
		}
		profile, err = loadProfile(ctx, tx, allianceID, now)
		if err != nil {
			return err
		}
		profile.Role = role
		profile.MaximumSize = configured.Team.MaximumAllianceSize
		profile.Diplomacy = configured.Team.DiplomacyEnabled
		return nil
	})
	if err != nil {
		return appalliance.Profile{}, err
	}
	return profile, nil
}

// InvitationsFor lists the offers a player has received.
func (r *AllianceRepository) InvitationsFor(ctx context.Context, accountID int64, now time.Time) ([]appalliance.Invitation, error) {
	var invitations []appalliance.Invitation
	err := withWriteTx(ctx, r.write, "alliance repository: invitations", func(tx *sql.Tx) error {
		playerID, err := playerByAccount(ctx, tx, accountID)
		if err != nil {
			return err
		}
		invitations, err = pendingInvitations(ctx, tx, "i.player_id = ?", playerID, now)
		return err
	})
	if err != nil {
		return nil, err
	}
	return invitations, nil
}

// Create founds an alliance and makes its author the founder.
func (r *AllianceRepository) Create(ctx context.Context, accountID int64, name, tag, description string, now time.Time) (appalliance.Profile, error) {
	var profile appalliance.Profile
	err := withWriteTx(ctx, r.write, "alliance repository: create", func(tx *sql.Tx) error {
		configured, _, err := activeRuleset(ctx, tx)
		if err != nil {
			return err
		}
		if !configured.Team.AlliancesEnabled {
			return appalliance.ErrDisabled
		}
		playerID, err := playerByAccount(ctx, tx, accountID)
		if err != nil {
			return err
		}
		if _, _, err := membership(ctx, tx, playerID); err == nil {
			return appalliance.ErrAlreadyAMember
		} else if !errors.Is(err, appalliance.ErrNotAMember) {
			return err
		}
		result, err := tx.ExecContext(ctx, `
			INSERT INTO alliances(name, name_normalized, tag, description, founder_player_id, created_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, name, strings.ToLower(name), tag, description, playerID, timestamp(now))
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return appalliance.ErrNameTaken
			}
			return fmt.Errorf("alliance repository: create alliance: %w", err)
		}
		allianceID, err := result.LastInsertId()
		if err != nil {
			return fmt.Errorf("alliance repository: alliance id: %w", err)
		}
		if err := addMember(ctx, tx, allianceID, playerID, domainalliance.Founder, now); err != nil {
			return err
		}
		if err := recordHistory(ctx, tx, allianceID, playerID, "alliance_founded", 0, now, "json_object('tag', '"+tag+"')"); err != nil {
			return err
		}
		profile, err = loadProfile(ctx, tx, allianceID, now)
		if err != nil {
			return err
		}
		profile.Role = domainalliance.Founder
		profile.MaximumSize = configured.Team.MaximumAllianceSize
		profile.Diplomacy = configured.Team.DiplomacyEnabled
		return nil
	})
	if err != nil {
		return appalliance.Profile{}, err
	}
	return profile, nil
}

// Invite offers a seat to a player who belongs to no alliance.
func (r *AllianceRepository) Invite(ctx context.Context, accountID int64, playerName string, now time.Time) error {
	return withWriteTx(ctx, r.write, "alliance repository: invite", func(tx *sql.Tx) error {
		allianceID, role, playerID, configured, err := actingMember(ctx, tx, accountID)
		if err != nil {
			return err
		}
		if !role.Can(domainalliance.Invite) {
			return appalliance.ErrForbidden
		}
		var guestID int64
		err = tx.QueryRowContext(ctx, "SELECT id FROM players WHERE display_name = ?", playerName).Scan(&guestID)
		if errors.Is(err, sql.ErrNoRows) {
			return appalliance.ErrNoSuchPlayer
		}
		if err != nil {
			return fmt.Errorf("alliance repository: read guest: %w", err)
		}
		if _, _, err := membership(ctx, tx, guestID); err == nil {
			return appalliance.ErrAlreadyAMember
		} else if !errors.Is(err, appalliance.ErrNotAMember) {
			return err
		}
		if err := allianceHasRoom(ctx, tx, allianceID, configured.Team.MaximumAllianceSize); err != nil {
			return err
		}
		expiresAt := now.Add(time.Duration(configured.Team.InvitationLifetimeHours) * time.Hour)
		_, err = tx.ExecContext(ctx, `
			INSERT INTO alliance_invitations(alliance_id, player_id, invited_by_player_id, expires_at, created_at)
			VALUES (?, ?, ?, ?, ?)
		`, allianceID, guestID, playerID, timestamp(expiresAt), timestamp(now))
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				// An offer is already standing: inviting again is a no-op.
				return nil
			}
			return fmt.Errorf("alliance repository: invite: %w", err)
		}
		return recordHistory(ctx, tx, allianceID, playerID, "invitation_sent", guestID, now, "json_object()")
	})
}

// Accept joins the alliance of an invitation, once and only once.
func (r *AllianceRepository) Accept(ctx context.Context, accountID, invitationID int64, now time.Time) error {
	return withWriteTx(ctx, r.write, "alliance repository: accept", func(tx *sql.Tx) error {
		playerID, err := playerByAccount(ctx, tx, accountID)
		if err != nil {
			return err
		}
		allianceID, expiresAt, err := ownInvitation(ctx, tx, invitationID, playerID)
		if err != nil {
			return err
		}
		if allianceID == 0 {
			// Already answered: replaying the answer changes nothing.
			return nil
		}
		if !expiresAt.After(now) {
			return appalliance.ErrInvitationEnded
		}
		if _, _, err := membership(ctx, tx, playerID); err == nil {
			return appalliance.ErrAlreadyAMember
		} else if !errors.Is(err, appalliance.ErrNotAMember) {
			return err
		}
		configured, _, err := activeRuleset(ctx, tx)
		if err != nil {
			return err
		}
		if err := allianceHasRoom(ctx, tx, allianceID, configured.Team.MaximumAllianceSize); err != nil {
			return err
		}
		if err := addMember(ctx, tx, allianceID, playerID, domainalliance.Member, now); err != nil {
			return err
		}
		if err := resolveInvitation(ctx, tx, invitationID, "accepted", now); err != nil {
			return err
		}
		// Joining one team withdraws the player from every other waiting list.
		if _, err := tx.ExecContext(ctx, `
			UPDATE alliance_invitations SET status = 'revoked', resolved_at = ?
			WHERE player_id = ? AND status = 'pending'
		`, timestamp(now), playerID); err != nil {
			return fmt.Errorf("alliance repository: revoke other invitations: %w", err)
		}
		return recordHistory(ctx, tx, allianceID, playerID, "member_joined", playerID, now, "json_object()")
	})
}

// Decline refuses an invitation, once and only once.
func (r *AllianceRepository) Decline(ctx context.Context, accountID, invitationID int64, now time.Time) error {
	return withWriteTx(ctx, r.write, "alliance repository: decline", func(tx *sql.Tx) error {
		playerID, err := playerByAccount(ctx, tx, accountID)
		if err != nil {
			return err
		}
		allianceID, _, err := ownInvitation(ctx, tx, invitationID, playerID)
		if err != nil {
			return err
		}
		if allianceID == 0 {
			return nil
		}
		if err := resolveInvitation(ctx, tx, invitationID, "declined", now); err != nil {
			return err
		}
		return recordHistory(ctx, tx, allianceID, playerID, "invitation_declined", playerID, now, "json_object()")
	})
}

// Leave gives up membership, dissolving the alliance with the last member.
func (r *AllianceRepository) Leave(ctx context.Context, accountID int64, now time.Time) error {
	return withWriteTx(ctx, r.write, "alliance repository: leave", func(tx *sql.Tx) error {
		allianceID, role, playerID, _, err := actingMember(ctx, tx, accountID)
		if err != nil {
			return err
		}
		members, err := memberCount(ctx, tx, allianceID)
		if err != nil {
			return err
		}
		if role == domainalliance.Founder && members > 1 {
			return appalliance.ErrLastFounder
		}
		if err := removeMember(ctx, tx, playerID); err != nil {
			return err
		}
		if members == 1 {
			if _, err := tx.ExecContext(ctx, "DELETE FROM alliances WHERE id = ?", allianceID); err != nil {
				return fmt.Errorf("alliance repository: dissolve alliance: %w", err)
			}
			return nil
		}
		return recordHistory(ctx, tx, allianceID, playerID, "member_left", playerID, now, "json_object()")
	})
}

// Expel removes a member of a lower rank.
func (r *AllianceRepository) Expel(ctx context.Context, accountID, targetID int64, now time.Time) error {
	return withWriteTx(ctx, r.write, "alliance repository: expel", func(tx *sql.Tx) error {
		allianceID, role, playerID, _, err := actingMember(ctx, tx, accountID)
		if err != nil {
			return err
		}
		targetAlliance, targetRole, err := membership(ctx, tx, targetID)
		if err != nil {
			return err
		}
		if targetAlliance != allianceID || targetID == playerID {
			return appalliance.ErrForbidden
		}
		if !role.CanExpel(targetRole) {
			return appalliance.ErrForbidden
		}
		if err := removeMember(ctx, tx, targetID); err != nil {
			return err
		}
		return recordHistory(ctx, tx, allianceID, playerID, "member_expelled", targetID, now, "json_object()")
	})
}

// Promote changes the rank of a member. Handing over the founder charge demotes
// the previous founder, so an alliance always has exactly one.
func (r *AllianceRepository) Promote(ctx context.Context, accountID, targetID int64, role domainalliance.Role, now time.Time) error {
	return withWriteTx(ctx, r.write, "alliance repository: promote", func(tx *sql.Tx) error {
		allianceID, actorRole, playerID, _, err := actingMember(ctx, tx, accountID)
		if err != nil {
			return err
		}
		if actorRole != domainalliance.Founder {
			return appalliance.ErrForbidden
		}
		targetAlliance, _, err := membership(ctx, tx, targetID)
		if err != nil {
			return err
		}
		if targetAlliance != allianceID || targetID == playerID {
			return appalliance.ErrForbidden
		}
		if _, err := tx.ExecContext(ctx, "UPDATE alliance_members SET role = ? WHERE player_id = ?",
			string(role), targetID); err != nil {
			return fmt.Errorf("alliance repository: change role: %w", err)
		}
		if role == domainalliance.Founder {
			if _, err := tx.ExecContext(ctx, "UPDATE alliance_members SET role = 'officer' WHERE player_id = ?",
				playerID); err != nil {
				return fmt.Errorf("alliance repository: hand over charge: %w", err)
			}
			if _, err := tx.ExecContext(ctx, "UPDATE alliances SET founder_player_id = ? WHERE id = ?",
				targetID, allianceID); err != nil {
				return fmt.Errorf("alliance repository: record new founder: %w", err)
			}
		}
		return recordHistory(ctx, tx, allianceID, playerID, "role_changed", targetID, now,
			"json_object('role', '"+string(role)+"')")
	})
}

// Declare states an intention towards another alliance.
func (r *AllianceRepository) Declare(ctx context.Context, accountID int64, tag string, relation domainalliance.Relation, now time.Time) error {
	return withWriteTx(ctx, r.write, "alliance repository: declare", func(tx *sql.Tx) error {
		allianceID, role, playerID, configured, err := actingMember(ctx, tx, accountID)
		if err != nil {
			return err
		}
		if !configured.Team.DiplomacyEnabled {
			return appalliance.ErrDisabled
		}
		if !role.Can(domainalliance.Diplomacy) {
			return appalliance.ErrForbidden
		}
		var otherID int64
		err = tx.QueryRowContext(ctx, "SELECT id FROM alliances WHERE tag = ?", tag).Scan(&otherID)
		if errors.Is(err, sql.ErrNoRows) {
			return appalliance.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("alliance repository: read other alliance: %w", err)
		}
		if otherID == allianceID {
			return appalliance.ErrForbidden
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO alliance_relations(alliance_id, other_alliance_id, relation, declared_by_player_id, declared_at)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(alliance_id, other_alliance_id) DO UPDATE SET
				relation = excluded.relation, declared_by_player_id = excluded.declared_by_player_id,
				declared_at = excluded.declared_at
		`, allianceID, otherID, string(relation), playerID, timestamp(now)); err != nil {
			return fmt.Errorf("alliance repository: declare relation: %w", err)
		}
		return recordHistory(ctx, tx, allianceID, playerID, "relation_declared", 0, now,
			"json_object('tag', '"+tag+"', 'relation', '"+string(relation)+"')")
	})
}

// Describe rewrites the public description of the alliance.
func (r *AllianceRepository) Describe(ctx context.Context, accountID int64, description string, now time.Time) error {
	return withWriteTx(ctx, r.write, "alliance repository: describe", func(tx *sql.Tx) error {
		allianceID, role, playerID, _, err := actingMember(ctx, tx, accountID)
		if err != nil {
			return err
		}
		if !role.Can(domainalliance.EditProfile) {
			return appalliance.ErrForbidden
		}
		if _, err := tx.ExecContext(ctx, "UPDATE alliances SET description = ? WHERE id = ?",
			description, allianceID); err != nil {
			return fmt.Errorf("alliance repository: describe: %w", err)
		}
		return recordHistory(ctx, tx, allianceID, playerID, "profile_changed", 0, now, "json_object()")
	})
}
