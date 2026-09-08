package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	appeconomy "universeatwar/internal/app/economy"
	"universeatwar/internal/domain/building"
	"universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/research"
	"universeatwar/internal/domain/rules"
	"universeatwar/internal/domain/unit"
	"universeatwar/internal/domain/universe"
)

// EconomyRepository persists each economic action in one serialized write transaction.
type EconomyRepository struct {
	write     *sql.DB
	catalogue building.Catalogue
}

func NewEconomyRepository(write *sql.DB, catalogue building.Catalogue) *EconomyRepository {
	return &EconomyRepository{write: write, catalogue: catalogue}
}

// RegisterHandlers plugs building completion into the shared event processor.
func (r *EconomyRepository) RegisterHandlers(processor *EventProcessor) {
	processor.Register("building_completed", func(ctx context.Context, transaction *sql.Tx, event ScheduledEvent, now time.Time) error {
		return completeBuilding(ctx, transaction, event, now, r.catalogue)
	})
}

func (r *EconomyRepository) CreateEmpire(ctx context.Context, accountID int64, name string, now time.Time) (appeconomy.Planet, error) {
	tx, err := r.write.BeginTx(ctx, nil)
	if err != nil {
		return appeconomy.Planet{}, fmt.Errorf("economy repository: begin empire: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var existing int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM players WHERE account_id = ?", accountID).Scan(&existing); err != nil {
		return appeconomy.Planet{}, fmt.Errorf("economy repository: inspect empire: %w", err)
	}
	if existing != 0 {
		return appeconomy.Planet{}, appeconomy.ErrEmpireExists
	}
	configured, _, err := activeRuleset(ctx, tx)
	if err != nil {
		return appeconomy.Planet{}, err
	}
	coordinate, err := firstFreeCoordinate(ctx, tx, configured)
	if err != nil {
		return appeconomy.Planet{}, err
	}
	now = now.UTC().Truncate(time.Second)
	result, err := tx.ExecContext(ctx, "INSERT INTO players(account_id, display_name, created_at) VALUES (?, ?, ?)", accountID, name, timestamp(now))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return appeconomy.Planet{}, appeconomy.ErrEmpireExists
		}
		return appeconomy.Planet{}, fmt.Errorf("economy repository: create player: %w", err)
	}
	playerID, err := result.LastInsertId()
	if err != nil {
		return appeconomy.Planet{}, fmt.Errorf("economy repository: player id: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO account_roles(account_id, role, granted_at, granted_by_account_id) VALUES (?, 'PLAYER', ?, ?)`, accountID, timestamp(now), accountID); err != nil {
		return appeconomy.Planet{}, fmt.Errorf("economy repository: grant player role: %w", err)
	}
	fields := (configured.Topology.MinPlanetFields + configured.Topology.MaxPlanetFields) / 2
	maximumTemperature := 40 - (coordinate.Position-8)*10
	result, err = tx.ExecContext(ctx, `
		INSERT INTO planets(owner_player_id, name, galaxy, system, position, total_fields, minimum_temperature, maximum_temperature, created_at)
		VALUES (?, 'Planète mère', ?, ?, ?, ?, ?, ?, ?)
	`, playerID, coordinate.Galaxy, coordinate.System, coordinate.Position, fields, maximumTemperature-40, maximumTemperature, timestamp(now))
	if err != nil {
		return appeconomy.Planet{}, fmt.Errorf("economy repository: create planet: %w", err)
	}
	planetID, err := result.LastInsertId()
	if err != nil {
		return appeconomy.Planet{}, fmt.Errorf("economy repository: planet id: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO planet_resources(planet_id, produced_at) VALUES (?, ?)", planetID, timestamp(now)); err != nil {
		return appeconomy.Planet{}, fmt.Errorf("economy repository: create resources: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO game_event_log(event_type, actor_type, actor_id, entity_type, entity_id, occurred_at, payload) VALUES ('empire_created', 'account', ?, 'planet', ?, ?, json_object('coordinate', ?))`, accountID, planetID, timestamp(now), coordinate.String()); err != nil {
		return appeconomy.Planet{}, fmt.Errorf("economy repository: log empire: %w", err)
	}
	planet := appeconomy.Planet{
		ID: planetID, Kind: building.OnPlanet, Name: "Planète mère", PlayerName: name, Coordinate: coordinate,
		TotalFields: fields, MinimumTemperature: maximumTemperature - 40, MaximumTemperature: maximumTemperature,
		Stock: economy.Resources{Metal: 500, Crystal: 500}, Levels: building.Levels{}, Rules: configured,
	}
	if err := enrichEconomy(&planet); err != nil {
		return appeconomy.Planet{}, err
	}
	if err := tx.Commit(); err != nil {
		return appeconomy.Planet{}, fmt.Errorf("economy repository: commit empire: %w", err)
	}
	return planet, nil
}

func firstFreeCoordinate(ctx context.Context, tx *sql.Tx, configured rules.Ruleset) (universe.Coordinate, error) {
	preferred := (configured.Topology.PositionsPerSystem + 1) / 2
	for galaxy := 1; galaxy <= configured.Topology.Galaxies; galaxy++ {
		for system := 1; system <= configured.Topology.SystemsPerGalaxy; system++ {
			positions := make([]int, 0, configured.Topology.PositionsPerSystem)
			positions = append(positions, preferred)
			for position := 1; position <= configured.Topology.PositionsPerSystem; position++ {
				if position != preferred {
					positions = append(positions, position)
				}
			}
			for _, position := range positions {
				var occupied int
				if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM planets WHERE galaxy = ? AND system = ? AND position = ?)", galaxy, system, position).Scan(&occupied); err != nil {
					return universe.Coordinate{}, fmt.Errorf("economy repository: inspect position: %w", err)
				}
				if occupied == 0 {
					return universe.Coordinate{Galaxy: galaxy, System: system, Position: position}, nil
				}
			}
		}
	}
	return universe.Coordinate{}, appeconomy.ErrUniverseFull
}

