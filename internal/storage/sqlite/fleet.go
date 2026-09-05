package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	appeconomy "universeatwar/internal/app/economy"
	appfleet "universeatwar/internal/app/fleet"
	"universeatwar/internal/domain/catalogue"
	"universeatwar/internal/domain/economy"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/unit"
	"universeatwar/internal/domain/universe"
)

// Event priorities of the fleet engine, documented with the scheduled events.
const (
	fleetArrivalPriority = 30
	fleetReturnPriority  = 40
)

// FleetRepository persists each fleet action in one write transaction.
type FleetRepository struct {
	write      *sql.DB
	catalogues catalogue.Set
}

func NewFleetRepository(write *sql.DB, catalogues catalogue.Set) *FleetRepository {
	return &FleetRepository{write: write, catalogues: catalogues}
}

// RegisterHandlers plugs arrival and return into the shared event processor.
func (r *FleetRepository) RegisterHandlers(processor *EventProcessor) {
	processor.Register("fleet_arrived", func(ctx context.Context, tx *sql.Tx, event ScheduledEvent, now time.Time) error {
		return r.resolveArrival(ctx, tx, event, now)
	})
	processor.Register("fleet_returned", func(ctx context.Context, tx *sql.Tx, event ScheduledEvent, now time.Time) error {
		return r.resolveReturn(ctx, tx, event, now)
	})
}

// Overview lists the stationed units of a planet and the fleets in flight.
func (r *FleetRepository) Overview(ctx context.Context, accountID, planetID int64, now time.Time) (appfleet.Overview, error) {
	var overview appfleet.Overview
	err := withWriteTx(ctx, r.write, "fleet repository: overview", func(tx *sql.Tx) error {
		planet, _, production, err := loadPlanet(ctx, tx, accountID, planetID, now, r.catalogues.Buildings)
		if err != nil {
			return err
		}
		if err := persistProduction(ctx, tx, planet.ID, production); err != nil {
			return err
		}
		playerID, err := playerOfPlanet(ctx, tx, planet.ID)
		if err != nil {
			return err
		}
		fleets, err := fleetsInFlight(ctx, tx, playerID)
		if err != nil {
			return err
		}
		overview = appfleet.Overview{
			Planet:    planet,
			Stationed: planet.Units,
			Slots:     planet.Researches.FleetSlots(),
			Used:      len(fleets),
			Fleets:    fleets,
		}
		return nil
	})
	if err != nil {
		return appfleet.Overview{}, err
	}
	return overview, nil
}

