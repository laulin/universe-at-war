package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	appfleet "universeatwar/internal/app/fleet"
	"universeatwar/internal/domain/economy"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/unit"
	"universeatwar/internal/domain/universe"
)

// fleetRow is the persisted shape of a mission.
type fleetRow struct {
	id             int64
	ownerPlayerID  int64
	originPlanetID int64
	origin         universe.Coordinate
	target         universe.Coordinate
	targetKind     domainfleet.TargetKind
	targetPlanetID int64
	mission        domainfleet.Mission
	speedPercent   int
	fuel           int64
	seed           int64
	rulesetVersion int64
	departedAt     time.Time
	arrivesAt      time.Time
	returnsAt      *time.Time
	recalledAt     *time.Time
	state          domainfleet.State
	version        int64
}

// resolveArrival applies the mission of a fleet that has reached its target.
func (r *FleetRepository) resolveArrival(ctx context.Context, tx *sql.Tx, event ScheduledEvent, now time.Time) error {
	fleetID, err := strconv.ParseInt(event.EntityID, 10, 64)
	if err != nil {
		return errors.New("fleet repository: invalid fleet event reference")
	}
	row, err := loadFleetRow(ctx, tx, fleetID)
	if err != nil {
		return err
	}
	if row.state != domainfleet.Outbound {
		// A recall won the race, or the arrival was already applied.
		return nil
	}
	cargo, err := loadCargo(ctx, tx, fleetID)
	if err != nil {
		return err
	}
	targetPlanetID, _, err := planetAt(ctx, tx, row.target)
	if err != nil {
		return err
	}
	switch row.mission {
	case domainfleet.MissionRecycle:
		return r.resolveRecycling(ctx, tx, row, event.DueAt, now)
	case domainfleet.MissionColonize:
		return r.resolveColonization(ctx, tx, row, event.DueAt, now)
	}
	if targetPlanetID == 0 {
		return r.abortMission(ctx, tx, row, "target_missing", now)
	}
	switch row.mission {
	case domainfleet.MissionDeploy:
		return r.completeDeployment(ctx, tx, row, targetPlanetID, cargo, event.DueAt, now)
	case domainfleet.MissionEspionage:
		return r.resolveEspionage(ctx, tx, row, targetPlanetID, event.DueAt, now)
	case domainfleet.MissionAttack:
		return r.resolveCombat(ctx, tx, row, targetPlanetID, event.DueAt, now)
	default:
		return r.completeTransport(ctx, tx, row, targetPlanetID, cargo, event.DueAt, now)
	}
}

// completeTransport unloads what the target can store and sends the fleet home
// with the remainder.
func (r *FleetRepository) completeTransport(ctx context.Context, tx *sql.Tx, row fleetRow, targetPlanetID int64, cargo economy.Resources, dueAt, now time.Time) error {
	target, _, production, err := loadPlanetByID(ctx, tx, targetPlanetID, dueAt, r.catalogues.Buildings)
	if err != nil {
		return err
	}
	delivered := deliverable(cargo, production.Stock, target.Capacity)
	production.Stock = economy.Resources{
		Metal:     production.Stock.Metal + delivered.Metal,
		Crystal:   production.Stock.Crystal + delivered.Crystal,
		Deuterium: production.Stock.Deuterium + delivered.Deuterium,
	}
	if err := persistProduction(ctx, tx, targetPlanetID, production); err != nil {
		return err
	}
	remaining := economy.Resources{
		Metal:     cargo.Metal - delivered.Metal,
		Crystal:   cargo.Crystal - delivered.Crystal,
		Deuterium: cargo.Deuterium - delivered.Deuterium,
	}
	if err := storeCargo(ctx, tx, row.id, remaining); err != nil {
		return err
	}
	returnsAt := row.arrivesAt.Add(row.arrivesAt.Sub(row.departedAt))
	if row.returnsAt != nil {
		returnsAt = *row.returnsAt
	}
	if err := transitionFleet(ctx, tx, row, domainfleet.Returning, "arrived", now); err != nil {
		return err
	}
	if err := scheduleReturn(ctx, tx, row.id, row.rulesetVersion, returnsAt, now); err != nil {
		return err
	}
	return logFleetEvent(ctx, tx, "fleet_arrived", row.id, targetPlanetID, now,
		fmt.Sprintf("json_object('mission', '%s', 'delivered_metal', %d, 'delivered_crystal', %d, 'delivered_deuterium', %d)",
			row.mission, delivered.Metal, delivered.Crystal, delivered.Deuterium))
}

