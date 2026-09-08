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
	appshipyard "universeatwar/internal/app/shipyard"
	"universeatwar/internal/domain/building"
	"universeatwar/internal/domain/catalogue"
	domaineconomy "universeatwar/internal/domain/economy"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/prerequisite"
	"universeatwar/internal/domain/rules"
	"universeatwar/internal/domain/unit"
)

// productionEventPriority orders production completion against the other events
// of the same deadline, as documented in the scheduled events architecture.
const productionEventPriority = 70

// ShipyardRepository persists each production action in one write transaction.
type ShipyardRepository struct {
	write      *sql.DB
	catalogues catalogue.Set
}

func NewShipyardRepository(write *sql.DB, catalogues catalogue.Set) *ShipyardRepository {
	return &ShipyardRepository{write: write, catalogues: catalogues}
}

// RegisterHandlers plugs production completion into the shared event processor.
func (r *ShipyardRepository) RegisterHandlers(processor *EventProcessor) {
	processor.Register("production_completed", func(ctx context.Context, tx *sql.Tx, event ScheduledEvent, now time.Time) error {
		return completeProduction(ctx, tx, event, now, r.catalogues)
	})
}

// State reads the production situation of one planet.
func (r *ShipyardRepository) State(ctx context.Context, accountID, planetID int64, now time.Time) (appshipyard.State, error) {
	var state appshipyard.State
	err := withWriteTx(ctx, r.write, "shipyard repository: state", func(tx *sql.Tx) error {
		planet, _, production, err := loadPlanet(ctx, tx, accountID, planetID, now, r.catalogues.Buildings)
		if err != nil {
			return err
		}
		if err := persistProduction(ctx, tx, planet.ID, production); err != nil {
			return err
		}
		ships, err := planetProductionQueue(ctx, tx, planet.ID, unit.Ship)
		if err != nil {
			return err
		}
		defenses, err := planetProductionQueue(ctx, tx, planet.ID, unit.Defense)
		if err != nil {
			return err
		}
		estimateProductionQueue(ships, planet, r.catalogues.Units, now)
		estimateProductionQueue(defenses, planet, r.catalogues.Units, now)
		ordered, err := orderedUnits(ctx, tx, planet.ID)
		if err != nil {
			return err
		}
		state = appshipyard.State{
			Planet:    planet,
			Inventory: planet.Units,
			SiloUsed:  siloSlotsUsed(planet.Units, append(append([]appshipyard.Order{}, ships...), defenses...), r.catalogues.Units),
			Ships:     ships,
			Defenses:  defenses,
			Ordered:   ordered,
		}
		return nil
	})
	if err != nil {
		return appshipyard.State{}, err
	}
	return state, nil
}

