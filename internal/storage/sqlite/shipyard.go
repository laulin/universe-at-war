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

	appshipyard "universeatwar/internal/app/shipyard"
	"universeatwar/internal/domain/building"
	"universeatwar/internal/domain/catalogue"
	domaineconomy "universeatwar/internal/domain/economy"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/prerequisite"
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
		return completeProduction(ctx, tx, event, now)
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
		active, err := activeProduction(ctx, tx, planet.ID)
		if err != nil {
			return err
		}
		state = appshipyard.State{
			Planet:    planet,
			Inventory: planet.Units,
			SiloUsed:  siloSlotsUsed(planet.Units, active, r.catalogues.Units),
			Active:    active,
		}
		return nil
	})
	if err != nil {
		return appshipyard.State{}, err
	}
	return state, nil
}

// Order debits the total cost, creates the order, its completion event, its
// idempotency key and its journal entry in a single transaction.
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
		active, err := activeProduction(ctx, tx, planet.ID)
		if err != nil {
			return err
		}
		if active != nil {
			return appshipyard.ErrQueueBusy
		}
		if err := shipyardIsIdle(ctx, tx, planet.ID); err != nil {
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
			Pending:          unit.Inventory{},
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
		startedAt := now.UTC().Truncate(time.Second)
		completesAt := startedAt.Add(plan.Duration)
		unitSeconds := int64(plan.UnitDuration / time.Second)
		result, err := tx.ExecContext(ctx, `
			INSERT INTO production_orders(planet_id, unit_id, family, quantity, unit_metal_cost, unit_crystal_cost, unit_deuterium_cost, unit_seconds, ruleset_version, started_at, completes_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, planet.ID, string(id), string(plan.Family), plan.Quantity, plan.UnitCost.Metal, plan.UnitCost.Crystal,
			plan.UnitCost.Deuterium, unitSeconds, rulesetVersion, timestamp(startedAt), timestamp(completesAt))
		if err != nil {
			if strings.Contains(err.Error(), "production_orders_one_active_idx") || strings.Contains(err.Error(), "UNIQUE") {
				return appshipyard.ErrQueueBusy
			}
			return fmt.Errorf("shipyard repository: enqueue production: %w", err)
		}
		orderID, err := result.LastInsertId()
		if err != nil {
			return fmt.Errorf("shipyard repository: order id: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO scheduled_events(event_type, due_at, priority, entity_type, entity_id, ruleset_version, payload, idempotency_key, created_at)
			VALUES ('production_completed', ?, ?, 'production_orders', ?, ?, json_object('order_id', ?), ?, ?)
		`, timestamp(completesAt), productionEventPriority, strconv.FormatInt(orderID, 10), rulesetVersion, orderID,
			fmt.Sprintf("production-complete:%d", orderID), timestamp(startedAt)); err != nil {
			return fmt.Errorf("shipyard repository: schedule production: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO idempotency_keys(actor_id, operation, key, request_hash, result_type, result_id, created_at)
			VALUES (?, 'order_units', ?, ?, 'production_orders', ?, ?)
		`, strconv.FormatInt(accountID, 10), idempotencyKey, requestHash, strconv.FormatInt(orderID, 10), timestamp(startedAt)); err != nil {
			return fmt.Errorf("shipyard repository: record idempotency: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO game_event_log(event_type, actor_type, actor_id, entity_type, entity_id, occurred_at, payload)
			VALUES ('production_ordered', 'account', ?, 'planet', ?, ?, json_object('unit_id', ?, 'quantity', ?, 'order_id', ?))
		`, accountID, planet.ID, timestamp(startedAt), string(id), plan.Quantity, orderID); err != nil {
			return fmt.Errorf("shipyard repository: log production order: %w", err)
		}
		order = appshipyard.Order{
			ID: orderID, PlanetID: planet.ID, Unit: id, Family: plan.Family, Quantity: plan.Quantity,
			UnitCost: plan.UnitCost, TotalCost: plan.TotalCost, UnitDuration: plan.UnitDuration,
			StartedAt: startedAt, CompletesAt: completesAt, State: "active",
		}
		return nil
	})
	if err != nil {
		return appshipyard.Order{}, err
	}
	return order, nil
}