// completeDeployment moves the ships and the cargo to the target for good. The
// cargo above the storage capacity is lost, which the journal records.
func (r *FleetRepository) completeDeployment(ctx context.Context, tx *sql.Tx, row fleetRow, targetPlanetID int64, cargo economy.Resources, dueAt, now time.Time) error {
	target, _, production, err := loadPlanetByID(ctx, tx, targetPlanetID, dueAt, r.catalogues.Buildings)
	if err != nil {
		return err
	}
	delivered := deliverable(cargo, production.Stock, target.Capacity)
	production.Stock = economy.Resources{
		Metal:     production.Stock.Metal + delivered.Metal,
		Crystal:   production.Stock.Crystal + delivered.Crystal,
		Deuterium: production.Stock.Deuterium + delivered.Deuterium,
	}
	if err := persistProduction(ctx, tx, targetPlanetID, production); err != nil {
		return err
	}
	if err := moveShips(ctx, tx, row.id, targetPlanetID); err != nil {
		return err
	}
	if err := storeCargo(ctx, tx, row.id, economy.Resources{}); err != nil {
		return err
	}
	if err := transitionFleet(ctx, tx, row, domainfleet.Completed, "deployed", now); err != nil {
		return err
	}
	lost := economy.Resources{
		Metal:     cargo.Metal - delivered.Metal,
		Crystal:   cargo.Crystal - delivered.Crystal,
		Deuterium: cargo.Deuterium - delivered.Deuterium,
	}
	return logFleetEvent(ctx, tx, "fleet_arrived", row.id, targetPlanetID, now,
		fmt.Sprintf("json_object('mission', '%s', 'lost_metal', %d, 'lost_crystal', %d, 'lost_deuterium', %d)",
			row.mission, lost.Metal, lost.Crystal, lost.Deuterium))
}

// abortMission sends a fleet home when its target no longer exists.
func (r *FleetRepository) abortMission(ctx context.Context, tx *sql.Tx, row fleetRow, reason string, now time.Time) error {
	returnsAt := row.arrivesAt.Add(row.arrivesAt.Sub(row.departedAt))
	if row.returnsAt != nil {
		returnsAt = *row.returnsAt
	}
	if err := transitionFleet(ctx, tx, row, domainfleet.Returning, reason, now); err != nil {
		return err
	}
	if err := scheduleReturn(ctx, tx, row.id, row.rulesetVersion, returnsAt, now); err != nil {
		return err
	}
	return logFleetEvent(ctx, tx, "mission_aborted", row.id, row.originPlanetID, now,
		fmt.Sprintf("json_object('mission', '%s', 'reason', '%s')", row.mission, reason))
}

// resolveReturn brings the ships and the cargo back to the origin.
func (r *FleetRepository) resolveReturn(ctx context.Context, tx *sql.Tx, event ScheduledEvent, now time.Time) error {
	fleetID, err := strconv.ParseInt(event.EntityID, 10, 64)
	if err != nil {
		return errors.New("fleet repository: invalid fleet event reference")
	}
	row, err := loadFleetRow(ctx, tx, fleetID)
	if err != nil {
		return err
	}
	if row.state != domainfleet.Returning && row.state != domainfleet.Recalled {
		return nil
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM planets WHERE id = ?)", row.originPlanetID).Scan(&exists); err != nil {
		return fmt.Errorf("fleet repository: inspect origin: %w", err)
	}
	if !exists {
		if err := transitionFleet(ctx, tx, row, domainfleet.Destroyed, "origin_lost", now); err != nil {
			return err
		}
		return logFleetEvent(ctx, tx, "fleet_destroyed", row.id, row.originPlanetID, now,
			"json_object('reason', 'origin_lost')")
	}
	cargo, err := loadCargo(ctx, tx, fleetID)
	if err != nil {
		return err
	}
	origin, _, production, err := loadPlanetByID(ctx, tx, row.originPlanetID, event.DueAt, r.catalogues.Buildings)
	if err != nil {
		return err
	}
	delivered := deliverable(cargo, production.Stock, origin.Capacity)
	production.Stock = economy.Resources{
		Metal:     production.Stock.Metal + delivered.Metal,
		Crystal:   production.Stock.Crystal + delivered.Crystal,
		Deuterium: production.Stock.Deuterium + delivered.Deuterium,
	}
	if err := persistProduction(ctx, tx, row.originPlanetID, production); err != nil {
		return err
	}
	if err := moveShips(ctx, tx, row.id, row.originPlanetID); err != nil {
		return err
	}
	if err := storeCargo(ctx, tx, row.id, economy.Resources{}); err != nil {
		return err
	}
	if err := transitionFleet(ctx, tx, row, domainfleet.Completed, "returned", now); err != nil {
		return err
	}
	return logFleetEvent(ctx, tx, "fleet_returned", row.id, row.originPlanetID, now,
		fmt.Sprintf("json_object('metal', %d, 'crystal', %d, 'deuterium', %d)",
			delivered.Metal, delivered.Crystal, delivered.Deuterium))
}

