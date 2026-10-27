package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	appeconomy "universeatwar/internal/app/economy"
	appmissile "universeatwar/internal/app/missile"
	"universeatwar/internal/domain/building"
	"universeatwar/internal/domain/catalogue"
	"universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/missile"
	"universeatwar/internal/domain/random"
	"universeatwar/internal/domain/report"
	"universeatwar/internal/domain/research"
	"universeatwar/internal/domain/unit"
)

type MissileRepository struct {
	write      *sql.DB
	catalogues catalogue.Set
}

func NewMissileRepository(write *sql.DB, catalogues catalogue.Set) *MissileRepository {
	return &MissileRepository{write: write, catalogues: catalogues}
}

func (r *MissileRepository) RegisterHandlers(processor *EventProcessor) {
	processor.Register("missile_impact", r.impact)
}

func (r *MissileRepository) Overview(ctx context.Context, accountID, planetID int64, now time.Time) (appmissile.Overview, error) {
	var overview appmissile.Overview
	err := withWriteTx(ctx, r.write, "missile overview", func(tx *sql.Tx) error {
		planet, _, production, err := loadPlanet(ctx, tx, accountID, planetID, now, r.catalogues.Buildings)
		if err != nil {
			return err
		}
		if err := persistProduction(ctx, tx, planet.ID, production); err != nil {
			return err
		}
		overview.Planet = planet
		rows, err := tx.QueryContext(ctx, `SELECT id, origin_galaxy, origin_system, origin_position,
			target_galaxy, target_system, target_position, target_kind, quantity, primary_target, arrives_at, state,
			target_body_id = ? FROM missile_strikes WHERE state = 'outbound' AND (
                (origin_body_id = ? AND owner_player_id = (SELECT id FROM players WHERE account_id = ?))
                OR (target_body_id = ? AND target_created_at = (SELECT created_at FROM planets WHERE id = ?)))
			ORDER BY julianday(arrives_at), id`, planetID, planetID, accountID, planetID, planetID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var strike appmissile.Strike
			var arrival string
			if err := rows.Scan(&strike.ID, &strike.Origin.Galaxy, &strike.Origin.System, &strike.Origin.Position,
				&strike.Target.Galaxy, &strike.Target.System, &strike.Target.Position, &strike.TargetKind,
				&strike.Count, &strike.Priority, &arrival, &strike.State, &strike.Incoming); err != nil {
				return err
			}
			strike.ArrivesAt, err = time.Parse(time.RFC3339Nano, arrival)
			if err != nil {
				return err
			}
			// An incoming attack does not disclose the attacker's priority selection.
			if strike.Incoming {
				strike.Priority = ""
			}
			overview.Strikes = append(overview.Strikes, strike)
		}
		return rows.Err()
	})
	return overview, err
}

func (r *MissileRepository) Preview(ctx context.Context, accountID, planetID int64, request appmissile.Request, now time.Time) (appmissile.Plan, error) {
	var plan appmissile.Plan
	err := withWriteTx(ctx, r.write, "missile preview", func(tx *sql.Tx) error {
		planned, _, _, _, err := r.plan(ctx, tx, accountID, planetID, request, now)
		plan = planned
		return err
	})
	return plan, err
}

func (r *MissileRepository) plan(ctx context.Context, tx *sql.Tx, accountID, planetID int64, request appmissile.Request, now time.Time) (appmissile.Plan, appeconomy.Planet, int64, int64, error) {
	var plan appmissile.Plan
	var targetID int64
	planet, rulesVersion, production, err := loadPlanet(ctx, tx, accountID, planetID, now, r.catalogues.Buildings)
	if err != nil {
		return plan, planet, 0, 0, err
	}
	validate := func() error {
		if request.Count <= 0 || !missile.ValidPriority(request.Priority) {
			return appmissile.ErrInvalid
		}
		if !planet.Rules.Combat.MissilesEnabled {
			return appmissile.ErrDisabled
		}
		if planet.Kind != building.OnPlanet || planet.Levels[building.MissileSilo] < 4 {
			return appmissile.ErrOrigin
		}
		if planet.Units[unit.InterplanetaryMissile] < request.Count {
			return appmissile.ErrStock
		}
		if request.TargetKind != fleet.TargetPlanet && request.TargetKind != fleet.TargetMoon {
			return appmissile.ErrTarget
		}
		duration, err := missile.Flight(planet.Coordinate, request.Target, planet.Researches[research.ImpulseDrive], planet.Rules.Topology, planet.Rules.Time.HostileFleetSpeed)
		if err != nil {
			return err
		}
		plan.Duration, plan.ArrivesAt = duration, now.Add(duration)
		err = tx.QueryRowContext(ctx, `SELECT p.id, p.name FROM planets p JOIN players pl ON pl.id = p.owner_player_id
			WHERE p.galaxy = ? AND p.system = ? AND p.position = ? AND p.kind = ? AND pl.account_id <> ?`,
			request.Target.Galaxy, request.Target.System, request.Target.Position, string(request.TargetKind), accountID).Scan(&targetID, &plan.TargetName)
		if errors.Is(err, sql.ErrNoRows) {
			return appmissile.ErrTarget
		}
		if err != nil {
			return err
		}
		return persistProduction(ctx, tx, planet.ID, production)
	}
	err = validate()
	return plan, planet, rulesVersion, targetID, err
}