// settleProduction delivers the units an active order has finished. It runs in
// every transaction that reads or spends units, exactly like lazy production.
func settleProduction(ctx context.Context, tx *sql.Tx, planetID int64, now time.Time) error {
	order, err := activeProduction(ctx, tx, planetID)
	if err != nil || order == nil {
		return err
	}
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

// completeProduction delivers the remainder of a batch and closes it exactly
// once.
func completeProduction(ctx context.Context, tx *sql.Tx, event ScheduledEvent, now time.Time) error {
	orderID, err := strconv.ParseInt(event.EntityID, 10, 64)
	if err != nil {
		return errors.New("shipyard repository: invalid order event reference")
	}
	var planetID int64
	var state string
	if err := tx.QueryRowContext(ctx, "SELECT planet_id, state FROM production_orders WHERE id = ?", orderID).
		Scan(&planetID, &state); err != nil {
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
	return nil
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

// activeProduction returns the running order of a planet, if any.
func activeProduction(ctx context.Context, tx *sql.Tx, planetID int64) (*appshipyard.Order, error) {
	order, err := loadProductionOrder(ctx, tx, "planet_id = ? AND state = 'active'", planetID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return order, err
}

func loadProductionOrder(ctx context.Context, tx *sql.Tx, condition string, argument any) (*appshipyard.Order, error) {
	var order appshipyard.Order
	var unitID, family, startedText, completesText string
	var unitSeconds int64
	err := tx.QueryRowContext(ctx, `
		SELECT id, planet_id, unit_id, family, quantity, delivered, unit_metal_cost, unit_crystal_cost, unit_deuterium_cost, unit_seconds, started_at, completes_at, state
		FROM production_orders WHERE `+condition, argument).
		Scan(&order.ID, &order.PlanetID, &unitID, &family, &order.Quantity, &order.Delivered, &order.UnitCost.Metal,
			&order.UnitCost.Crystal, &order.UnitCost.Deuterium, &unitSeconds, &startedText, &completesText, &order.State)
	if err != nil {
		return nil, err
	}
	order.Unit = unit.ID(unitID)
	order.Family = unit.Family(family)
	order.UnitDuration = time.Duration(unitSeconds) * time.Second
	order.TotalCost = economyTimes(order.UnitCost, order.Quantity)
	order.StartedAt, err = time.Parse(time.RFC3339Nano, startedText)
	if err != nil {
		return nil, fmt.Errorf("shipyard repository: parse start time: %w", err)
	}
	order.CompletesAt, err = time.Parse(time.RFC3339Nano, completesText)
	if err != nil {
		return nil, fmt.Errorf("shipyard repository: parse completion time: %w", err)
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
// is being upgraded.
func shipyardIsIdle(ctx context.Context, tx *sql.Tx, planetID int64) error {
	var busy bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM building_queue
			WHERE planet_id = ? AND state = 'active' AND building_id IN ('shipyard', 'nanite_factory')
		)
	`, planetID).Scan(&busy); err != nil {
		return fmt.Errorf("shipyard repository: inspect shipyard: %w", err)
	}
	if busy {
		return appshipyard.ErrFacilityBusy
	}
	return nil
}

// siloSlotsUsed counts the missile slots already taken, production included.
func siloSlotsUsed(inventory unit.Inventory, active *appshipyard.Order, catalogue unit.Catalogue) int {
	used := 0
	for id, quantity := range inventory {
		if definition, known := catalogue.Definition(id); known {
			used += definition.SiloSlots * int(quantity)
		}
	}
	if active != nil {
		if definition, known := catalogue.Definition(active.Unit); known {
			used += definition.SiloSlots * int(active.Quantity-active.Delivered)
		}
	}
	return used
}