// Preview calculates a mission without writing anything.
func (r *FleetRepository) Preview(ctx context.Context, accountID, planetID int64, request appfleet.LaunchRequest, now time.Time) (domainfleet.Plan, error) {
	transaction, err := r.write.BeginTx(ctx, nil)
	if err != nil {
		return domainfleet.Plan{}, fmt.Errorf("fleet repository: begin preview: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	plan, _, _, err := r.planLaunch(ctx, transaction, accountID, planetID, request, now)
	if err != nil {
		return domainfleet.Plan{}, err
	}
	return plan, nil
}

// Launch removes the ships and the cargo, debits the fuel, creates the fleet and
// its arrival event, then journals it, all in a single transaction.
func (r *FleetRepository) Launch(ctx context.Context, accountID, planetID int64, request appfleet.LaunchRequest, seed int64, idempotencyKey string, now time.Time) (appfleet.Fleet, error) {
	var fleet appfleet.Fleet
	err := withWriteTx(ctx, r.write, "fleet repository: launch", func(tx *sql.Tx) error {
		digest := sha256.Sum256([]byte(launchSignature(planetID, request)))
		requestHash := hex.EncodeToString(digest[:])
		replayed, found, err := replayedFleet(ctx, tx, accountID, "launch_fleet", idempotencyKey, requestHash)
		if err != nil {
			return err
		}
		if found {
			fleet = replayed
			return nil
		}

		plan, planet, targetPlanetID, err := r.planLaunch(ctx, tx, accountID, planetID, request, now)
		if err != nil {
			return err
		}
		production, err := settledProduction(ctx, tx, planet.ID, now)
		if err != nil {
			return err
		}
		production.Stock, err = production.Stock.Debit(plan.Debit)
		if err != nil {
			return err
		}
		if err := persistProduction(ctx, tx, planet.ID, production); err != nil {
			return err
		}
		for id, quantity := range request.Composition {
			if quantity == 0 {
				continue
			}
			if err := adjustInventory(ctx, tx, planet.ID, id, -quantity); err != nil {
				return err
			}
		}
		playerID, err := playerOfPlanet(ctx, tx, planet.ID)
		if err != nil {
			return err
		}
		rulesetVersion, err := activeRulesetVersion(ctx, tx)
		if err != nil {
			return err
		}
		var returnsAt any
		if plan.ReturnsAt != nil {
			returnsAt = timestamp(*plan.ReturnsAt)
		}
		var targetReference any
		if targetPlanetID > 0 {
			targetReference = targetPlanetID
		}
		result, err := tx.ExecContext(ctx, `
			INSERT INTO fleets(owner_player_id, origin_planet_id, origin_galaxy, origin_system, origin_position,
				target_galaxy, target_system, target_position, target_kind, target_planet_id, mission,
				speed_percent, fleet_speed, distance, fuel, seed, ruleset_version, departed_at, arrives_at, returns_at, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, playerID, planet.ID, planet.Coordinate.Galaxy, planet.Coordinate.System, planet.Coordinate.Position,
			request.Target.Galaxy, request.Target.System, request.Target.Position, string(request.TargetKind),
			targetReference, string(request.Mission), request.Percent, plan.Speed, plan.Distance, plan.Fuel, seed,
			rulesetVersion, timestamp(plan.DepartsAt), timestamp(plan.ArrivesAt), returnsAt, timestamp(plan.DepartsAt))
		if err != nil {
			return fmt.Errorf("fleet repository: create fleet: %w", err)
		}
		fleetID, err := result.LastInsertId()
		if err != nil {
			return fmt.Errorf("fleet repository: fleet id: %w", err)
		}
		for _, id := range sortedComposition(request.Composition) {
			if _, err := tx.ExecContext(ctx,
				"INSERT INTO fleet_ships(fleet_id, unit_id, quantity) VALUES (?, ?, ?)",
				fleetID, string(id), request.Composition[id]); err != nil {
				return fmt.Errorf("fleet repository: store composition: %w", err)
			}
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO fleet_cargo(fleet_id, metal, crystal, deuterium) VALUES (?, ?, ?, ?)",
			fleetID, request.Cargo.Metal, request.Cargo.Crystal, request.Cargo.Deuterium); err != nil {
			return fmt.Errorf("fleet repository: store cargo: %w", err)
		}
		if err := recordTransition(ctx, tx, fleetID, "", domainfleet.Outbound, "launched", plan.DepartsAt); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO scheduled_events(event_type, due_at, priority, entity_type, entity_id, ruleset_version, payload, idempotency_key, created_at)
			VALUES ('fleet_arrived', ?, ?, 'fleet', ?, ?, json_object('mission', ?), ?, ?)
		`, timestamp(plan.ArrivesAt), fleetArrivalPriority, strconv.FormatInt(fleetID, 10), rulesetVersion,
			string(request.Mission), fmt.Sprintf("fleet-arrive:%d", fleetID), timestamp(plan.DepartsAt)); err != nil {
			return fmt.Errorf("fleet repository: schedule arrival: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO idempotency_keys(actor_id, operation, key, request_hash, result_type, result_id, created_at)
			VALUES (?, 'launch_fleet', ?, ?, 'fleets', ?, ?)
		`, strconv.FormatInt(accountID, 10), idempotencyKey, requestHash, strconv.FormatInt(fleetID, 10), timestamp(plan.DepartsAt)); err != nil {
			return fmt.Errorf("fleet repository: record idempotency: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO game_event_log(event_type, actor_type, actor_id, entity_type, entity_id, occurred_at, payload)
			VALUES ('fleet_launched', 'account', ?, 'fleet', ?, ?, json_object('mission', ?, 'target', ?, 'fuel', ?))
		`, accountID, fleetID, timestamp(plan.DepartsAt), string(request.Mission), request.Target.String(), plan.Fuel); err != nil {
			return fmt.Errorf("fleet repository: log launch: %w", err)
		}
		loaded, err := loadFleetProjection(ctx, tx, fleetID)
		if err != nil {
			return err
		}
		fleet = loaded
		return nil
	})
	if err != nil {
		return appfleet.Fleet{}, err
	}
	return fleet, nil
}

// planLaunch validates a launch against the current state without mutating it.
func (r *FleetRepository) planLaunch(ctx context.Context, tx *sql.Tx, accountID, planetID int64, request appfleet.LaunchRequest, now time.Time) (domainfleet.Plan, appeconomy.Planet, int64, error) {
	planet, _, _, err := loadPlanet(ctx, tx, accountID, planetID, now, r.catalogues.Buildings)
	if err != nil {
		return domainfleet.Plan{}, appeconomy.Planet{}, 0, err
	}
	if err := r.catalogues.Matches(planet.Rules); err != nil {
		return domainfleet.Plan{}, appeconomy.Planet{}, 0, err
	}
	playerID, err := playerOfPlanet(ctx, tx, planet.ID)
	if err != nil {
		return domainfleet.Plan{}, appeconomy.Planet{}, 0, err
	}
	targetPlanetID, targetOwner, err := planetAt(ctx, tx, request.Target)
	if err != nil {
		return domainfleet.Plan{}, appeconomy.Planet{}, 0, err
	}
	if request.TargetKind == domainfleet.TargetPlanet {
		if targetPlanetID == 0 {
			return domainfleet.Plan{}, appeconomy.Planet{}, 0, domainfleet.ErrInvalidTarget
		}
		if request.Mission.TargetsOwnBody() && targetOwner != playerID {
			return domainfleet.Plan{}, appeconomy.Planet{}, 0, domainfleet.ErrInvalidTarget
		}
		if targetPlanetID == planet.ID {
			return domainfleet.Plan{}, appeconomy.Planet{}, 0, domainfleet.ErrInvalidTarget
		}
	}
	active, err := activeFleetCount(ctx, tx, playerID)
	if err != nil {
		return domainfleet.Plan{}, appeconomy.Planet{}, 0, err
	}
	plan, err := domainfleet.PlanLaunch(domainfleet.LaunchRequest{
		Origin:      planet.Coordinate,
		Target:      request.Target,
		TargetKind:  request.TargetKind,
		Mission:     request.Mission,
		Composition: request.Composition,
		Cargo:       request.Cargo,
		Percent:     request.Percent,
	}, domainfleet.Context{
		Catalogue:    r.catalogues.Units,
		Levels:       planet.Researches,
		Inventory:    planet.Units,
		Stock:        planet.Stock,
		ActiveFleets: active,
		Rules:        planet.Rules,
		Now:          now,
	})
	if err != nil {
		return domainfleet.Plan{}, appeconomy.Planet{}, 0, err
	}
	return plan, planet, targetPlanetID, nil
}

// Recall cancels the arrival of an outbound fleet and sends it home.
func (r *FleetRepository) Recall(ctx context.Context, accountID, fleetID int64, idempotencyKey string, now time.Time) (appfleet.Fleet, error) {
	var fleet appfleet.Fleet
	err := withWriteTx(ctx, r.write, "fleet repository: recall", func(tx *sql.Tx) error {
		digest := sha256.Sum256([]byte(strconv.FormatInt(fleetID, 10)))
		requestHash := hex.EncodeToString(digest[:])
		replayed, found, err := replayedFleet(ctx, tx, accountID, "recall_fleet", idempotencyKey, requestHash)
		if err != nil {
			return err
		}
		if found {
			fleet = replayed
			return nil
		}

		row, err := loadFleetRow(ctx, tx, fleetID)
		if err != nil {
			return err
		}
		owner, err := accountOfPlayer(ctx, tx, row.ownerPlayerID)
		if err != nil {
			return err
		}
		if owner != accountID {
			return appfleet.ErrNotFound
		}
		if !row.state.Recallable() {
			return appfleet.ErrNotRecallable
		}
		result, err := tx.ExecContext(ctx,
			"UPDATE scheduled_events SET state = 'cancelled', processed_at = ? WHERE idempotency_key = ? AND state = 'pending'",
			timestamp(now), fmt.Sprintf("fleet-arrive:%d", fleetID))
		if err != nil {
			return fmt.Errorf("fleet repository: cancel arrival: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("fleet repository: cancel arrival: %w", err)
		}
		if affected != 1 {
			// The arrival was already being processed: the recall lost the race.
			return appfleet.ErrNotRecallable
		}
		returnsAt := domainfleet.RecallReturn(row.departedAt, now)
		if _, err := tx.ExecContext(ctx, `
			UPDATE fleets SET state = 'recalled', recalled_at = ?, returns_at = ?, version = version + 1
			WHERE id = ? AND state = 'outbound' AND version = ?
		`, timestamp(now), timestamp(returnsAt), fleetID, row.version); err != nil {
			return fmt.Errorf("fleet repository: recall fleet: %w", err)
		}
		if err := recordTransition(ctx, tx, fleetID, domainfleet.Outbound, domainfleet.Recalled, "recalled", now); err != nil {
			return err
		}
		if err := scheduleReturn(ctx, tx, fleetID, row.rulesetVersion, returnsAt, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO game_event_log(event_type, actor_type, actor_id, entity_type, entity_id, occurred_at, payload)
			VALUES ('fleet_recalled', 'account', ?, 'fleet', ?, ?, json_object('returns_at', ?))
		`, accountID, fleetID, timestamp(now), timestamp(returnsAt)); err != nil {
			return fmt.Errorf("fleet repository: log recall: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO idempotency_keys(actor_id, operation, key, request_hash, result_type, result_id, created_at)
			VALUES (?, 'recall_fleet', ?, ?, 'fleets', ?, ?)
		`, strconv.FormatInt(accountID, 10), idempotencyKey, requestHash, strconv.FormatInt(fleetID, 10), timestamp(now)); err != nil {
			return fmt.Errorf("fleet repository: record idempotency: %w", err)
		}
		loaded, err := loadFleetProjection(ctx, tx, fleetID)
		if err != nil {
			return err
		}
		fleet = loaded
		return nil
	})
	if err != nil {
		return appfleet.Fleet{}, err
	}
	return fleet, nil
}

func launchSignature(planetID int64, request appfleet.LaunchRequest) string {
	signature := fmt.Sprintf("%d:%s:%s:%s:%d:%d/%d/%d", planetID, request.Mission, request.TargetKind,
		request.Target.String(), request.Percent, request.Cargo.Metal, request.Cargo.Crystal, request.Cargo.Deuterium)
	for _, id := range sortedComposition(request.Composition) {
		signature += fmt.Sprintf(":%s=%d", id, request.Composition[id])
	}
	return signature
}

func sortedComposition(composition domainfleet.Composition) []unit.ID {
	identifiers := make([]unit.ID, 0, len(composition))
	for id, quantity := range composition {
		if quantity > 0 {
			identifiers = append(identifiers, id)
		}
	}
	sort.Slice(identifiers, func(first, second int) bool { return identifiers[first] < identifiers[second] })
	return identifiers
}

// planetAt returns the planet occupying a coordinate, if any.
func planetAt(ctx context.Context, tx *sql.Tx, at universe.Coordinate) (int64, int64, error) {
	var planetID, ownerID int64
	err := tx.QueryRowContext(ctx,
		"SELECT id, owner_player_id FROM planets WHERE galaxy = ? AND system = ? AND position = ?",
		at.Galaxy, at.System, at.Position).Scan(&planetID, &ownerID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, fmt.Errorf("fleet repository: read target planet: %w", err)
	}
	return planetID, ownerID, nil
}

func accountOfPlayer(ctx context.Context, tx *sql.Tx, playerID int64) (int64, error) {
	var accountID int64
	if err := tx.QueryRowContext(ctx, "SELECT account_id FROM players WHERE id = ?", playerID).Scan(&accountID); err != nil {
		return 0, fmt.Errorf("fleet repository: read fleet owner: %w", err)
	}
	return accountID, nil
}

func activeFleetCount(ctx context.Context, tx *sql.Tx, playerID int64) (int, error) {
	var count int
	if err := tx.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM fleets WHERE owner_player_id = ? AND state IN ('outbound', 'returning', 'recalled')",
		playerID).Scan(&count); err != nil {
		return 0, fmt.Errorf("fleet repository: count fleets: %w", err)
	}
	return count, nil
}

func activeRulesetVersion(ctx context.Context, tx *sql.Tx) (int64, error) {
	var version int64
	if err := tx.QueryRowContext(ctx,
		"SELECT version FROM ruleset_versions WHERE status = 'active' ORDER BY version DESC LIMIT 1").Scan(&version); err != nil {
		return 0, fmt.Errorf("fleet repository: read ruleset version: %w", err)
	}
	return version, nil
}

// settledProduction reads the production state of a planet already settled to
// the given instant, ready to be debited.
func settledProduction(ctx context.Context, tx *sql.Tx, planetID int64, now time.Time) (economy.ProductionState, error) {
	var stock economy.Resources
	var remainders economy.Remainders
	var producedText string
	if err := tx.QueryRowContext(ctx, `
		SELECT metal, crystal, deuterium, metal_remainder, crystal_remainder, deuterium_remainder, produced_at
		FROM planet_resources WHERE planet_id = ?
	`, planetID).Scan(&stock.Metal, &stock.Crystal, &stock.Deuterium,
		&remainders.Metal, &remainders.Crystal, &remainders.Deuterium, &producedText); err != nil {
		return economy.ProductionState{}, fmt.Errorf("fleet repository: read resources: %w", err)
	}
	producedAt, err := time.Parse(time.RFC3339Nano, producedText)
	if err != nil {
		return economy.ProductionState{}, fmt.Errorf("fleet repository: parse production time: %w", err)
	}
	return economy.ProductionState{Stock: stock, Remainder: remainders, ProducedAt: producedAt}, nil
}

func recordTransition(ctx context.Context, tx *sql.Tx, fleetID int64, from, to domainfleet.State, reason string, at time.Time) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO fleet_transitions(fleet_id, from_state, to_state, reason, occurred_at) VALUES (?, ?, ?, ?, ?)
	`, fleetID, string(from), string(to), reason, timestamp(at)); err != nil {
		return fmt.Errorf("fleet repository: record transition: %w", err)
	}
	return nil
}

func scheduleReturn(ctx context.Context, tx *sql.Tx, fleetID, rulesetVersion int64, returnsAt, now time.Time) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO scheduled_events(event_type, due_at, priority, entity_type, entity_id, ruleset_version, payload, idempotency_key, created_at)
		VALUES ('fleet_returned', ?, ?, 'fleet', ?, ?, json_object(), ?, ?)
	`, timestamp(returnsAt), fleetReturnPriority, strconv.FormatInt(fleetID, 10), rulesetVersion,
		fmt.Sprintf("fleet-return:%d", fleetID), timestamp(now)); err != nil {
		return fmt.Errorf("fleet repository: schedule return: %w", err)
	}
	return nil
}

func replayedFleet(ctx context.Context, tx *sql.Tx, accountID int64, operation, idempotencyKey, requestHash string) (appfleet.Fleet, bool, error) {
	var storedHash, resultID string
	err := tx.QueryRowContext(ctx, `
		SELECT request_hash, result_id FROM idempotency_keys WHERE actor_id = ? AND operation = ? AND key = ?
	`, strconv.FormatInt(accountID, 10), operation, idempotencyKey).Scan(&storedHash, &resultID)
	if errors.Is(err, sql.ErrNoRows) {
		return appfleet.Fleet{}, false, nil
	}
	if err != nil {
		return appfleet.Fleet{}, false, fmt.Errorf("fleet repository: inspect idempotency: %w", err)
	}
	if storedHash != requestHash {
		return appfleet.Fleet{}, false, appfleet.ErrInvalidRequest
	}
	fleetID, err := strconv.ParseInt(resultID, 10, 64)
	if err != nil {
		return appfleet.Fleet{}, false, errors.New("fleet repository: corrupt idempotency result")
	}
	fleet, err := loadFleetProjection(ctx, tx, fleetID)
	if err != nil {
		return appfleet.Fleet{}, false, err
	}
	return fleet, true, nil
}