// Order debits the total cost and appends the batch to the queue of its family.
// Only the batch that lands at the head is scheduled; the ones behind it wait
// with no deadline of their own.
func (r *ShipyardRepository) Order(ctx context.Context, accountID, planetID int64, id unit.ID, quantity int64, idempotencyKey string, now time.Time) (appshipyard.Order, error) {
	var order appshipyard.Order
	err := withWriteTx(ctx, r.write, "shipyard repository: order", func(tx *sql.Tx) error {
		digest := sha256.Sum256([]byte(fmt.Sprintf("%d:%s:%d", planetID, id, quantity)))
		requestHash := hex.EncodeToString(digest[:])
		replayed, found, err := replayedProduction(ctx, tx, accountID, idempotencyKey, requestHash)
		if err != nil {
			return err
		}
		if found {
			order = replayed
			return nil
		}

		planet, rulesetVersion, production, err := loadPlanet(ctx, tx, accountID, planetID, now, r.catalogues.Buildings)
		if err != nil {
			return err
		}
		if err := r.catalogues.Matches(planet.Rules); err != nil {
			return err
		}
		definition, known := r.catalogues.Units.Definition(id)
		if !known {
			return unit.ErrUnknownUnit
		}
		queue, err := planetProductionQueue(ctx, tx, planet.ID, definition.Family)
		if err != nil {
			return err
		}
		if len(queue) >= planet.Rules.Progression.QueueLength {
			return appshipyard.ErrQueueFull
		}
		if err := shipyardIsIdle(ctx, tx, planet.ID); err != nil {
			return err
		}
		// Units still owed by the whole yard count against the limits a model
		// carries, so a second shield dome cannot slip in behind the first.
		pending, err := pendingUnits(ctx, tx, planet.ID)
		if err != nil {
			return err
		}
		plan, err := r.catalogues.Units.Order(id, quantity, unit.Inputs{
			State: prerequisite.State{
				Buildings:  planet.Levels.Generic(),
				Researches: planet.Researches.Generic(),
			},
			ShipyardLevel:    planet.Levels[building.Shipyard],
			NaniteLevel:      planet.Levels[building.NaniteFactory],
			MissileSiloLevel: planet.Levels[building.MissileSilo],
			Inventory:        planet.Units,
			Pending:          pending,
		}, planet.Rules)
		if err != nil {
			return err
		}
		production.Stock, err = production.Stock.Debit(plan.TotalCost)
		if err != nil {
			return err
		}
		if err := persistProduction(ctx, tx, planet.ID, production); err != nil {
			return err
		}
		queuedAt := now.UTC().Truncate(time.Second)
		unitSeconds := int64(plan.UnitDuration / time.Second)
		position := 0
		if last := len(queue); last > 0 {
			position = queue[last-1].Position + 1
		}
		head := len(queue) == 0
		orderState := "queued"
		var startedAt, completesAt time.Time
		var startedValue, completesValue any
		if head {
			orderState = "active"
			startedAt = queuedAt
			completesAt = startedAt.Add(plan.Duration)
			startedValue, completesValue = timestamp(startedAt), timestamp(completesAt)
		}
		result, err := tx.ExecContext(ctx, `
			INSERT INTO production_orders(planet_id, unit_id, family, quantity, unit_metal_cost, unit_crystal_cost, unit_deuterium_cost, unit_seconds, ruleset_version, position, queued_at, started_at, completes_at, state)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, planet.ID, string(id), string(plan.Family), plan.Quantity, plan.UnitCost.Metal, plan.UnitCost.Crystal,
			plan.UnitCost.Deuterium, unitSeconds, rulesetVersion, position, timestamp(queuedAt), startedValue, completesValue, orderState)
		if err != nil {
			if strings.Contains(err.Error(), "production_orders_one_active_idx") || strings.Contains(err.Error(), "production_orders_position_idx") {
				return appshipyard.ErrQueueBusy
			}
			return fmt.Errorf("shipyard repository: enqueue production: %w", err)
		}
		orderID, err := result.LastInsertId()
		if err != nil {
			return fmt.Errorf("shipyard repository: order id: %w", err)
		}
		if head {
			if err := scheduleProduction(ctx, tx, orderID, rulesetVersion, startedAt, completesAt); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO idempotency_keys(actor_id, operation, key, request_hash, result_type, result_id, created_at)
			VALUES (?, 'order_units', ?, ?, 'production_orders', ?, ?)
		`, strconv.FormatInt(accountID, 10), idempotencyKey, requestHash, strconv.FormatInt(orderID, 10), timestamp(queuedAt)); err != nil {
			return fmt.Errorf("shipyard repository: record idempotency: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO game_event_log(event_type, actor_type, actor_id, entity_type, entity_id, occurred_at, payload)
			VALUES ('production_ordered', 'account', ?, 'planet', ?, ?, json_object('unit_id', ?, 'quantity', ?, 'order_id', ?, 'position', ?))
		`, accountID, planet.ID, timestamp(queuedAt), string(id), plan.Quantity, orderID, position); err != nil {
			return fmt.Errorf("shipyard repository: log production order: %w", err)
		}
		order = appshipyard.Order{
			ID: orderID, PlanetID: planet.ID, Unit: id, Family: plan.Family, Quantity: plan.Quantity,
			UnitCost: plan.UnitCost, TotalCost: plan.TotalCost, UnitDuration: plan.UnitDuration,
			Position: position, StartedAt: startedAt, CompletesAt: completesAt, State: orderState,
		}
		return nil
	})
	if err != nil {
		return appshipyard.Order{}, err
	}
	return order, nil
}

// settleProduction delivers the units the running batches have finished. It
// runs in every transaction that reads or spends units, exactly like lazy
// production, and covers both families because they build side by side.
func settleProduction(ctx context.Context, tx *sql.Tx, planetID int64, now time.Time) error {
	running, err := activeProductions(ctx, tx, planetID)
	if err != nil {
		return err
	}
	for _, order := range running {
		if err := settleOneProduction(ctx, tx, planetID, order, now); err != nil {
			return err
		}
	}
	return nil
}

func settleOneProduction(ctx context.Context, tx *sql.Tx, planetID int64, order appshipyard.Order, now time.Time) error {
	produced := unit.Delivered(order.StartedAt, order.UnitDuration, order.Quantity, now)
	if produced <= order.Delivered {
		return nil
	}
	delivery := produced - order.Delivered
	if err := adjustInventory(ctx, tx, planetID, order.Unit, delivery); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx,
		"UPDATE production_orders SET delivered = ? WHERE id = ? AND delivered = ? AND state = 'active'",
		produced, order.ID, order.Delivered)
	if err != nil {
		return fmt.Errorf("shipyard repository: record delivery: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("shipyard repository: record delivery: %w", err)
	}
	if affected != 1 {
		return errors.New("shipyard repository: production order changed during delivery")
	}
	return nil
}

// completeProduction delivers the remainder of a batch, closes it exactly once
// and hands the yard to whatever the player queued behind it.
func completeProduction(ctx context.Context, tx *sql.Tx, event ScheduledEvent, now time.Time, catalogues catalogue.Set) error {
	orderID, err := strconv.ParseInt(event.EntityID, 10, 64)
	if err != nil {
		return errors.New("shipyard repository: invalid order event reference")
	}
	var planetID int64
	var state, family string
	if err := tx.QueryRowContext(ctx, "SELECT planet_id, state, family FROM production_orders WHERE id = ?", orderID).
		Scan(&planetID, &state, &family); err != nil {
		return fmt.Errorf("shipyard repository: read due order: %w", err)
	}
	if state != "active" {
		return nil
	}
	if err := settleProduction(ctx, tx, planetID, event.DueAt); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE production_orders SET state = 'completed', completed_at = ?
		WHERE id = ? AND state = 'active' AND delivered = quantity
	`, timestamp(now), orderID)
	if err != nil {
		return fmt.Errorf("shipyard repository: complete order: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("shipyard repository: complete order: %w", err)
	}
	if affected != 1 {
		return errors.New("shipyard repository: production order is not fully delivered")
	}
	var unitID string
	var quantity int64
	if err := tx.QueryRowContext(ctx, "SELECT unit_id, quantity FROM production_orders WHERE id = ?", orderID).
		Scan(&unitID, &quantity); err != nil {
		return fmt.Errorf("shipyard repository: read completed order: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO game_event_log(event_type, entity_type, entity_id, occurred_at, payload)
		VALUES ('production_completed', 'planet', ?, ?, json_object('unit_id', ?, 'quantity', ?, 'order_id', ?))
	`, planetID, timestamp(now), unitID, quantity, orderID); err != nil {
		return fmt.Errorf("shipyard repository: log completion: %w", err)
	}
	// The queue must not gain idle time from a late settlement, so the next
	// batch starts at the instant this one was due.
	return promoteNextProduction(ctx, tx, planetID, unit.Family(family), event.DueAt, catalogues)
}

// CancelOrder drops one batch and refunds the units the yard still owed. The
// ones already delivered stay with the planet, and the batch behind takes over.
func (r *ShipyardRepository) CancelOrder(ctx context.Context, accountID, planetID, orderID int64, now time.Time) (appeconomy.Cancellation, error) {
	var cancellation appeconomy.Cancellation
	err := withWriteTx(ctx, r.write, "shipyard repository: cancel order", func(tx *sql.Tx) error {
		planet, _, production, err := loadPlanet(ctx, tx, accountID, planetID, now, r.catalogues.Buildings)
		if err != nil {
			return err
		}
		order, err := loadProductionOrder(ctx, tx, "id = ? AND planet_id = ?", orderID, planet.ID)
		if errors.Is(err, sql.ErrNoRows) {
			return appshipyard.ErrQueueEntryNotFound
		}
		if err != nil {
			return err
		}
		if order.State != "active" && order.State != "queued" {
			return appshipyard.ErrQueueEntryNotFound
		}
		if err := cancelQueueEntry(ctx, tx, "production_orders", order.ID, fmt.Sprintf("production-complete:%d", order.ID), now); err != nil {
			return err
		}
		owed := order.Quantity - order.Delivered
		refund := economyTimes(order.UnitCost, owed)
		var lost domaineconomy.Resources
		production.Stock, lost = production.Stock.Refund(refund, planet.Capacity)
		if err := persistProduction(ctx, tx, planet.ID, production); err != nil {
			return err
		}
		if order.State == "active" {
			if err := promoteNextProduction(ctx, tx, planet.ID, order.Family, now, r.catalogues); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO game_event_log(event_type, actor_type, actor_id, entity_type, entity_id, occurred_at, payload)
			VALUES ('production_cancelled', 'account', ?, 'planet', ?, ?, json_object('order_id', ?, 'unit_id', ?, 'refunded_units', ?))
		`, accountID, planet.ID, timestamp(now), order.ID, string(order.Unit), owed); err != nil {
			return fmt.Errorf("shipyard repository: log cancellation: %w", err)
		}
		cancellation = appeconomy.Cancellation{
			Cancelled: 1,
			Refunded:  domaineconomy.Resources{Metal: refund.Metal - lost.Metal, Crystal: refund.Crystal - lost.Crystal, Deuterium: refund.Deuterium - lost.Deuterium},
			Lost:      lost,
		}
		return nil
	})
	if err != nil {
		return appeconomy.Cancellation{}, err
	}
	return cancellation, nil
}

// scheduleProduction books the completion of the batch now at the head.
func scheduleProduction(ctx context.Context, tx *sql.Tx, orderID, rulesetVersion int64, startedAt, completesAt time.Time) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO scheduled_events(event_type, due_at, priority, entity_type, entity_id, ruleset_version, payload, idempotency_key, created_at)
		VALUES ('production_completed', ?, ?, 'production_orders', ?, ?, json_object('order_id', ?), ?, ?)
	`, timestamp(completesAt), productionEventPriority, strconv.FormatInt(orderID, 10), rulesetVersion, orderID,
		fmt.Sprintf("production-complete:%d", orderID), timestamp(startedAt)); err != nil {
		return fmt.Errorf("shipyard repository: schedule production: %w", err)
	}
	return nil
}

// promoteNextProduction starts whichever batch now waits at the front of a
// family's queue, timing it with the yard the planet owns at this instant.
func promoteNextProduction(ctx context.Context, tx *sql.Tx, planetID int64, family unit.Family, now time.Time, catalogues catalogue.Set) error {
	var orderID, quantity int64
	var unitID string
	var unitCost domaineconomy.Resources
	err := tx.QueryRowContext(ctx, `
		SELECT id, unit_id, quantity, unit_metal_cost, unit_crystal_cost, unit_deuterium_cost
		FROM production_orders
		WHERE planet_id = ? AND family = ? AND state = 'queued' ORDER BY position, id LIMIT 1
	`, planetID, string(family)).Scan(&orderID, &unitID, &quantity, &unitCost.Metal, &unitCost.Crystal, &unitCost.Deuterium)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("shipyard repository: read waiting batch: %w", err)
	}
	configured, rulesetVersion, err := activeRuleset(ctx, tx)
	if err != nil {
		return err
	}
	levels, err := loadLevels(ctx, tx, planetID, catalogues.Buildings)
	if err != nil {
		return err
	}
	unitDuration, err := catalogues.Units.UnitDuration(unitCost,
		levels[building.Shipyard], levels[building.NaniteFactory], familySpeed(family, configured))
	if err != nil {
		return err
	}
	startedAt := now.UTC().Truncate(time.Second)
	completesAt := startedAt.Add(unitDuration * time.Duration(quantity))
	result, err := tx.ExecContext(ctx, `
		UPDATE production_orders SET state = 'active', started_at = ?, completes_at = ?, unit_seconds = ?, ruleset_version = ?
		WHERE id = ? AND state = 'queued'
	`, timestamp(startedAt), timestamp(completesAt), int64(unitDuration/time.Second), rulesetVersion, orderID)
	if err != nil {
		return fmt.Errorf("shipyard repository: promote batch: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("shipyard repository: promote batch: %w", err)
	}
	if affected != 1 {
		return errors.New("shipyard repository: the waiting batch changed during promotion")
	}
	return scheduleProduction(ctx, tx, orderID, rulesetVersion, startedAt, completesAt)
}

// familySpeed is the ruleset speed that applies to one family.
func familySpeed(family unit.Family, configured rules.Ruleset) float64 {
	if family == unit.Defense {
		return configured.Time.DefenseSpeed
	}
	return configured.Time.ShipyardSpeed
}

// adjustInventory adds or removes units. A removal is a guarded update: it only
// applies when the planet really owns the units, so no inventory can ever go
// negative, even under concurrency.
func adjustInventory(ctx context.Context, tx *sql.Tx, planetID int64, id unit.ID, delta int64) error {
	if delta == 0 {
		return nil
	}
	if delta > 0 {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO planet_units(planet_id, unit_id, quantity) VALUES (?, ?, ?)
			ON CONFLICT(planet_id, unit_id) DO UPDATE SET quantity = quantity + excluded.quantity
		`, planetID, string(id), delta); err != nil {
			return fmt.Errorf("shipyard repository: adjust inventory: %w", err)
		}
		return nil
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE planet_units SET quantity = quantity + ? WHERE planet_id = ? AND unit_id = ? AND quantity >= ?
	`, delta, planetID, string(id), -delta)
	if err != nil {
		return fmt.Errorf("shipyard repository: adjust inventory: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("shipyard repository: adjust inventory: %w", err)
	}
	if affected != 1 {
		return domainfleet.ErrInsufficientUnits
	}
	return nil
}

// economyTimes multiplies a unit cost by a quantity for display; the authority
// on overflow stays the domain, which refused the order otherwise.
func economyTimes(cost domaineconomy.Resources, quantity int64) domaineconomy.Resources {
	return domaineconomy.Resources{Metal: cost.Metal * quantity, Crystal: cost.Crystal * quantity, Deuterium: cost.Deuterium * quantity}
}

const productionColumns = `id, planet_id, unit_id, family, quantity, delivered, unit_metal_cost, unit_crystal_cost, unit_deuterium_cost, unit_seconds, position, started_at, completes_at, state`

// activeProductions returns the batches of a planet that are being built, one
// per family at most.
func activeProductions(ctx context.Context, tx *sql.Tx, planetID int64) ([]appshipyard.Order, error) {
	rows, err := tx.QueryContext(ctx,
		"SELECT "+productionColumns+" FROM production_orders WHERE planet_id = ? AND state = 'active' ORDER BY family, id", planetID)
	if err != nil {
		return nil, fmt.Errorf("shipyard repository: read running batches: %w", err)
	}
	return scanProductionOrders(rows)
}

// planetProductionQueue reads one family's queue, head first. Only the head
// carries a schedule.
func planetProductionQueue(ctx context.Context, tx *sql.Tx, planetID int64, family unit.Family) ([]appshipyard.Order, error) {
	rows, err := tx.QueryContext(ctx,
		"SELECT "+productionColumns+" FROM production_orders WHERE planet_id = ? AND family = ? AND state IN ('active', 'queued') ORDER BY position, id",
		planetID, string(family))
	if err != nil {
		return nil, fmt.Errorf("shipyard repository: read production queue: %w", err)
	}
	return scanProductionOrders(rows)
}

// pendingUnits counts the units the whole yard still owes the planet.
func pendingUnits(ctx context.Context, tx *sql.Tx, planetID int64) (unit.Inventory, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT unit_id, SUM(quantity - delivered) FROM production_orders
		WHERE planet_id = ? AND state IN ('active', 'queued') GROUP BY unit_id
	`, planetID)
	if err != nil {
		return nil, fmt.Errorf("shipyard repository: read pending units: %w", err)
	}
	defer rows.Close()
	pending := unit.Inventory{}
	for rows.Next() {
		var id string
		var owed int64
		if err := rows.Scan(&id, &owed); err != nil {
			return nil, fmt.Errorf("shipyard repository: scan pending units: %w", err)
		}
		pending[unit.ID(id)] = owed
	}
	return pending, rows.Err()
}

