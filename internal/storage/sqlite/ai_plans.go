package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	appai "universeatwar/internal/app/ai"
	domainai "universeatwar/internal/domain/ai"
	"universeatwar/internal/domain/universe"
)

// AssignRoles writes down who does what for the alliance. A role is only ever
// given to a member of that alliance.
func (r *AIRepository) AssignRoles(ctx context.Context, allianceID int64,
	roles map[int64]domainai.Role, now time.Time) error {
	return withWriteTx(ctx, r.write, "ai repository: assign roles", func(tx *sql.Tx) error {
		for _, playerID := range sortedPlayers(roles) {
			role := roles[playerID]
			if !role.Valid() {
				return fmt.Errorf("ai repository: unknown alliance role %q", role)
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO ai_alliance_roles(alliance_id, player_id, role, assigned_at)
				SELECT ?, ?, ?, ?
				WHERE EXISTS(SELECT 1 FROM alliance_members WHERE alliance_id = ? AND player_id = ?)
				ON CONFLICT(alliance_id, player_id) DO UPDATE SET
					role = excluded.role, assigned_at = excluded.assigned_at
			`, allianceID, playerID, string(role), timestamp(now), allianceID, playerID); err != nil {
				return fmt.Errorf("ai repository: assign role: %w", err)
			}
		}
		// A member that has left keeps no role behind.
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM ai_alliance_roles WHERE alliance_id = ? AND player_id NOT IN
				(SELECT player_id FROM alliance_members WHERE alliance_id = ?)
		`, allianceID, allianceID); err != nil {
			return fmt.Errorf("ai repository: clear roles: %w", err)
		}
		return nil
	})
}

// Roles reads who does what for the alliance.
func (r *AIRepository) Roles(ctx context.Context, allianceID int64) (map[int64]domainai.Role, error) {
	rows, err := r.write.QueryContext(ctx,
		"SELECT player_id, role FROM ai_alliance_roles WHERE alliance_id = ? ORDER BY player_id", allianceID)
	if err != nil {
		return nil, fmt.Errorf("ai repository: read roles: %w", err)
	}
	defer rows.Close()
	roles := map[int64]domainai.Role{}
	for rows.Next() {
		var playerID int64
		var role string
		if err := rows.Scan(&playerID, &role); err != nil {
			return nil, fmt.Errorf("ai repository: scan role: %w", err)
		}
		roles[playerID] = domainai.Role(role)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ai repository: iterate roles: %w", err)
	}
	return roles, nil
}

// OpenObjective starts the single plan an alliance pursues. A second plan is
// refused by the schema itself.
func (r *AIRepository) OpenObjective(ctx context.Context, allianceID int64,
	objective domainai.Objective) (domainai.Objective, error) {
	var opened domainai.Objective
	err := withWriteTx(ctx, r.write, "ai repository: open objective", func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `
			INSERT INTO ai_alliance_objectives(alliance_id, kind, galaxy, system, position, quorum,
				opened_at, deadline_at, reason)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, allianceID, string(objective.Kind), objective.Coordinate.Galaxy, objective.Coordinate.System,
			objective.Coordinate.Position, objective.Quorum, timestamp(objective.OpenedAt),
			timestamp(objective.DeadlineAt), objective.Reason)
		if err != nil {
			return fmt.Errorf("ai repository: open objective: %w", err)
		}
		objectiveID, err := result.LastInsertId()
		if err != nil {
			return fmt.Errorf("ai repository: objective id: %w", err)
		}
		opened, _, err = objectiveRow(ctx, tx, objectiveID)
		return err
	})
	if err != nil {
		return domainai.Objective{}, err
	}
	return opened, nil
}

// Objective returns the plan an alliance is pursuing, if it has one.
func (r *AIRepository) Objective(ctx context.Context, allianceID int64) (domainai.Objective, bool, error) {
	var objectiveID int64
	err := r.write.QueryRowContext(ctx, `
		SELECT id FROM ai_alliance_objectives
		WHERE alliance_id = ? AND state IN ('scouting', 'assembling')
	`, allianceID).Scan(&objectiveID)
	if errors.Is(err, sql.ErrNoRows) {
		return domainai.Objective{}, false, nil
	}
	if err != nil {
		return domainai.Objective{}, false, fmt.Errorf("ai repository: read objective: %w", err)
	}
	var objective domainai.Objective
	err = withWriteTx(ctx, r.write, "ai repository: objective", func(tx *sql.Tx) error {
		var found bool
		var err error
		objective, found, err = objectiveRow(ctx, tx, objectiveID)
		if err != nil || !found {
			return err
		}
		return nil
	})
	if err != nil {
		return domainai.Objective{}, false, err
	}
	return objective, objective.ID != 0, nil
}

// AdvanceObjective moves a plan along, or gives it up with a reason. The move
// is guarded by the state it came from, so two leaders cannot both advance it.
func (r *AIRepository) AdvanceObjective(ctx context.Context, objective domainai.Objective,
	to domainai.ObjectiveState, groupID int64, reason string, now time.Time) error {
	return withWriteTx(ctx, r.write, "ai repository: advance objective", func(tx *sql.Tx) error {
		var closed any
		if !to.Open() {
			closed = timestamp(now)
		}
		var group any
		if groupID > 0 {
			group = groupID
		} else if objective.GroupID > 0 {
			group = objective.GroupID
		}
		result, err := tx.ExecContext(ctx, `
			UPDATE ai_alliance_objectives
			SET state = ?, group_id = ?, closed_at = ?, reason = ?, version = version + 1
			WHERE id = ? AND state = ?
		`, string(to), group, closed, reason, objective.ID, string(objective.State))
		if err != nil {
			return fmt.Errorf("ai repository: advance objective: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("ai repository: advance objective: %w", err)
		}
		if affected != 1 {
			return appai.ErrInvalidRequest
		}
		return nil
	})
}

func objectiveRow(ctx context.Context, tx rowQuerier, objectiveID int64) (domainai.Objective, bool, error) {
	var objective domainai.Objective
	var kind, state, openedText, deadlineText string
	var group sql.NullInt64
	var at universe.Coordinate
	err := tx.QueryRowContext(ctx, `
		SELECT id, kind, galaxy, system, position, state, group_id, quorum, opened_at, deadline_at, reason
		FROM ai_alliance_objectives WHERE id = ?
	`, objectiveID).Scan(&objective.ID, &kind, &at.Galaxy, &at.System, &at.Position, &state, &group,
		&objective.Quorum, &openedText, &deadlineText, &objective.Reason)
	if errors.Is(err, sql.ErrNoRows) {
		return domainai.Objective{}, false, nil
	}
	if err != nil {
		return domainai.Objective{}, false, fmt.Errorf("ai repository: read objective: %w", err)
	}
	objective.Kind = domainai.ObjectiveKind(kind)
	objective.State = domainai.ObjectiveState(state)
	objective.Coordinate = at
	objective.GroupID = group.Int64
	if objective.OpenedAt, err = time.Parse(time.RFC3339Nano, openedText); err != nil {
		return domainai.Objective{}, false, fmt.Errorf("ai repository: parse opening: %w", err)
	}
	if objective.DeadlineAt, err = time.Parse(time.RFC3339Nano, deadlineText); err != nil {
		return domainai.Objective{}, false, fmt.Errorf("ai repository: parse deadline: %w", err)
	}
	return objective, true, nil
}

func sortedPlayers(roles map[int64]domainai.Role) []int64 {
	identifiers := make([]int64, 0, len(roles))
	for playerID := range roles {
		identifiers = append(identifiers, playerID)
	}
	for first := 1; first < len(identifiers); first++ {
		for second := first; second > 0 && identifiers[second] < identifiers[second-1]; second-- {
			identifiers[second], identifiers[second-1] = identifiers[second-1], identifiers[second]
		}
	}
	return identifiers
}