// Planet settles and returns one planet of the account. A zero identifier
// selects the oldest one.
func (r *EconomyRepository) Planet(ctx context.Context, accountID, planetID int64, now time.Time, catalogue building.Catalogue) (appeconomy.Planet, error) {
	var planet appeconomy.Planet
	err := withWriteTx(ctx, r.write, "economy repository: planet", func(tx *sql.Tx) error {
		loaded, _, state, err := loadPlanet(ctx, tx, accountID, planetID, now, catalogue)
		if err != nil {
			return err
		}
		if err := persistProduction(ctx, tx, loaded.ID, state); err != nil {
			return err
		}
		planet = loaded
		return nil
	})
	if err != nil {
		return appeconomy.Planet{}, err
	}
	return planet, nil
}

// Planets settles and returns every body of the account, oldest first.
func (r *EconomyRepository) Planets(ctx context.Context, accountID int64, now time.Time, catalogue building.Catalogue) ([]appeconomy.Planet, error) {
	var planets []appeconomy.Planet
	err := withWriteTx(ctx, r.write, "economy repository: planets", func(tx *sql.Tx) error {
		identifiers, err := ownedPlanetIDs(ctx, tx, accountID)
		if err != nil {
			return err
		}
		planets = make([]appeconomy.Planet, 0, len(identifiers))
		for _, planetID := range identifiers {
			planet, _, state, err := loadPlanetByID(ctx, tx, planetID, now, catalogue)
			if err != nil {
				return err
			}
			if err := persistProduction(ctx, tx, planet.ID, state); err != nil {
				return err
			}
			planets = append(planets, planet)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return planets, nil
}

// ownedPlanetIDs lists the planets of an account and refuses an account without
// an empire, so no caller has to guess between an empty list and no player.
func ownedPlanetIDs(ctx context.Context, tx *sql.Tx, accountID int64) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT p.id FROM planets p
		JOIN players pl ON pl.id = p.owner_player_id
		WHERE pl.account_id = ?
		ORDER BY p.id
	`, accountID)
	if err != nil {
		return nil, fmt.Errorf("economy repository: list planets: %w", err)
	}
	defer rows.Close()
	var identifiers []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("economy repository: scan planet: %w", err)
		}
		identifiers = append(identifiers, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("economy repository: iterate planets: %w", err)
	}
	if len(identifiers) == 0 {
		return nil, appeconomy.ErrNoEmpire
	}
	return identifiers, nil
}

// EnqueueBuilding appends one construction to a body's queue. The cost is taken
// here and snapshotted on the row, so an entry that reached the queue is
// already paid for; only its duration waits until it reaches the head.
func (r *EconomyRepository) EnqueueBuilding(ctx context.Context, accountID, planetID int64, id building.ID, idempotencyKey string, now time.Time, catalogue building.Catalogue) (appeconomy.Queue, error) {
	tx, err := r.write.BeginTx(ctx, nil)
	if err != nil {
		return appeconomy.Queue{}, fmt.Errorf("economy repository: begin construction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	requestDigest := sha256.Sum256([]byte(fmt.Sprintf("%d:%s", planetID, id)))
	requestHash := hex.EncodeToString(requestDigest[:])
	var storedHash, resultID string
	err = tx.QueryRowContext(ctx, `SELECT request_hash, result_id FROM idempotency_keys WHERE actor_id = ? AND operation = 'start_building' AND key = ?`, strconv.FormatInt(accountID, 10), idempotencyKey).Scan(&storedHash, &resultID)
	if err == nil {
		if storedHash != requestHash {
			return appeconomy.Queue{}, appeconomy.ErrInvalidRequest
		}
		queueID, parseErr := strconv.ParseInt(resultID, 10, 64)
		if parseErr != nil {
			return appeconomy.Queue{}, errors.New("economy repository: corrupt idempotency result")
		}
		queue, loadErr := loadQueue(ctx, tx, queueID, accountID)
		if loadErr != nil {
			return appeconomy.Queue{}, loadErr
		}
		if commitErr := tx.Commit(); commitErr != nil {
			return appeconomy.Queue{}, commitErr
		}
		return queue, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return appeconomy.Queue{}, fmt.Errorf("economy repository: inspect idempotency: %w", err)
	}
	planet, rulesetVersion, state, err := loadPlanet(ctx, tx, accountID, planetID, now, catalogue)
	if err != nil {
		return appeconomy.Queue{}, err
	}
	if len(planet.Queue) >= planet.Rules.Progression.QueueLength {
		return appeconomy.Queue{}, appeconomy.ErrQueueFull
	}
	if err := facilityIsIdle(ctx, tx, planet.ID, id); err != nil {
		return appeconomy.Queue{}, err
	}
	// Every entry still in the queue counts: its level is the one the new order
	// builds upon, and the field it will consume is already spoken for.
	projected := projectedLevels(planet)
	plan, err := catalogue.Plan(id, planet.Kind, projected, planet.Researches.Generic(), planet.UsedFields+len(planet.Queue), planet.TotalFields, planet.Rules)
	if err != nil {
		return appeconomy.Queue{}, err
	}
	state.Stock, err = state.Stock.Debit(plan.Cost)
	if err != nil {
		return appeconomy.Queue{}, err
	}
	if err := persistProduction(ctx, tx, planet.ID, state); err != nil {
		return appeconomy.Queue{}, err
	}
	queuedAt := now.UTC().Truncate(time.Second)
	position := 0
	if last := len(planet.Queue); last > 0 {
		position = planet.Queue[last-1].Position + 1
	}
	head := len(planet.Queue) == 0
	entryState := "queued"
	var startedAt, completesAt time.Time
	var startedValue, completesValue any
	if head {
		entryState = "active"
		startedAt = queuedAt
		completesAt = startedAt.Add(plan.Duration)
		startedValue, completesValue = timestamp(startedAt), timestamp(completesAt)
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO building_queue(planet_id, building_id, target_level, metal_cost, crystal_cost, deuterium_cost, ruleset_version, position, queued_at, started_at, completes_at, state)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, planet.ID, string(id), plan.TargetLevel, plan.Cost.Metal, plan.Cost.Crystal, plan.Cost.Deuterium, rulesetVersion, position, timestamp(queuedAt), startedValue, completesValue, entryState)
	if err != nil {
		if strings.Contains(err.Error(), "building_queue_one_active_idx") || strings.Contains(err.Error(), "building_queue_position_idx") {
			return appeconomy.Queue{}, appeconomy.ErrQueueBusy
		}
		return appeconomy.Queue{}, fmt.Errorf("economy repository: enqueue building: %w", err)
	}
	queueID, err := result.LastInsertId()
	if err != nil {
		return appeconomy.Queue{}, fmt.Errorf("economy repository: queue id: %w", err)
	}
	if head {
		if err := scheduleBuilding(ctx, tx, queueID, rulesetVersion, startedAt, completesAt); err != nil {
			return appeconomy.Queue{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO idempotency_keys(actor_id, operation, key, request_hash, result_type, result_id, created_at) VALUES (?, 'start_building', ?, ?, 'building_queue', ?, ?)`, strconv.FormatInt(accountID, 10), idempotencyKey, requestHash, strconv.FormatInt(queueID, 10), timestamp(queuedAt)); err != nil {
		return appeconomy.Queue{}, fmt.Errorf("economy repository: record idempotency: %w", err)
	}
	journal := "building_queued"
	if head {
		journal = "building_started"
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO game_event_log(event_type, actor_type, actor_id, entity_type, entity_id, occurred_at, payload) VALUES (?, 'account', ?, 'planet', ?, ?, json_object('building_id', ?, 'target_level', ?, 'queue_id', ?, 'position', ?))`, journal, accountID, planet.ID, timestamp(queuedAt), string(id), plan.TargetLevel, queueID, position); err != nil {
		return appeconomy.Queue{}, fmt.Errorf("economy repository: log building order: %w", err)
	}
	queue := appeconomy.Queue{ID: queueID, Building: id, TargetLevel: plan.TargetLevel, Cost: plan.Cost, Position: position, StartedAt: startedAt, CompletesAt: completesAt, State: entryState}
	if err := tx.Commit(); err != nil {
		return appeconomy.Queue{}, fmt.Errorf("economy repository: commit construction: %w", err)
	}
	return queue, nil
}

// completeBuilding raises the finished level exactly once. A queue that is no
// longer active makes the event a successful no-op so that a redelivery after a
// crash cannot increment twice.
func completeBuilding(ctx context.Context, tx *sql.Tx, event ScheduledEvent, now time.Time, catalogue building.Catalogue) error {
	queueID, err := strconv.ParseInt(event.EntityID, 10, 64)
	if err != nil {
		return errors.New("economy repository: invalid queue event reference")
	}
	var planetID int64
	var buildingID string
	var targetLevel int
	var queueState string
	err = tx.QueryRowContext(ctx, "SELECT planet_id, building_id, target_level, state FROM building_queue WHERE id = ?", queueID).Scan(&planetID, &buildingID, &targetLevel, &queueState)
	if err != nil {
		return fmt.Errorf("economy repository: read due queue: %w", err)
	}
	if queueState != "active" {
		return nil
	}
	planet, _, production, err := loadPlanetByID(ctx, tx, planetID, event.DueAt, catalogue)
	if err != nil {
		return err
	}
	if err := persistProduction(ctx, tx, planetID, production); err != nil {
		return err
	}
	current := planet.Levels[building.ID(buildingID)]
	if current+1 != targetLevel {
		return errors.New("economy repository: building queue target is stale")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO planet_buildings(planet_id, building_id, level) VALUES (?, ?, ?) ON CONFLICT(planet_id, building_id) DO UPDATE SET level = excluded.level`, planetID, buildingID, targetLevel); err != nil {
		return fmt.Errorf("economy repository: complete building level: %w", err)
	}
	extraFields := 0
	switch building.ID(buildingID) {
	case building.Terraformer:
		extraFields = 5
	case building.LunarBase:
		extraFields = planet.Rules.Expansion.LunarBaseFields
	}
	if _, err := tx.ExecContext(ctx, "UPDATE planets SET used_fields = used_fields + 1, total_fields = total_fields + ? WHERE id = ?", extraFields, planetID); err != nil {
		return fmt.Errorf("economy repository: consume planet field: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE building_queue SET state = 'completed', completed_at = ? WHERE id = ? AND state = 'active'", timestamp(now), queueID); err != nil {
		return fmt.Errorf("economy repository: complete queue: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO game_event_log(event_type, entity_type, entity_id, occurred_at, payload) VALUES ('building_completed', 'planet', ?, ?, json_object('building_id', ?, 'level', ?, 'queue_id', ?))`, planetID, timestamp(now), buildingID, targetLevel, queueID); err != nil {
		return fmt.Errorf("economy repository: log completion: %w", err)
	}
	// The queue must not gain idle time from a late settlement, so the next
	// entry starts at the instant this one was due.
	return promoteNextBuilding(ctx, tx, planetID, event.DueAt, catalogue)
}

// scheduleBuilding books the completion of the entry now at the head.
func scheduleBuilding(ctx context.Context, tx *sql.Tx, queueID, rulesetVersion int64, startedAt, completesAt time.Time) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO scheduled_events(event_type, due_at, priority, entity_type, entity_id, ruleset_version, payload, idempotency_key, created_at)
		VALUES ('building_completed', ?, 50, 'building_queue', ?, ?, json_object('queue_id', ?), ?, ?)
	`, timestamp(completesAt), strconv.FormatInt(queueID, 10), rulesetVersion, queueID, fmt.Sprintf("building-complete:%d", queueID), timestamp(startedAt)); err != nil {
		return fmt.Errorf("economy repository: schedule building: %w", err)
	}
	return nil
}

// promoteNextBuilding starts whichever entry now waits at the front of the
// queue. Only here is its duration decided, so a robotics factory finished a
// moment ago speeds up everything still queued behind it.
func promoteNextBuilding(ctx context.Context, tx *sql.Tx, planetID int64, now time.Time, catalogue building.Catalogue) error {
	var entryID int64
	var cost economy.Resources
	err := tx.QueryRowContext(ctx, `
		SELECT id, metal_cost, crystal_cost, deuterium_cost FROM building_queue
		WHERE planet_id = ? AND state = 'queued' ORDER BY position, id LIMIT 1
	`, planetID).Scan(&entryID, &cost.Metal, &cost.Crystal, &cost.Deuterium)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("economy repository: read waiting queue: %w", err)
	}
	configured, rulesetVersion, err := activeRuleset(ctx, tx)
	if err != nil {
		return err
	}
	levels, err := loadLevels(ctx, tx, planetID, catalogue)
	if err != nil {
		return err
	}
	duration, err := catalogue.Duration(cost, levels[building.RoboticsFactory], levels[building.NaniteFactory], configured.Time.BuildingSpeed)
	if err != nil {
		return err
	}
	startedAt := now.UTC().Truncate(time.Second)
	completesAt := startedAt.Add(duration)
	result, err := tx.ExecContext(ctx, `
		UPDATE building_queue SET state = 'active', started_at = ?, completes_at = ?, ruleset_version = ?
		WHERE id = ? AND state = 'queued'
	`, timestamp(startedAt), timestamp(completesAt), rulesetVersion, entryID)
	if err != nil {
		return fmt.Errorf("economy repository: promote building: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("economy repository: promote building: %w", err)
	}
	if affected != 1 {
		return errors.New("economy repository: the waiting construction changed during promotion")
	}
	return scheduleBuilding(ctx, tx, entryID, rulesetVersion, startedAt, completesAt)
}

func loadPlanet(ctx context.Context, tx *sql.Tx, accountID, requestedPlanetID int64, now time.Time, catalogue building.Catalogue) (appeconomy.Planet, int64, economy.ProductionState, error) {
	condition := "pl.account_id = ?"
	arguments := []any{accountID}
	if requestedPlanetID > 0 {
		condition += " AND p.id = ?"
		arguments = append(arguments, requestedPlanetID)
	}
	planet, version, state, err := scanAndSettlePlanet(ctx, tx, condition, arguments, now, catalogue)
	if errors.Is(err, appeconomy.ErrNoEmpire) && requestedPlanetID > 0 {
		var hasEmpire bool
		if scanErr := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM players WHERE account_id = ?)", accountID).Scan(&hasEmpire); scanErr != nil {
			return appeconomy.Planet{}, 0, economy.ProductionState{}, fmt.Errorf("economy repository: inspect empire: %w", scanErr)
		}
		if hasEmpire {
			return appeconomy.Planet{}, 0, economy.ProductionState{}, appeconomy.ErrPlanetNotFound
		}
	}
	return planet, version, state, err
}

func loadPlanetByID(ctx context.Context, tx *sql.Tx, planetID int64, now time.Time, catalogue building.Catalogue) (appeconomy.Planet, int64, economy.ProductionState, error) {
	return scanAndSettlePlanet(ctx, tx, "p.id = ?", []any{planetID}, now, catalogue)
}

func scanAndSettlePlanet(ctx context.Context, tx *sql.Tx, condition string, arguments []any, now time.Time, catalogue building.Catalogue) (appeconomy.Planet, int64, economy.ProductionState, error) {
	configured, rulesetVersion, err := activeRuleset(ctx, tx)
	if err != nil {
		return appeconomy.Planet{}, 0, economy.ProductionState{}, err
	}
	query := `SELECT p.id, p.kind, p.parent_planet_id, p.name, pl.display_name, p.galaxy, p.system, p.position, p.total_fields, p.used_fields, p.minimum_temperature, p.maximum_temperature, r.metal, r.crystal, r.deuterium, r.produced_at FROM planets p JOIN players pl ON pl.id = p.owner_player_id JOIN planet_resources r ON r.planet_id = p.id WHERE ` + condition + ` ORDER BY p.id LIMIT 1`
	var planet appeconomy.Planet
	var producedText, kind string
	var parentID sql.NullInt64
	err = tx.QueryRowContext(ctx, query, arguments...).Scan(&planet.ID, &kind, &parentID, &planet.Name, &planet.PlayerName, &planet.Coordinate.Galaxy, &planet.Coordinate.System, &planet.Coordinate.Position, &planet.TotalFields, &planet.UsedFields, &planet.MinimumTemperature, &planet.MaximumTemperature, &planet.Stock.Metal, &planet.Stock.Crystal, &planet.Stock.Deuterium, &producedText)
	if errors.Is(err, sql.ErrNoRows) {
		return appeconomy.Planet{}, 0, economy.ProductionState{}, appeconomy.ErrNoEmpire
	}
	if err != nil {
		return appeconomy.Planet{}, 0, economy.ProductionState{}, fmt.Errorf("economy repository: read planet: %w", err)
	}
	// Read remainders separately to keep the main projection scan explicit.
	var remainders economy.Remainders
	if err := tx.QueryRowContext(ctx, "SELECT metal_remainder, crystal_remainder, deuterium_remainder FROM planet_resources WHERE planet_id = ?", planet.ID).Scan(&remainders.Metal, &remainders.Crystal, &remainders.Deuterium); err != nil {
		return appeconomy.Planet{}, 0, economy.ProductionState{}, err
	}
	producedAt, err := time.Parse(time.RFC3339Nano, producedText)
	if err != nil {
		return appeconomy.Planet{}, 0, economy.ProductionState{}, fmt.Errorf("economy repository: parse production time: %w", err)
	}
	planet.Levels, err = loadLevels(ctx, tx, planet.ID, catalogue)
	if err != nil {
		return appeconomy.Planet{}, 0, economy.ProductionState{}, err
	}
	planet.Researches, err = loadResearchLevels(ctx, tx, planet.ID)
	if err != nil {
		return appeconomy.Planet{}, 0, economy.ProductionState{}, err
	}
	planet.Units, err = loadUnits(ctx, tx, planet.ID)
	if err != nil {
		return appeconomy.Planet{}, 0, economy.ProductionState{}, err
	}
	planet.Kind = building.Placement(kind)
	planet.ParentID = parentID.Int64
	planet.Rules = configured
	if err := enrichEconomy(&planet); err != nil {
		return appeconomy.Planet{}, 0, economy.ProductionState{}, err
	}
	state, err := economy.Settle(economy.ProductionState{Stock: planet.Stock, Remainder: remainders, ProducedAt: producedAt}, now, planet.Rates, planet.Capacity)
	if err != nil {
		return appeconomy.Planet{}, 0, economy.ProductionState{}, err
	}
	planet.Stock = state.Stock
	// Units finished since the last settlement are delivered after the resources
	// of the elapsed interval have been produced, so satellites delivered during
	// that interval only count from the next one.
	if err := settleProduction(ctx, tx, planet.ID, now); err != nil {
		return appeconomy.Planet{}, 0, economy.ProductionState{}, err
	}
	planet.Units, err = loadUnits(ctx, tx, planet.ID)
	if err != nil {
		return appeconomy.Planet{}, 0, economy.ProductionState{}, err
	}
	if err := enrichEconomy(&planet); err != nil {
		return appeconomy.Planet{}, 0, economy.ProductionState{}, err
	}
	planet.Queue, err = planetQueue(ctx, tx, planet.ID)
	if err != nil {
		return appeconomy.Planet{}, 0, economy.ProductionState{}, fmt.Errorf("economy repository: read construction queue: %w", err)
	}
	estimateQueue(planet.Queue, planet.Levels, configured, catalogue, now)
	return planet, rulesetVersion, state, nil
}

func activeRuleset(ctx context.Context, tx *sql.Tx) (rules.Ruleset, int64, error) {
	var document string
	var version int64
	if err := tx.QueryRowContext(ctx, "SELECT document, version FROM ruleset_versions WHERE status = 'active' ORDER BY version DESC LIMIT 1").Scan(&document, &version); err != nil {
		return rules.Ruleset{}, 0, fmt.Errorf("economy repository: active ruleset: %w", err)
	}
	configured, err := rules.Decode([]byte(document))
	if err != nil {
		return rules.Ruleset{}, 0, err
	}
	return configured, version, nil
}

// activeRulesetFrom reads the active ruleset outside any transaction, for the
// read-only projections.
func activeRulesetFrom(ctx context.Context, database *sql.DB) (rules.Ruleset, error) {
	var document string
	if err := database.QueryRowContext(ctx,
		"SELECT document FROM ruleset_versions WHERE status = 'active' ORDER BY version DESC LIMIT 1").Scan(&document); err != nil {
		return rules.Ruleset{}, fmt.Errorf("economy repository: active ruleset: %w", err)
	}
	return rules.Decode([]byte(document))
}

func loadLevels(ctx context.Context, tx *sql.Tx, planetID int64, catalogue building.Catalogue) (building.Levels, error) {
	levels := building.Levels{}
	known := map[building.ID]bool{}
	for _, definition := range catalogue.Definitions() {
		known[definition.ID] = true
	}
	rows, err := tx.QueryContext(ctx, "SELECT building_id, level FROM planet_buildings WHERE planet_id = ?", planetID)
	if err != nil {
		return nil, fmt.Errorf("economy repository: read levels: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id building.ID
		var level int
		if err := rows.Scan(&id, &level); err != nil {
			return nil, fmt.Errorf("economy repository: scan level: %w", err)
		}
		if !known[id] {
			return nil, fmt.Errorf("economy repository: unknown persisted building %q", id)
		}
		levels[id] = level
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("economy repository: iterate levels: %w", err)
	}
	return levels, nil
}

// facilityIsIdle refuses to upgrade a facility that another queue is counting
// on: the laboratory while a research is running or waiting, the shipyard and
// the nanite factory while a production order is running or waiting. Looking at
// whole queues rather than at the running entry alone keeps the exclusion true
// at every instant, without a queue ever having to stall.
func facilityIsIdle(ctx context.Context, tx *sql.Tx, planetID int64, id building.ID) error {
	var busy bool
	switch id {
	case building.ResearchLab:
		if err := tx.QueryRowContext(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM research_queue q
				JOIN planets p ON p.owner_player_id = q.player_id
				WHERE p.id = ? AND q.state IN ('active', 'queued')
			)
		`, planetID).Scan(&busy); err != nil {
			return fmt.Errorf("economy repository: inspect research queue: %w", err)
		}
	case building.Shipyard, building.NaniteFactory:
		if err := tx.QueryRowContext(ctx,
			"SELECT EXISTS(SELECT 1 FROM production_orders WHERE planet_id = ? AND state IN ('active', 'queued'))",
			planetID).Scan(&busy); err != nil {
			return fmt.Errorf("economy repository: inspect production orders: %w", err)
		}
	default:
		return nil
	}
	if busy {
		return appeconomy.ErrFacilityBusy
	}
	return nil
}