// orderedUnits counts every unit of each model the planet has ever ordered,
// finished and cancelled included. It only grows, so a page can build an
// idempotency key from it that no live order already holds.
func orderedUnits(ctx context.Context, tx *sql.Tx, planetID int64) (unit.Inventory, error) {
	rows, err := tx.QueryContext(ctx,
		"SELECT unit_id, SUM(quantity) FROM production_orders WHERE planet_id = ? GROUP BY unit_id", planetID)
	if err != nil {
		return nil, fmt.Errorf("shipyard repository: read ordered units: %w", err)
	}
	defer rows.Close()
	ordered := unit.Inventory{}
	for rows.Next() {
		var id string
		var total int64
		if err := rows.Scan(&id, &total); err != nil {
			return nil, fmt.Errorf("shipyard repository: scan ordered units: %w", err)
		}
		ordered[unit.ID(id)] = total
	}
	return ordered, rows.Err()
}

// estimateProductionQueue dates the batches still waiting, at the pace the yard
// runs today.
func estimateProductionQueue(orders []appshipyard.Order, planet appeconomy.Planet, catalogue unit.Catalogue, now time.Time) {
	cursor := now
	for index, order := range orders {
		if !order.Waiting() {
			if order.CompletesAt.After(cursor) {
				cursor = order.CompletesAt
			}
			continue
		}
		unitDuration, err := catalogue.UnitDuration(order.UnitCost,
			planet.Levels[building.Shipyard], planet.Levels[building.NaniteFactory], familySpeed(order.Family, planet.Rules))
		if err != nil {
			return
		}
		orders[index].EstimatedStartAt = cursor
		cursor = cursor.Add(unitDuration * time.Duration(order.Quantity))
		orders[index].EstimatedCompletesAt = cursor
	}
}

