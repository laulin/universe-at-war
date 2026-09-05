package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	appeconomy "universeatwar/internal/app/economy"
	"universeatwar/internal/domain/building"
	"universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/rules"
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
		ID: planetID, Name: "Planète mère", PlayerName: name, Coordinate: coordinate,
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

func (r *EconomyRepository) StartConstruction(ctx context.Context, accountID, planetID int64, id building.ID, idempotencyKey string, now time.Time, catalogue building.Catalogue) (appeconomy.Queue, error) {
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
	if planet.ActiveQueue != nil {
		return appeconomy.Queue{}, appeconomy.ErrQueueBusy
	}
	plan, err := catalogue.Plan(id, planet.Levels, planet.UsedFields, planet.TotalFields, planet.Rules)
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
	startedAt := now.UTC().Truncate(time.Second)
	completesAt := startedAt.Add(plan.Duration)
	result, err := tx.ExecContext(ctx, `
		INSERT INTO building_queue(planet_id, building_id, target_level, metal_cost, crystal_cost, deuterium_cost, ruleset_version, started_at, completes_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, planet.ID, string(id), plan.TargetLevel, plan.Cost.Metal, plan.Cost.Crystal, plan.Cost.Deuterium, rulesetVersion, timestamp(startedAt), timestamp(completesAt))
	if err != nil {
		if strings.Contains(err.Error(), "building_queue_one_active_idx") || strings.Contains(err.Error(), "UNIQUE") {
			return appeconomy.Queue{}, appeconomy.ErrQueueBusy
		}
		return appeconomy.Queue{}, fmt.Errorf("economy repository: enqueue building: %w", err)
	}
	queueID, err := result.LastInsertId()
	if err != nil {
		return appeconomy.Queue{}, fmt.Errorf("economy repository: queue id: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO scheduled_events(event_type, due_at, priority, entity_type, entity_id, ruleset_version, payload, idempotency_key, created_at)
		VALUES ('building_completed', ?, 50, 'building_queue', ?, ?, json_object('queue_id', ?), ?, ?)
	`, timestamp(completesAt), strconv.FormatInt(queueID, 10), rulesetVersion, queueID, fmt.Sprintf("building-complete:%d", queueID), timestamp(startedAt)); err != nil {
		return appeconomy.Queue{}, fmt.Errorf("economy repository: schedule building: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO idempotency_keys(actor_id, operation, key, request_hash, result_type, result_id, created_at) VALUES (?, 'start_building', ?, ?, 'building_queue', ?, ?)`, strconv.FormatInt(accountID, 10), idempotencyKey, requestHash, strconv.FormatInt(queueID, 10), timestamp(startedAt)); err != nil {
		return appeconomy.Queue{}, fmt.Errorf("economy repository: record idempotency: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO game_event_log(event_type, actor_type, actor_id, entity_type, entity_id, occurred_at, payload) VALUES ('building_started', 'account', ?, 'planet', ?, ?, json_object('building_id', ?, 'target_level', ?, 'queue_id', ?))`, accountID, planet.ID, timestamp(startedAt), string(id), plan.TargetLevel, queueID); err != nil {
		return appeconomy.Queue{}, fmt.Errorf("economy repository: log building start: %w", err)
	}
	queue := appeconomy.Queue{ID: queueID, Building: id, TargetLevel: plan.TargetLevel, Cost: plan.Cost, StartedAt: startedAt, CompletesAt: completesAt, State: "active"}
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
	if building.ID(buildingID) == building.Terraformer {
		extraFields = 5
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
	return nil
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
	query := `SELECT p.id, p.name, pl.display_name, p.galaxy, p.system, p.position, p.total_fields, p.used_fields, p.minimum_temperature, p.maximum_temperature, r.metal, r.crystal, r.deuterium, r.metal_remainder, r.crystal_remainder, r.deuterium_remainder, r.produced_at FROM planets p JOIN players pl ON pl.id = p.owner_player_id JOIN planet_resources r ON r.planet_id = p.id WHERE ` + condition + ` ORDER BY p.id LIMIT 1`
	var planet appeconomy.Planet
	var producedText string
	err = tx.QueryRowContext(ctx, query, arguments...).Scan(&planet.ID, &planet.Name, &planet.PlayerName, &planet.Coordinate.Galaxy, &planet.Coordinate.System, &planet.Coordinate.Position, &planet.TotalFields, &planet.UsedFields, &planet.MinimumTemperature, &planet.MaximumTemperature, &planet.Stock.Metal, &planet.Stock.Crystal, &planet.Stock.Deuterium, new(int64), new(int64), new(int64), &producedText)
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
	planet.Rules = configured
	if err := enrichEconomy(&planet); err != nil {
		return appeconomy.Planet{}, 0, economy.ProductionState{}, err
	}
	state, err := economy.Settle(economy.ProductionState{Stock: planet.Stock, Remainder: remainders, ProducedAt: producedAt}, now, planet.Rates, planet.Capacity)
	if err != nil {
		return appeconomy.Planet{}, 0, economy.ProductionState{}, err
	}
	planet.Stock = state.Stock
	planet.ActiveQueue, err = activeQueue(ctx, tx, planet.ID)
	if err != nil {
		return appeconomy.Planet{}, 0, economy.ProductionState{}, fmt.Errorf("economy repository: read active queue: %w", err)
	}
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

func enrichEconomy(planet *appeconomy.Planet) error {
	levels := economy.Levels{
		MetalMine: planet.Levels[building.MetalMine], CrystalMine: planet.Levels[building.CrystalMine],
		DeuteriumSynthesizer: planet.Levels[building.DeuteriumSynthesizer], SolarPlant: planet.Levels[building.SolarPlant],
		MetalStorage: planet.Levels[building.MetalStorage], CrystalStorage: planet.Levels[building.CrystalStorage], DeuteriumTank: planet.Levels[building.DeuteriumTank],
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

func activeQueue(ctx context.Context, tx *sql.Tx, planetID int64) (*appeconomy.Queue, error) {
	var queue appeconomy.Queue
	var buildingID, startedText, completesText string
	err := tx.QueryRowContext(ctx, `SELECT id, building_id, target_level, metal_cost, crystal_cost, deuterium_cost, started_at, completes_at, state FROM building_queue WHERE planet_id = ? AND state = 'active'`, planetID).Scan(&queue.ID, &buildingID, &queue.TargetLevel, &queue.Cost.Metal, &queue.Cost.Crystal, &queue.Cost.Deuterium, &startedText, &completesText, &queue.State)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	queue.Building = building.ID(buildingID)
	queue.StartedAt, err = time.Parse(time.RFC3339Nano, startedText)
	if err != nil {
		return nil, err
	}
	queue.CompletesAt, err = time.Parse(time.RFC3339Nano, completesText)
	return &queue, err
}

func loadQueue(ctx context.Context, tx *sql.Tx, queueID, accountID int64) (appeconomy.Queue, error) {
	var queue appeconomy.Queue
	var buildingID, startedText, completesText string
	err := tx.QueryRowContext(ctx, `SELECT q.id, q.building_id, q.target_level, q.metal_cost, q.crystal_cost, q.deuterium_cost, q.started_at, q.completes_at, q.state FROM building_queue q JOIN planets p ON p.id = q.planet_id JOIN players pl ON pl.id = p.owner_player_id WHERE q.id = ? AND pl.account_id = ?`, queueID, accountID).Scan(&queue.ID, &buildingID, &queue.TargetLevel, &queue.Cost.Metal, &queue.Cost.Crystal, &queue.Cost.Deuterium, &startedText, &completesText, &queue.State)
	if err != nil {
		return appeconomy.Queue{}, err
	}
	queue.Building = building.ID(buildingID)
	queue.StartedAt, err = time.Parse(time.RFC3339Nano, startedText)
	if err != nil {
		return appeconomy.Queue{}, err
	}
	queue.CompletesAt, err = time.Parse(time.RFC3339Nano, completesText)
	return queue, err
}
