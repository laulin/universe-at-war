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
	appjumpgate "universeatwar/internal/app/jumpgate"
	"universeatwar/internal/domain/building"
	"universeatwar/internal/domain/catalogue"
	domainfleet "universeatwar/internal/domain/fleet"
)

// JumpGateRepository moves ships between two moons in one transaction.
type JumpGateRepository struct {
	write      *sql.DB
	catalogues catalogue.Set
}

func NewJumpGateRepository(write *sql.DB, catalogues catalogue.Set) *JumpGateRepository {
	return &JumpGateRepository{write: write, catalogues: catalogues}
}

// Overview reads the gate of one moon and every moon it may reach.
func (r *JumpGateRepository) Overview(ctx context.Context, accountID, moonID int64, now time.Time) (appjumpgate.Overview, error) {
	var overview appjumpgate.Overview
	err := withWriteTx(ctx, r.write, "jump gate repository: overview", func(tx *sql.Tx) error {
		moon, _, production, err := loadPlanet(ctx, tx, accountID, moonID, now, r.catalogues.Buildings)
		if err != nil {
			return err
		}
		if moon.Kind != building.OnMoon {
			return appjumpgate.ErrNotAMoon
		}
		if err := persistProduction(ctx, tx, moon.ID, production); err != nil {
			return err
		}
		readyAt, err := gateReadyAt(ctx, tx, moon.ID)
		if err != nil {
			return err
		}
		destinations, err := reachableGates(ctx, tx, accountID, moon.ID, now)
		if err != nil {
			return err
		}
		overview = appjumpgate.Overview{
			Moon: moon, Level: moon.Levels[building.JumpGate], ReadyAt: readyAt,
			Ready: !readyAt.After(now), Stationed: moon.Units, Destinations: destinations,
		}
		return nil
	})
	if err != nil {
		return appjumpgate.Overview{}, err
	}
	return overview, nil
}

// Jump moves the ships and starts the cooldown of both gates.
func (r *JumpGateRepository) Jump(ctx context.Context, accountID, fromMoonID, toMoonID int64,
	composition domainfleet.Composition, idempotencyKey string, now time.Time) (appjumpgate.Transfer, error) {
	var transfer appjumpgate.Transfer
	err := withWriteTx(ctx, r.write, "jump gate repository: jump", func(tx *sql.Tx) error {
		digest := sha256.Sum256([]byte(jumpSignature(fromMoonID, toMoonID, composition)))
		requestHash := hex.EncodeToString(digest[:])
		stored, found, err := replayedJump(ctx, tx, accountID, idempotencyKey, requestHash)
		if err != nil {
			return err
		}
		if found {
			transfer = stored
			return nil
		}

		source, err := loadMoon(ctx, tx, accountID, fromMoonID, now, r.catalogues)
		if err != nil {
			return err
		}
		destination, err := loadMoon(ctx, tx, accountID, toMoonID, now, r.catalogues)
		if err != nil {
			return err
		}
		if source.Levels[building.JumpGate] <= 0 || destination.Levels[building.JumpGate] <= 0 {
			return appjumpgate.ErrNoGate
		}
		cooldown := time.Duration(source.Rules.Expansion.JumpGateCooldownSeconds) * time.Second
		for _, moon := range []appeconomy.Planet{source, destination} {
			readyAt, err := gateReadyAt(ctx, tx, moon.ID)
			if err != nil {
				return err
			}
			if readyAt.After(now) {
				return appjumpgate.ErrCoolingDown
			}
		}
		if err := composition.Validate(r.catalogues.Units); err != nil {
			return err
		}
		for id, quantity := range composition {
			if quantity <= 0 {
				continue
			}
			if source.Units[id] < quantity {
				return domainfleet.ErrInsufficientUnits
			}
			if err := adjustInventory(ctx, tx, source.ID, id, -quantity); err != nil {
				return err
			}
			if err := adjustInventory(ctx, tx, destination.ID, id, quantity); err != nil {
				return err
			}
		}
		readyAt := now.Add(cooldown)
		for _, moon := range []appeconomy.Planet{source, destination} {
			if err := startCooldown(ctx, tx, moon.ID, readyAt, now); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO idempotency_keys(actor_id, operation, key, request_hash, result_type, result_id, created_at)
			VALUES (?, 'jump_gate', ?, ?, 'planets', ?, ?)
		`, strconv.FormatInt(accountID, 10), idempotencyKey, requestHash, strconv.FormatInt(destination.ID, 10), timestamp(now)); err != nil {
			return fmt.Errorf("jump gate repository: record idempotency: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO game_event_log(event_type, actor_type, actor_id, entity_type, entity_id, occurred_at, payload)
			VALUES ('jump_gate_used', 'account', ?, 'planet', ?, ?, json_object('to_moon_id', ?, 'ships', ?, 'ready_at', ?))
		`, accountID, source.ID, timestamp(now), destination.ID, composition.Count(), timestamp(readyAt)); err != nil {
			return fmt.Errorf("jump gate repository: log jump: %w", err)
		}
		transfer = appjumpgate.Transfer{
			FromMoonID: source.ID, ToMoonID: destination.ID, Composition: composition, ReadyAt: readyAt,
		}
		return nil
	})
	if err != nil {
		return appjumpgate.Transfer{}, err
	}
	return transfer, nil
}

