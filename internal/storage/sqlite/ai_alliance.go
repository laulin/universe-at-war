package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	appai "universeatwar/internal/app/ai"
	appalliance "universeatwar/internal/app/alliance"
	domainai "universeatwar/internal/domain/ai"
	domainalliance "universeatwar/internal/domain/alliance"
	"universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/universe"
)

// sharedPayload is the persisted body of one shared belief.
type sharedPayload struct {
	Plunder    economy.Resources `json:"plunder"`
	Defence    int64             `json:"defence"`
	Complete   bool              `json:"complete"`
	Summary    string            `json:"summary"`
	Capability *sharedCapability `json:"capability,omitempty"`
}

// sharedCapability is what a member declares it owns. It is a declaration, not
// a reading: nothing here comes from anywhere but its author.
type sharedCapability struct {
	BodyID        int64 `json:"body_id"`
	Awake         bool  `json:"awake"`
	Probes        int64 `json:"probes"`
	Recyclers     int64 `json:"recyclers"`
	WarStrength   int64 `json:"war_strength"`
	GroundDefence int64 `json:"ground_defence"`
	Hauling       int64 `json:"hauling"`
}

// AllianceOf returns the team of a player and who may lead an operation for it.
func (r *AIRepository) AllianceOf(ctx context.Context, playerID int64) (appai.Alliance, error) {
	var alliance appai.Alliance
	err := withWriteTx(ctx, r.write, "ai repository: alliance of", func(tx *sql.Tx) error {
		allianceID, _, err := membership(ctx, tx, playerID)
		if err != nil {
			if errors.Is(err, appalliance.ErrNotAMember) {
				return appai.ErrNotFound
			}
			return err
		}
		if err := tx.QueryRowContext(ctx,
			"SELECT id, name, tag FROM alliances WHERE id = ?", allianceID).
			Scan(&alliance.ID, &alliance.Name, &alliance.Tag); err != nil {
			return fmt.Errorf("ai repository: read alliance: %w", err)
		}
		rows, err := tx.QueryContext(ctx, `
			SELECT m.player_id, p.account_id, p.display_name, m.role,
				EXISTS(SELECT 1 FROM ai_profiles a WHERE a.player_id = m.player_id AND a.state = 'active')
			FROM alliance_members m
			JOIN players p ON p.id = m.player_id
			WHERE m.alliance_id = ?
			ORDER BY m.player_id
		`, allianceID)
		if err != nil {
			return fmt.Errorf("ai repository: read members: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var member appai.Member
			var role string
			if err := rows.Scan(&member.PlayerID, &member.AccountID, &member.Name, &role, &member.Artificial); err != nil {
				return fmt.Errorf("ai repository: scan member: %w", err)
			}
			member.Leads = domainalliance.Role(role).Can(domainalliance.LeadOperation)
			alliance.Members = append(alliance.Members, member)
		}
		return rows.Err()
	})
	if err != nil {
		return appai.Alliance{}, err
	}
	return alliance, nil
}

// Publish adds what a member observed to the common memory of its alliance. A
// belief drawn from a report keeps the reference, so unsharing that report
// takes the belief away with it.
func (r *AIRepository) Publish(ctx context.Context, allianceID, authorID int64, knowledge []domainai.Knowledge) error {
	return withWriteTx(ctx, r.write, "ai repository: publish", func(tx *sql.Tx) error {
		for _, belief := range knowledge {
			if !belief.Kind.Valid() {
				return fmt.Errorf("ai repository: unknown shared belief %q", belief.Kind)
			}
			payload := sharedPayload{
				Plunder: belief.Plunder, Defence: belief.Defence,
				Complete: belief.Complete, Summary: belief.Summary,
			}
			if belief.Kind == domainai.CapabilityKnowledge {
				payload.Capability = &sharedCapability{
					BodyID: belief.Capability.BodyID, Awake: belief.Capability.Awake,
					Probes: belief.Capability.Probes, Recyclers: belief.Capability.Recyclers,
					WarStrength:   belief.Capability.WarStrength,
					GroundDefence: belief.Capability.GroundDefence, Hauling: belief.Capability.Hauling,
				}
			}
			document, err := json.Marshal(payload)
			if err != nil {
				return fmt.Errorf("ai repository: encode belief: %w", err)
			}
			var report any
			if belief.ReportID > 0 {
				report = belief.ReportID
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO ai_alliance_memory(alliance_id, author_player_id, kind, galaxy, system, position,
					source_report_id, observed_at, expires_at, confidence, payload_version, payload)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?)
				ON CONFLICT(alliance_id, kind, author_player_id, galaxy, system, position) DO UPDATE SET
					source_report_id = excluded.source_report_id, observed_at = excluded.observed_at,
					expires_at = excluded.expires_at, confidence = excluded.confidence,
					payload = excluded.payload
			`, allianceID, authorID, string(belief.Kind),
				belief.Coordinate.Galaxy, belief.Coordinate.System, belief.Coordinate.Position,
				report, timestamp(belief.ObservedAt), timestamp(belief.ExpiresAt), belief.Confidence,
				string(document)); err != nil {
				return fmt.Errorf("ai repository: publish belief: %w", err)
			}
		}
		return nil
	})
}

// Recall reads what an alliance still believes. A belief whose report is no
// longer shared with the alliance, or whose author has left it, is simply not
// there: revocation needs no cleanup to be effective.
func (r *AIRepository) Recall(ctx context.Context, allianceID int64, now time.Time) ([]domainai.Knowledge, error) {
	return recallBeliefs(ctx, r.write, allianceID, now)
}

// beliefQuerier reads many rows, from a transaction or straight from the pool.
type beliefQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// recallBeliefs reads what an alliance still believes at that instant.
func recallBeliefs(ctx context.Context, source beliefQuerier, allianceID int64, now time.Time) ([]domainai.Knowledge, error) {
	rows, err := source.QueryContext(ctx, `
		SELECT m.kind, m.galaxy, m.system, m.position, m.author_player_id, p.display_name,
			m.observed_at, m.expires_at, m.confidence, COALESCE(m.source_report_id, 0), m.payload
		FROM ai_alliance_memory m
		JOIN players p ON p.id = m.author_player_id
		JOIN alliance_members a ON a.player_id = m.author_player_id AND a.alliance_id = m.alliance_id
		LEFT JOIN reports r ON r.id = m.source_report_id
		WHERE m.alliance_id = ? AND m.expires_at > ?
		  AND (m.source_report_id IS NULL OR r.shared_alliance_id = m.alliance_id)
		ORDER BY m.kind, m.galaxy, m.system, m.position, m.author_player_id
	`, allianceID, timestamp(now))
	if err != nil {
		return nil, fmt.Errorf("ai repository: recall: %w", err)
	}
	defer rows.Close()
	var beliefs []domainai.Knowledge
	for rows.Next() {
		var belief domainai.Knowledge
		var kind, observedText, expiresText, document string
		var at universe.Coordinate
		if err := rows.Scan(&kind, &at.Galaxy, &at.System, &at.Position, &belief.AuthorID, &belief.AuthorName,
			&observedText, &expiresText, &belief.Confidence, &belief.ReportID, &document); err != nil {
			return nil, fmt.Errorf("ai repository: scan belief: %w", err)
		}
		belief.Kind = domainai.KnowledgeKind(kind)
		belief.Coordinate = at
		var err error
		if belief.ObservedAt, err = time.Parse(time.RFC3339Nano, observedText); err != nil {
			return nil, fmt.Errorf("ai repository: parse observation: %w", err)
		}
		if belief.ExpiresAt, err = time.Parse(time.RFC3339Nano, expiresText); err != nil {
			return nil, fmt.Errorf("ai repository: parse expiry: %w", err)
		}
		var payload sharedPayload
		if err := json.Unmarshal([]byte(document), &payload); err != nil {
			return nil, fmt.Errorf("ai repository: decode belief: %w", err)
		}
		belief.Plunder, belief.Defence = payload.Plunder, payload.Defence
		belief.Complete, belief.Summary = payload.Complete, payload.Summary
		if payload.Capability != nil {
			belief.Capability = domainai.Capability{
				PlayerID: belief.AuthorID, PlayerName: belief.AuthorName, Coordinate: belief.Coordinate,
				BodyID: payload.Capability.BodyID, Awake: payload.Capability.Awake,
				Probes: payload.Capability.Probes, Recyclers: payload.Capability.Recyclers,
				WarStrength:   payload.Capability.WarStrength,
				GroundDefence: payload.Capability.GroundDefence, Hauling: payload.Capability.Hauling,
			}
		}
		beliefs = append(beliefs, belief)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ai repository: iterate beliefs: %w", err)
	}
	return beliefs, nil
}

// Enlistment tells the administration who an artificial player is and, when the
// alliance already exists, which of its members could invite it.
func (r *AIRepository) Enlistment(ctx context.Context, playerID int64, tag string) (appai.Enlistment, error) {
	var enlistment appai.Enlistment
	err := withWriteTx(ctx, r.write, "ai repository: enlistment", func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx,
			"SELECT account_id, display_name FROM players WHERE id = ?", playerID).
			Scan(&enlistment.AccountID, &enlistment.Name)
		if errors.Is(err, sql.ErrNoRows) {
			return appai.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("ai repository: read player: %w", err)
		}
		normalized, err := domainalliance.NormalizeTag(tag)
		if err != nil {
			return err
		}
		var allianceID int64
		err = tx.QueryRowContext(ctx, "SELECT id FROM alliances WHERE tag = ?", normalized).Scan(&allianceID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("ai repository: read alliance: %w", err)
		}
		enlistment.Exists = true
		// The oldest member allowed to invite carries the invitation.
		rows, err := tx.QueryContext(ctx, `
			SELECT p.account_id, m.role FROM alliance_members m
			JOIN players p ON p.id = m.player_id
			WHERE m.alliance_id = ? ORDER BY m.player_id
		`, allianceID)
		if err != nil {
			return fmt.Errorf("ai repository: read members: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var accountID int64
			var role string
			if err := rows.Scan(&accountID, &role); err != nil {
				return fmt.Errorf("ai repository: scan member: %w", err)
			}
			if enlistment.LeaderAccountID == 0 && domainalliance.Role(role).Can(domainalliance.Invite) {
				enlistment.LeaderAccountID = accountID
			}
		}
		return rows.Err()
	})
	if err != nil {
		return appai.Enlistment{}, err
	}
	return enlistment, nil
}