// playerOfPlanet returns the player owning a planet.
func playerOfPlanet(ctx context.Context, tx *sql.Tx, planetID int64) (int64, error) {
	var playerID int64
	if err := tx.QueryRowContext(ctx, "SELECT owner_player_id FROM planets WHERE id = ?", planetID).Scan(&playerID); err != nil {
		return 0, fmt.Errorf("economy repository: read planet owner: %w", err)
	}
	return playerID, nil
}

// loadResearchLevels reads the technology levels of the player owning a planet.
func loadResearchLevels(ctx context.Context, tx *sql.Tx, planetID int64) (research.Levels, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT r.research_id, r.level
		FROM player_research r
		JOIN planets p ON p.owner_player_id = r.player_id
		WHERE p.id = ?
	`, planetID)
	if err != nil {
		return nil, fmt.Errorf("economy repository: read research levels: %w", err)
	}
	defer rows.Close()
	levels := research.Levels{}
	for rows.Next() {
		var id string
		var level int
		if err := rows.Scan(&id, &level); err != nil {
			return nil, fmt.Errorf("economy repository: scan research level: %w", err)
		}
		levels[research.ID(id)] = level
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("economy repository: iterate research levels: %w", err)
	}
	return levels, nil
}

// loadUnits reads the units stationed on a planet.
func loadUnits(ctx context.Context, tx *sql.Tx, planetID int64) (unit.Inventory, error) {
	rows, err := tx.QueryContext(ctx, "SELECT unit_id, quantity FROM planet_units WHERE planet_id = ?", planetID)
	if err != nil {
		return nil, fmt.Errorf("economy repository: read units: %w", err)
	}
	defer rows.Close()
	inventory := unit.Inventory{}
	for rows.Next() {
		var id string
		var quantity int64
		if err := rows.Scan(&id, &quantity); err != nil {
			return nil, fmt.Errorf("economy repository: scan unit: %w", err)
		}
		inventory[unit.ID(id)] = quantity
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("economy repository: iterate units: %w", err)
	}
	return inventory, nil
}

func enrichEconomy(planet *appeconomy.Planet) error {
	if planet.Kind == building.OnMoon {
		// A moon produces nothing and has no storage building, so nothing caps
		// what it holds: whatever is landed there stays there.
		planet.Rates = economy.Rates{}
		planet.Energy = economy.Energy{}
		planet.Capacity = economy.Resources{Metal: math.MaxInt64, Crystal: math.MaxInt64, Deuterium: math.MaxInt64}
		return nil
	}
	levels := economy.Levels{
		MetalMine: planet.Levels[building.MetalMine], CrystalMine: planet.Levels[building.CrystalMine],
		DeuteriumSynthesizer: planet.Levels[building.DeuteriumSynthesizer], SolarPlant: planet.Levels[building.SolarPlant],
		MetalStorage: planet.Levels[building.MetalStorage], CrystalStorage: planet.Levels[building.CrystalStorage], DeuteriumTank: planet.Levels[building.DeuteriumTank],
		SolarSatellites: int(planet.Units[unit.SolarSatellite]),
	}
	var err error
	planet.Rates, planet.Energy, err = economy.CalculateRates(planet.Rules, levels, planet.MaximumTemperature)
	if err != nil {
		return err
	}
	planet.Capacity.Metal, err = economy.Capacity(planet.Rules.Economy.BaseStorage, levels.MetalStorage)
	if err != nil {
		return err
	}
	planet.Capacity.Crystal, err = economy.Capacity(planet.Rules.Economy.BaseStorage, levels.CrystalStorage)
	if err != nil {
		return err
	}
	planet.Capacity.Deuterium, err = economy.Capacity(planet.Rules.Economy.BaseStorage, levels.DeuteriumTank)
	return err
}

func persistProduction(ctx context.Context, tx *sql.Tx, planetID int64, state economy.ProductionState) error {
	_, err := tx.ExecContext(ctx, `UPDATE planet_resources SET metal = ?, crystal = ?, deuterium = ?, metal_remainder = ?, crystal_remainder = ?, deuterium_remainder = ?, produced_at = ?, version = version + 1 WHERE planet_id = ?`, state.Stock.Metal, state.Stock.Crystal, state.Stock.Deuterium, state.Remainder.Metal, state.Remainder.Crystal, state.Remainder.Deuterium, timestamp(state.ProducedAt), planetID)
	if err != nil {
		return fmt.Errorf("economy repository: persist production: %w", err)
	}
	return nil
}

// planetQueue reads every construction ordered on a body and not yet finished,
// head first. Only the head carries a schedule.
func planetQueue(ctx context.Context, tx *sql.Tx, planetID int64) ([]appeconomy.Queue, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, building_id, target_level, metal_cost, crystal_cost, deuterium_cost, position, started_at, completes_at, state
		FROM building_queue WHERE planet_id = ? AND state IN ('active', 'queued')
		ORDER BY position, id
	`, planetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []appeconomy.Queue
	for rows.Next() {
		var entry appeconomy.Queue
		var buildingID string
		var startedText, completesText sql.NullString
		if err := rows.Scan(&entry.ID, &buildingID, &entry.TargetLevel, &entry.Cost.Metal, &entry.Cost.Crystal, &entry.Cost.Deuterium, &entry.Position, &startedText, &completesText, &entry.State); err != nil {
			return nil, err
		}
		entry.Building = building.ID(buildingID)
		if startedText.Valid {
			if entry.StartedAt, err = time.Parse(time.RFC3339Nano, startedText.String); err != nil {
				return nil, err
			}
			if entry.CompletesAt, err = time.Parse(time.RFC3339Nano, completesText.String); err != nil {
				return nil, err
			}
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

// projectedLevels applies the whole queue to the built levels, so that a second
// order for the same building targets the level after the first.
func projectedLevels(planet appeconomy.Planet) building.Levels {
	projected := building.Levels{}
	for id, level := range planet.Levels {
		projected[id] = level
	}
	for _, entry := range planet.Queue {
		projected[entry.Building] = entry.TargetLevel
	}
	return projected
}

// estimateQueue dates the entries that are still waiting, by walking the queue
// with the factory levels each of them will find when its turn comes. These
// dates are a forecast shown to the player and are never persisted.
func estimateQueue(entries []appeconomy.Queue, levels building.Levels, configured rules.Ruleset, catalogue building.Catalogue, now time.Time) {
	cursor := now
	projected := building.Levels{}
	for id, level := range levels {
		projected[id] = level
	}
	for index, entry := range entries {
		if !entry.Waiting() {
			if entry.CompletesAt.After(cursor) {
				cursor = entry.CompletesAt
			}
			projected[entry.Building] = entry.TargetLevel
			continue
		}
		duration, err := catalogue.Duration(entry.Cost, projected[building.RoboticsFactory], projected[building.NaniteFactory], configured.Time.BuildingSpeed)
		if err != nil {
			return
		}
		entries[index].EstimatedStartAt = cursor
		cursor = cursor.Add(duration)
		entries[index].EstimatedCompletesAt = cursor
		projected[entry.Building] = entry.TargetLevel
	}
}

func loadQueue(ctx context.Context, tx *sql.Tx, queueID, accountID int64) (appeconomy.Queue, error) {
	var queue appeconomy.Queue
	var buildingID string
	var startedText, completesText sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT q.id, q.building_id, q.target_level, q.metal_cost, q.crystal_cost, q.deuterium_cost, q.position, q.started_at, q.completes_at, q.state FROM building_queue q JOIN planets p ON p.id = q.planet_id JOIN players pl ON pl.id = p.owner_player_id WHERE q.id = ? AND pl.account_id = ?`, queueID, accountID).Scan(&queue.ID, &buildingID, &queue.TargetLevel, &queue.Cost.Metal, &queue.Cost.Crystal, &queue.Cost.Deuterium, &queue.Position, &startedText, &completesText, &queue.State)
	if err != nil {
		return appeconomy.Queue{}, err
	}
	queue.Building = building.ID(buildingID)
	if !startedText.Valid {
		return queue, nil
	}
	if queue.StartedAt, err = time.Parse(time.RFC3339Nano, startedText.String); err != nil {
		return appeconomy.Queue{}, err
	}
	queue.CompletesAt, err = time.Parse(time.RFC3339Nano, completesText.String)
	return queue, err
}
