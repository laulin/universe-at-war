package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"universeatwar/internal/domain/combat"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/rules"
	"universeatwar/internal/domain/universe"
)

// holdingEventPriority orders the end of a stationing against the events of the
// same instant, documented with the scheduled events.
const holdingEventPriority = 80

// defensibleBody reports whether a player may station a fleet on a body: their
// own, or the body of somebody in the same alliance.
func defensibleBody(ctx context.Context, tx *sql.Tx, configured rules.Ruleset, playerID, targetOwner int64) error {
	if playerID == targetOwner {
		return nil
	}
	if !configured.Team.AlliancesEnabled || !configured.Team.GroupDefenseEnabled {
		return domainfleet.ErrInvalidTarget
	}
	allied, err := alliesOf(ctx, tx, playerID, targetOwner)
	if err != nil {
		return err
	}
	if !allied {
		return domainfleet.ErrInvalidTarget
	}
	return nil
}

// beginHold parks a fleet on the body it defends until its holding time ends.
func (r *FleetRepository) beginHold(ctx context.Context, tx *sql.Tx, row fleetRow, targetPlanetID int64, now time.Time) error {
	if row.holdsUntil == nil {
		// A stationing without an end time cannot wait: it turns around.
		return r.abortMission(ctx, tx, row, "no_holding_time", now)
	}
	if err := transitionFleet(ctx, tx, row, domainfleet.Holding, "holding", now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO scheduled_events(event_type, due_at, priority, entity_type, entity_id, ruleset_version, payload, idempotency_key, created_at)
		VALUES ('holding_ended', ?, ?, 'fleet', ?, ?, json_object(), ?, ?)
	`, timestamp(*row.holdsUntil), holdingEventPriority, strconv.FormatInt(row.id, 10), row.rulesetVersion,
		fmt.Sprintf("fleet-hold:%d", row.id), timestamp(now)); err != nil {
		return fmt.Errorf("fleet repository: schedule holding end: %w", err)
	}
	return logFleetEvent(ctx, tx, "fleet_holding", row.id, targetPlanetID, now,
		fmt.Sprintf("json_object('mission', '%s', 'holds_until', '%s')", row.mission, timestamp(*row.holdsUntil)))
}

// resolveHoldingEnd sends a stationed fleet home once its watch is over.
func (r *FleetRepository) resolveHoldingEnd(ctx context.Context, tx *sql.Tx, event ScheduledEvent, now time.Time) error {
	fleetID, err := strconv.ParseInt(event.EntityID, 10, 64)
	if err != nil {
		return fmt.Errorf("fleet repository: invalid fleet event reference")
	}
	row, err := loadFleetRow(ctx, tx, fleetID)
	if err != nil {
		return err
	}
	if row.state != domainfleet.Holding {
		// The fleet was destroyed defending, or already left.
		return nil
	}
	if row.mission == domainfleet.MissionExpedition {
		// The wait is what the trip was for: this is where the draw happens.
		return r.resolveExpedition(ctx, tx, row, event.DueAt, now)
	}
	returnsAt := event.DueAt.Add(event.DueAt.Sub(row.arrivesAt))
	if row.returnsAt != nil {
		returnsAt = *row.returnsAt
	}
	if err := transitionFleet(ctx, tx, row, domainfleet.Returning, "holding_ended", now); err != nil {
		return err
	}
	if err := scheduleReturn(ctx, tx, row.id, row.rulesetVersion, returnsAt, now); err != nil {
		return err
	}
	return logFleetEvent(ctx, tx, "holding_ended", row.id, row.originPlanetID, now, "json_object()")
}

// defendingFleet is one allied fleet stationed on the body under attack.
type defendingFleet struct {
	row   fleetRow
	party combat.Party
}

// stationedDefenders lists the allied fleets that fight for a body, in a stable
// order so the same battle always resolves the same way.
func (r *FleetRepository) stationedDefenders(ctx context.Context, tx *sql.Tx, at universe.Coordinate) ([]defendingFleet, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id FROM fleets
		WHERE target_galaxy = ? AND target_system = ? AND target_position = ?
			AND state = 'holding' AND mission = 'hold'
		ORDER BY id
	`, at.Galaxy, at.System, at.Position)
	if err != nil {
		return nil, fmt.Errorf("fleet repository: list defenders: %w", err)
	}
	defer rows.Close()
	var identifiers []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("fleet repository: scan defender: %w", err)
		}
		identifiers = append(identifiers, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("fleet repository: iterate defenders: %w", err)
	}
	defenders := make([]defendingFleet, 0, len(identifiers))
	for _, id := range identifiers {
		row, err := loadFleetRow(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		composition, err := loadComposition(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		if composition.Count() == 0 {
			continue
		}
		levels, err := researchOfPlayer(ctx, tx, row.ownerPlayerID)
		if err != nil {
			return nil, err
		}
		defenders = append(defenders, defendingFleet{
			row: row,
			party: combat.Party{
				PlayerID: row.ownerPlayerID, Units: composition, Technologies: factorsOf(levels),
			},
		})
	}
	return defenders, nil
}

// applyDefenderLosses writes back what is left of each stationed fleet. A fleet
// wiped out stops waiting: it never comes home.
func (r *FleetRepository) applyDefenderLosses(ctx context.Context, tx *sql.Tx, defenders []defendingFleet,
	results []combat.PartyResult, now time.Time) error {
	for index, defender := range defenders {
		result := results[index]
		composition := domainfleet.Composition{}
		for id, quantity := range defender.party.Units {
			composition[id] = quantity
		}
		if err := applyFleetLosses(ctx, tx, defender.row.id, composition, result.Survivors); err != nil {
			return err
		}
		if totalShips(result.Survivors) > 0 {
			continue
		}
		if err := transitionFleet(ctx, tx, defender.row, domainfleet.Destroyed, "lost_defending", now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			"UPDATE scheduled_events SET state = 'cancelled', processed_at = ? WHERE idempotency_key = ? AND state = 'pending'",
			timestamp(now), fmt.Sprintf("fleet-hold:%d", defender.row.id)); err != nil {
			return fmt.Errorf("fleet repository: cancel holding end: %w", err)
		}
	}
	return nil
}