// deliverable is what a target can actually store out of a cargo.
func deliverable(cargo, stock, capacity economy.Resources) economy.Resources {
	return economy.Resources{
		Metal:     roomFor(cargo.Metal, stock.Metal, capacity.Metal),
		Crystal:   roomFor(cargo.Crystal, stock.Crystal, capacity.Crystal),
		Deuterium: roomFor(cargo.Deuterium, stock.Deuterium, capacity.Deuterium),
	}
}

func roomFor(cargo, stock, capacity int64) int64 {
	room := capacity - stock
	if room <= 0 {
		return 0
	}
	if cargo > room {
		return room
	}
	return cargo
}

// moveShips unloads the composition of a fleet onto a planet.
func moveShips(ctx context.Context, tx *sql.Tx, fleetID, planetID int64) error {
	rows, err := tx.QueryContext(ctx, "SELECT unit_id, quantity FROM fleet_ships WHERE fleet_id = ? ORDER BY unit_id", fleetID)
	if err != nil {
		return fmt.Errorf("fleet repository: read composition: %w", err)
	}
	defer rows.Close()
	composition := domainfleet.Composition{}
	for rows.Next() {
		var id string
		var quantity int64
		if err := rows.Scan(&id, &quantity); err != nil {
			return fmt.Errorf("fleet repository: scan composition: %w", err)
		}
		composition[unit.ID(id)] = quantity
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("fleet repository: iterate composition: %w", err)
	}
	for _, id := range sortedComposition(composition) {
		if err := adjustInventory(ctx, tx, planetID, id, composition[id]); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM fleet_ships WHERE fleet_id = ?", fleetID); err != nil {
		return fmt.Errorf("fleet repository: clear composition: %w", err)
	}
	return nil
}

func storeCargo(ctx context.Context, tx *sql.Tx, fleetID int64, cargo economy.Resources) error {
	if _, err := tx.ExecContext(ctx,
		"UPDATE fleet_cargo SET metal = ?, crystal = ?, deuterium = ? WHERE fleet_id = ?",
		cargo.Metal, cargo.Crystal, cargo.Deuterium, fleetID); err != nil {
		return fmt.Errorf("fleet repository: store cargo: %w", err)
	}
	return nil
}