// loadMoon reads a moon of the account, refusing a planet.
func loadMoon(ctx context.Context, tx *sql.Tx, accountID, moonID int64, now time.Time, catalogues catalogue.Set) (appeconomy.Planet, error) {
	moon, _, production, err := loadPlanet(ctx, tx, accountID, moonID, now, catalogues.Buildings)
	if err != nil {
		return appeconomy.Planet{}, err
	}
	if moon.Kind != building.OnMoon {
		return appeconomy.Planet{}, appjumpgate.ErrNotAMoon
	}
	if err := persistProduction(ctx, tx, moon.ID, production); err != nil {
		return appeconomy.Planet{}, err
	}
	return moon, nil
}

// gateReadyAt is when the gate of a moon may fire again.
func gateReadyAt(ctx context.Context, tx *sql.Tx, moonID int64) (time.Time, error) {
	var value string
	err := tx.QueryRowContext(ctx, "SELECT ready_at FROM jump_gates WHERE moon_id = ?", moonID).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("jump gate repository: read cooldown: %w", err)
	}
	readyAt, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("jump gate repository: parse cooldown: %w", err)
	}
	return readyAt, nil
}

// startCooldown makes a gate unusable until the given instant.
func startCooldown(ctx context.Context, tx *sql.Tx, moonID int64, readyAt, now time.Time) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO jump_gates(moon_id, ready_at, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(moon_id) DO UPDATE SET ready_at = excluded.ready_at, updated_at = excluded.updated_at
	`, moonID, timestamp(readyAt), timestamp(now)); err != nil {
		return fmt.Errorf("jump gate repository: start cooldown: %w", err)
	}
	return nil
}

// reachableGates lists the other moons of the account that carry a gate.
func reachableGates(ctx context.Context, tx *sql.Tx, accountID, moonID int64, now time.Time) ([]appjumpgate.Gate, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT p.id, p.name, p.galaxy, p.system, p.position, b.level, COALESCE(g.ready_at, '')
		FROM planets p
		JOIN players pl ON pl.id = p.owner_player_id
		JOIN planet_buildings b ON b.planet_id = p.id AND b.building_id = 'jump_gate'
		LEFT JOIN jump_gates g ON g.moon_id = p.id
		WHERE pl.account_id = ? AND p.kind = 'moon' AND p.id <> ? AND b.level > 0
		ORDER BY p.id
	`, accountID, moonID)
	if err != nil {
		return nil, fmt.Errorf("jump gate repository: read destinations: %w", err)
	}
	defer rows.Close()
	var gates []appjumpgate.Gate
	for rows.Next() {
		var gate appjumpgate.Gate
		var readyText string
		if err := rows.Scan(&gate.MoonID, &gate.Name, &gate.Coordinate.Galaxy, &gate.Coordinate.System,
			&gate.Coordinate.Position, &gate.Level, &readyText); err != nil {
			return nil, fmt.Errorf("jump gate repository: scan destination: %w", err)
		}
		if readyText != "" {
			parsed, parseErr := time.Parse(time.RFC3339Nano, readyText)
			if parseErr != nil {
				return nil, fmt.Errorf("jump gate repository: parse destination cooldown: %w", parseErr)
			}
			gate.ReadyAt = parsed
		}
		gate.Ready = !gate.ReadyAt.After(now)
		gates = append(gates, gate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("jump gate repository: iterate destinations: %w", err)
	}
	return gates, nil
}

// replayedJump returns the transfer a previous identical request performed.
func replayedJump(ctx context.Context, tx *sql.Tx, accountID int64, idempotencyKey, requestHash string) (appjumpgate.Transfer, bool, error) {
	var storedHash, resultID string
	err := tx.QueryRowContext(ctx, `
		SELECT request_hash, result_id FROM idempotency_keys
		WHERE actor_id = ? AND operation = 'jump_gate' AND key = ?
	`, strconv.FormatInt(accountID, 10), idempotencyKey).Scan(&storedHash, &resultID)
	if errors.Is(err, sql.ErrNoRows) {
		return appjumpgate.Transfer{}, false, nil
	}
	if err != nil {
		return appjumpgate.Transfer{}, false, fmt.Errorf("jump gate repository: inspect idempotency: %w", err)
	}
	if storedHash != requestHash {
		return appjumpgate.Transfer{}, false, appjumpgate.ErrInvalidRequest
	}
	moonID, err := strconv.ParseInt(resultID, 10, 64)
	if err != nil {
		return appjumpgate.Transfer{}, false, errors.New("jump gate repository: corrupt idempotency result")
	}
	readyAt, err := gateReadyAt(ctx, tx, moonID)
	if err != nil {
		return appjumpgate.Transfer{}, false, err
	}
	return appjumpgate.Transfer{ToMoonID: moonID, ReadyAt: readyAt}, true, nil
}

// jumpSignature identifies one exact jump, so a replayed request never moves a
// second batch of ships.
func jumpSignature(fromMoonID, toMoonID int64, composition domainfleet.Composition) string {
	signature := fmt.Sprintf("%d:%d", fromMoonID, toMoonID)
	for _, id := range sortedComposition(composition) {
		signature += fmt.Sprintf(":%s=%d", id, composition[id])
	}
	return signature
}