func (r *MissileRepository) Launch(ctx context.Context, accountID, planetID int64, request appmissile.Request, seed int64, key string, now time.Time) (appmissile.Strike, error) {
	var strike appmissile.Strike
	err := withWriteTx(ctx, r.write, "missile launch", func(tx *sql.Tx) error {
		digest := sha256.Sum256(fmt.Appendf(nil, "%d:%s:%s:%d:%s", planetID, request.Target, request.TargetKind, request.Count, request.Priority))
		hash := hex.EncodeToString(digest[:])
		var previousHash, resultID string
		err := tx.QueryRowContext(ctx, `SELECT request_hash, result_id FROM idempotency_keys WHERE actor_id = ? AND operation = 'launch_missile' AND key = ?`,
			strconv.FormatInt(accountID, 10), key).Scan(&previousHash, &resultID)
		if err == nil {
			if previousHash != hash {
				return appmissile.ErrInvalid
			}
			var arrival string
			err = tx.QueryRowContext(ctx, `SELECT id, origin_galaxy, origin_system, origin_position,
				target_galaxy, target_system, target_position, target_kind, quantity, primary_target, arrives_at, state
				FROM missile_strikes WHERE id = ?`, resultID).Scan(&strike.ID, &strike.Origin.Galaxy, &strike.Origin.System,
				&strike.Origin.Position, &strike.Target.Galaxy, &strike.Target.System, &strike.Target.Position, &strike.TargetKind,
				&strike.Count, &strike.Priority, &arrival, &strike.State)
			if err != nil {
				return err
			}
			strike.ArrivesAt, err = time.Parse(time.RFC3339Nano, arrival)
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		plan, planet, version, targetID, err := r.plan(ctx, tx, accountID, planetID, request, now)
		if err != nil {
			return err
		}
		ownerID, err := playerOfPlanet(ctx, tx, planetID)
		if err != nil {
			return err
		}
		if err := adjustInventory(ctx, tx, planetID, unit.InterplanetaryMissile, -request.Count); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO missile_strikes(owner_player_id, origin_body_id, origin_galaxy, origin_system, origin_position,
			target_body_id, target_created_at, target_galaxy, target_system, target_position, target_kind, quantity, primary_target, seed, ruleset_version, departed_at, arrives_at)
			VALUES (?, ?, ?, ?, ?, ?, (SELECT created_at FROM planets WHERE id = ?), ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, ownerID, planetID,
			planet.Coordinate.Galaxy, planet.Coordinate.System, planet.Coordinate.Position, targetID, targetID,
			request.Target.Galaxy, request.Target.System, request.Target.Position, string(request.TargetKind), request.Count, string(request.Priority), seed, version, timestamp(now), timestamp(plan.ArrivesAt))
		if err != nil {
			return err
		}
		strikeID, err := result.LastInsertId()
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO scheduled_events(event_type, due_at, priority, entity_type, entity_id, ruleset_version, idempotency_key, created_at)
			VALUES ('missile_impact', ?, 10, 'missile', ?, ?, ?, ?)`, timestamp(plan.ArrivesAt), strconv.FormatInt(strikeID, 10), version, fmt.Sprintf("missile-impact:%d", strikeID), timestamp(now)); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO idempotency_keys(actor_id, operation, key, request_hash, result_type, result_id, created_at)
			VALUES (?, 'launch_missile', ?, ?, 'missile', ?, ?)`, strconv.FormatInt(accountID, 10), key, hash, strconv.FormatInt(strikeID, 10), timestamp(now)); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO game_event_log(event_type, actor_type, actor_id, entity_type, entity_id, occurred_at, payload)
			VALUES ('missile_launched', 'account', ?, 'missile', ?, ?, json_object('quantity', ?, 'target_body_id', ?))`, accountID, strikeID, timestamp(now), request.Count, targetID); err != nil {
			return err
		}
		strike = appmissile.Strike{ID: strikeID, Origin: planet.Coordinate, Target: request.Target, TargetKind: request.TargetKind, Count: request.Count, Priority: request.Priority, ArrivesAt: plan.ArrivesAt, State: "outbound"}
		return nil
	})
	return strike, err
}

func (r *MissileRepository) impact(ctx context.Context, tx *sql.Tx, event ScheduledEvent, now time.Time) error {
	var ownerID, targetID, seed int64
	var priority unit.ID
	var payload report.MissilePayload
	var state, targetCreatedAt string
	err := tx.QueryRowContext(ctx, `SELECT s.owner_player_id, s.target_body_id, s.seed, s.primary_target, s.quantity,
		s.origin_galaxy, s.origin_system, s.origin_position, s.target_galaxy, s.target_system, s.target_position, s.target_kind,
		pl.display_name, s.state, s.target_created_at FROM missile_strikes s JOIN players pl ON pl.id = s.owner_player_id WHERE s.id = ?`, event.EntityID).
		Scan(&ownerID, &targetID, &seed, &priority, &payload.Sent, &payload.Origin.Galaxy, &payload.Origin.System, &payload.Origin.Position,
			&payload.Target.Galaxy, &payload.Target.System, &payload.Target.Position, &payload.TargetKind, &payload.AttackerName, &state, &targetCreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil || state != "outbound" {
		return err
	}
	var defenderID int64
	err = tx.QueryRowContext(ctx, `SELECT owner_player_id FROM planets WHERE id = ? AND galaxy = ? AND system = ? AND position = ? AND kind = ? AND created_at = ?`,
		targetID, payload.Target.Galaxy, payload.Target.System, payload.Target.Position, payload.TargetKind, targetCreatedAt).Scan(&defenderID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if errors.Is(err, sql.ErrNoRows) || defenderID == ownerID {
		payload.Aborted = true
		state = "aborted"
	} else {
		target, _, production, err := loadPlanetByID(ctx, tx, targetID, event.DueAt, r.catalogues.Buildings)
		if err != nil {
			return err
		}
		if err := persistProduction(ctx, tx, targetID, production); err != nil {
			return err
		}
		shieldID := targetID
		shield := target
		if target.Kind == building.OnMoon {
			shieldID = target.ParentID
			var shieldProductionState = production
			shield, _, shieldProductionState, err = loadPlanetByID(ctx, tx, shieldID, event.DueAt, r.catalogues.Buildings)
			if err != nil {
				return err
			}
			if err := persistProduction(ctx, tx, shieldID, shieldProductionState); err != nil {
				return err
			}
		}
		var weapons int
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT level FROM player_research WHERE player_id = ? AND research_id = 'weapons_technology'), 0)`, ownerID).Scan(&weapons); err != nil {
			return err
		}
		outcome, err := missile.Resolve(payload.Sent, shield.Units[unit.AntiBallisticMissile], weapons,
			target.Researches[research.ArmourTechnology], priority, target.Units, r.catalogues.Units, random.NewSeeded(uint64(seed)))
		if err != nil {
			return err
		}
		if outcome.Intercepted > 0 {
			if err := adjustInventory(ctx, tx, shieldID, unit.AntiBallisticMissile, -outcome.Intercepted); err != nil {
				return err
			}
		}
		for id, count := range outcome.Losses {
			if err := adjustInventory(ctx, tx, targetID, id, -count); err != nil {
				return err
			}
		}
		payload.Intercepted = outcome.Intercepted
		payload.Losses = inventoryDocument(outcome.Losses)
		// Complete interception supplies no reconnaissance about untouched defenses.
		if payload.Intercepted < payload.Sent {
			payload.Remaining = inventoryDocument(outcome.Remaining)
		}
		state = "completed"
		if _, err := insertReport(ctx, tx, defenderID, report.MissileDefense, "planet", targetID, payload.Target, event.DueAt, payload); err != nil {
			return err
		}
	}
	if _, err := insertReport(ctx, tx, ownerID, report.MissileAttack, "planet", targetID, payload.Target, event.DueAt, payload); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE missile_strikes SET state = ?, resolved_at = ? WHERE id = ? AND state = 'outbound'`, state, timestamp(event.DueAt), event.EntityID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO game_event_log(event_type, actor_type, actor_id, entity_type, entity_id, occurred_at, payload)
		VALUES ('missile_impact', 'player', ?, 'missile', ?, ?, json_object('state', ?, 'intercepted', ?))`, ownerID, event.EntityID, timestamp(event.DueAt), state, payload.Intercepted)
	return err
}