func scanProductionOrders(rows *sql.Rows) ([]appshipyard.Order, error) {
	defer rows.Close()
	var orders []appshipyard.Order
	for rows.Next() {
		order, err := scanProductionOrder(rows)
		if err != nil {
			return nil, err
		}
		orders = append(orders, order)
	}
	return orders, rows.Err()
}

type productionScanner interface {
	Scan(destination ...any) error
}

func scanProductionOrder(row productionScanner) (appshipyard.Order, error) {
	var order appshipyard.Order
	var unitID, family string
	var startedText, completesText sql.NullString
	var unitSeconds int64
	if err := row.Scan(&order.ID, &order.PlanetID, &unitID, &family, &order.Quantity, &order.Delivered,
		&order.UnitCost.Metal, &order.UnitCost.Crystal, &order.UnitCost.Deuterium, &unitSeconds,
		&order.Position, &startedText, &completesText, &order.State); err != nil {
		return appshipyard.Order{}, err
	}
	order.Unit = unit.ID(unitID)
	order.Family = unit.Family(family)
	order.UnitDuration = time.Duration(unitSeconds) * time.Second
	order.TotalCost = economyTimes(order.UnitCost, order.Quantity)
	if !startedText.Valid {
		return order, nil
	}
	var err error
	if order.StartedAt, err = time.Parse(time.RFC3339Nano, startedText.String); err != nil {
		return appshipyard.Order{}, fmt.Errorf("shipyard repository: parse start time: %w", err)
	}
	if order.CompletesAt, err = time.Parse(time.RFC3339Nano, completesText.String); err != nil {
		return appshipyard.Order{}, fmt.Errorf("shipyard repository: parse completion time: %w", err)
	}
	return order, nil
}