// transitionFleet moves a fleet to a new state, guarded by its version so a
// concurrent command cannot apply a second transition.
func transitionFleet(ctx context.Context, tx *sql.Tx, row fleetRow, to domainfleet.State, reason string, now time.Time) error {
	if !domainfleet.CanTransition(row.state, to) {
		return fmt.Errorf("fleet repository: forbidden transition %s to %s", row.state, to)
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE fleets SET state = ?, version = version + 1 WHERE id = ? AND state = ? AND version = ?
	`, string(to), row.id, string(row.state), row.version)
	if err != nil {
		return fmt.Errorf("fleet repository: transition fleet: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("fleet repository: transition fleet: %w", err)
	}
	if affected != 1 {
		return appfleet.ErrConflict
	}
	return recordTransition(ctx, tx, row.id, row.state, to, reason, now)
}

func logFleetEvent(ctx context.Context, tx *sql.Tx, eventType string, fleetID, entityID int64, now time.Time, payload string) error {
	query := fmt.Sprintf(`
		INSERT INTO game_event_log(event_type, entity_type, entity_id, occurred_at, payload)
		VALUES (?, 'fleet', ?, ?, %s)
	`, payload)
	if _, err := tx.ExecContext(ctx, query, eventType, fleetID, timestamp(now)); err != nil {
		return fmt.Errorf("fleet repository: log %s: %w", eventType, err)
	}
	return nil
}

func loadFleetRow(ctx context.Context, tx *sql.Tx, fleetID int64) (fleetRow, error) {
	var row fleetRow
	var targetKind, mission, state, departedText, arrivesText string
	var targetPlanetID sql.NullInt64
	var returnsText, recalledText sql.NullString
	err := tx.QueryRowContext(ctx, `
		SELECT id, owner_player_id, origin_planet_id, origin_galaxy, origin_system, origin_position,
			target_galaxy, target_system, target_position, target_kind, target_planet_id, mission,
			speed_percent, fuel, seed, ruleset_version, departed_at, arrives_at, returns_at, recalled_at, state, version
		FROM fleets WHERE id = ?
	`, fleetID).Scan(&row.id, &row.ownerPlayerID, &row.originPlanetID,
		&row.origin.Galaxy, &row.origin.System, &row.origin.Position,
		&row.target.Galaxy, &row.target.System, &row.target.Position, &targetKind, &targetPlanetID, &mission,
		&row.speedPercent, &row.fuel, &row.seed, &row.rulesetVersion, &departedText, &arrivesText, &returnsText, &recalledText,
		&state, &row.version)
	if err != nil {
		return fleetRow{}, fmt.Errorf("fleet repository: read fleet: %w", err)
	}
	row.targetKind = domainfleet.TargetKind(targetKind)
	row.targetPlanetID = targetPlanetID.Int64
	row.mission = domainfleet.Mission(mission)
	row.state = domainfleet.State(state)
	row.departedAt, err = time.Parse(time.RFC3339Nano, departedText)
	if err != nil {
		return fleetRow{}, fmt.Errorf("fleet repository: parse departure: %w", err)
	}
	row.arrivesAt, err = time.Parse(time.RFC3339Nano, arrivesText)
	if err != nil {
		return fleetRow{}, fmt.Errorf("fleet repository: parse arrival: %w", err)
	}
	if returnsText.Valid {
		parsed, parseErr := time.Parse(time.RFC3339Nano, returnsText.String)
		if parseErr != nil {
			return fleetRow{}, fmt.Errorf("fleet repository: parse return: %w", parseErr)
		}
		row.returnsAt = &parsed
	}
	if recalledText.Valid {
		parsed, parseErr := time.Parse(time.RFC3339Nano, recalledText.String)
		if parseErr != nil {
			return fleetRow{}, fmt.Errorf("fleet repository: parse recall: %w", parseErr)
		}
		row.recalledAt = &parsed
	}
	return row, nil
}

func loadCargo(ctx context.Context, tx *sql.Tx, fleetID int64) (economy.Resources, error) {
	var cargo economy.Resources
	if err := tx.QueryRowContext(ctx,
		"SELECT metal, crystal, deuterium FROM fleet_cargo WHERE fleet_id = ?", fleetID).
		Scan(&cargo.Metal, &cargo.Crystal, &cargo.Deuterium); err != nil {
		return economy.Resources{}, fmt.Errorf("fleet repository: read cargo: %w", err)
	}
	return cargo, nil
}

// loadFleetProjection builds the player-facing view of one fleet.
func loadFleetProjection(ctx context.Context, tx *sql.Tx, fleetID int64) (appfleet.Fleet, error) {
	row, err := loadFleetRow(ctx, tx, fleetID)
	if err != nil {
		return appfleet.Fleet{}, err
	}
	cargo, err := loadCargo(ctx, tx, fleetID)
	if err != nil {
		return appfleet.Fleet{}, err
	}
	composition, err := loadComposition(ctx, tx, fleetID)
	if err != nil {
		return appfleet.Fleet{}, err
	}
	return appfleet.Fleet{
		ID: row.id, Mission: row.mission, State: row.state, Origin: row.origin, Target: row.target,
		TargetKind: row.targetKind, OriginID: row.originPlanetID, Composition: composition, Cargo: cargo,
		SpeedPercent: row.speedPercent, Fuel: row.fuel, DepartedAt: row.departedAt, ArrivesAt: row.arrivesAt,
		ReturnsAt: row.returnsAt, RecalledAt: row.recalledAt, Recallable: row.state.Recallable(),
	}, nil
}

func loadComposition(ctx context.Context, tx *sql.Tx, fleetID int64) (domainfleet.Composition, error) {
	rows, err := tx.QueryContext(ctx, "SELECT unit_id, quantity FROM fleet_ships WHERE fleet_id = ? ORDER BY unit_id", fleetID)
	if err != nil {
		return nil, fmt.Errorf("fleet repository: read composition: %w", err)
	}
	defer rows.Close()
	composition := domainfleet.Composition{}
	for rows.Next() {
		var id string
		var quantity int64
		if err := rows.Scan(&id, &quantity); err != nil {
			return nil, fmt.Errorf("fleet repository: scan composition: %w", err)
		}
		composition[unit.ID(id)] = quantity
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("fleet repository: iterate composition: %w", err)
	}
	return composition, nil
}

// fleetsInFlight lists the missions of a player that still occupy a slot.
func fleetsInFlight(ctx context.Context, tx *sql.Tx, playerID int64) ([]appfleet.Fleet, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id FROM fleets WHERE owner_player_id = ? AND state IN ('outbound', 'returning', 'recalled')
		ORDER BY arrives_at, id
	`, playerID)
	if err != nil {
		return nil, fmt.Errorf("fleet repository: list fleets: %w", err)
	}
	defer rows.Close()
	var identifiers []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("fleet repository: scan fleet: %w", err)
		}
		identifiers = append(identifiers, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("fleet repository: iterate fleets: %w", err)
	}
	fleets := make([]appfleet.Fleet, 0, len(identifiers))
	for _, id := range identifiers {
		fleet, err := loadFleetProjection(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		fleets = append(fleets, fleet)
	}
	return fleets, nil
}