func loadProductionOrder(ctx context.Context, tx *sql.Tx, condition string, arguments ...any) (*appshipyard.Order, error) {
	row := tx.QueryRowContext(ctx, "SELECT "+productionColumns+" FROM production_orders WHERE "+condition, arguments...)
	order, err := scanProductionOrder(row)
	if err != nil {
		return nil, err
	}
	return &order, nil
}

// replayedProduction returns the order a previous identical request created.
func replayedProduction(ctx context.Context, tx *sql.Tx, accountID int64, idempotencyKey, requestHash string) (appshipyard.Order, bool, error) {
	var storedHash, resultID string
	err := tx.QueryRowContext(ctx, `
		SELECT request_hash, result_id FROM idempotency_keys
		WHERE actor_id = ? AND operation = 'order_units' AND key = ?
	`, strconv.FormatInt(accountID, 10), idempotencyKey).Scan(&storedHash, &resultID)
	if errors.Is(err, sql.ErrNoRows) {
		return appshipyard.Order{}, false, nil
	}
	if err != nil {
		return appshipyard.Order{}, false, fmt.Errorf("shipyard repository: inspect idempotency: %w", err)
	}
	if storedHash != requestHash {
		return appshipyard.Order{}, false, appshipyard.ErrInvalidRequest
	}
	orderID, err := strconv.ParseInt(resultID, 10, 64)
	if err != nil {
		return appshipyard.Order{}, false, errors.New("shipyard repository: corrupt idempotency result")
	}
	order, err := loadProductionOrder(ctx, tx, "id = ?", orderID)
	if err != nil {
		return appshipyard.Order{}, false, err
	}
	return *order, true, nil
}

// shipyardIsIdle refuses a production while the shipyard or the nanite factory
// sits anywhere in the building queue, running or waiting. Looking at the whole
// queue keeps the exclusion true at every instant.
func shipyardIsIdle(ctx context.Context, tx *sql.Tx, planetID int64) error {
	var busy bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM building_queue
			WHERE planet_id = ? AND state IN ('active', 'queued') AND building_id IN ('shipyard', 'nanite_factory')
		)
	`, planetID).Scan(&busy); err != nil {
		return fmt.Errorf("shipyard repository: inspect shipyard: %w", err)
	}
	if busy {
		return appshipyard.ErrFacilityBusy
	}
	return nil
}

// siloSlotsUsed counts the missile slots already taken, everything the queues
// still owe included.
func siloSlotsUsed(inventory unit.Inventory, queued []appshipyard.Order, catalogue unit.Catalogue) int {
	used := 0
	for id, quantity := range inventory {
		if definition, known := catalogue.Definition(id); known {
			used += definition.SiloSlots * int(quantity)
		}
	}
	for _, order := range queued {
		if definition, known := catalogue.Definition(order.Unit); known {
			used += definition.SiloSlots * int(order.Quantity-order.Delivered)
		}
	}
	return used
}
