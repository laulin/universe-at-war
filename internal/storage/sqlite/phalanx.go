package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	appphalanx "universeatwar/internal/app/phalanx"
	"universeatwar/internal/domain/building"
	"universeatwar/internal/domain/catalogue"
	"universeatwar/internal/domain/economy"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/phalanx"
	"universeatwar/internal/domain/universe"
)

// PhalanxRepository performs a sweep and its payment in one transaction.
type PhalanxRepository struct {
	write      *sql.DB
	catalogues catalogue.Set
}

func NewPhalanxRepository(write *sql.DB, catalogues catalogue.Set) *PhalanxRepository {
	return &PhalanxRepository{write: write, catalogues: catalogues}
}

// Scan charges the sweep and returns only what a phalanx may observe.
func (r *PhalanxRepository) Scan(ctx context.Context, accountID, moonID int64, target universe.Coordinate, now time.Time) (appphalanx.Scan, error) {
	var scan appphalanx.Scan
	err := withWriteTx(ctx, r.write, "phalanx repository: scan", func(tx *sql.Tx) error {
		moon, _, production, err := loadPlanet(ctx, tx, accountID, moonID, now, r.catalogues.Buildings)
		if err != nil {
			return err
		}
		if moon.Kind != building.OnMoon {
			return appphalanx.ErrNotAMoon
		}
		level := moon.Levels[building.SensorPhalanx]
		if err := phalanx.InRange(moon.Coordinate, target, level, moon.Rules.Topology); err != nil {
			return err
		}
		cost := economy.Resources{Deuterium: moon.Rules.Expansion.PhalanxScanCost}
		production.Stock, err = production.Stock.Debit(cost)
		if err != nil {
			return err
		}
		if err := persistProduction(ctx, tx, moon.ID, production); err != nil {
			return err
		}
		sightings, err := observableMissions(ctx, tx, target)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO game_event_log(event_type, entity_type, entity_id, occurred_at, payload)
			VALUES ('phalanx_scanned', 'planet', ?, ?, json_object('target', ?, 'sightings', ?))
		`, moon.ID, timestamp(now), target.String(), len(sightings)); err != nil {
			return fmt.Errorf("phalanx repository: log scan: %w", err)
		}
		moon.Stock = production.Stock
		scan = appphalanx.Scan{
			Moon: moon, Level: level, Radius: phalanx.Radius(level),
			Cost: cost.Deuterium, Target: target, Sightings: sightings,
		}
		return nil
	})
	if err != nil {
		return appphalanx.Scan{}, err
	}
	return scan, nil
}

// observableMissions lists the flights a sensor can time. Espionage stays
// invisible: probes are too small to move a mass detector.
func observableMissions(ctx context.Context, tx *sql.Tx, target universe.Coordinate) ([]appphalanx.Sighting, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT f.mission, f.origin_galaxy, f.origin_system, f.origin_position,
			f.target_galaxy, f.target_system, f.target_position, f.arrives_at, f.returns_at,
			COALESCE((SELECT SUM(s.quantity) FROM fleet_ships s WHERE s.fleet_id = f.id), 0)
		FROM fleets f
		WHERE f.state IN ('outbound', 'returning', 'recalled')
		  AND f.mission <> 'espionage'
		  AND (
			(f.target_galaxy = ? AND f.target_system = ? AND f.target_position = ?)
			OR (f.origin_galaxy = ? AND f.origin_system = ? AND f.origin_position = ?)
		  )
		ORDER BY f.arrives_at, f.id
	`, target.Galaxy, target.System, target.Position, target.Galaxy, target.System, target.Position)
	if err != nil {
		return nil, fmt.Errorf("phalanx repository: read missions: %w", err)
	}
	defer rows.Close()
	var sightings []appphalanx.Sighting
	for rows.Next() {
		var sighting appphalanx.Sighting
		var mission, arrivesText string
		var returnsText sql.NullString
		if err := rows.Scan(&mission, &sighting.Origin.Galaxy, &sighting.Origin.System, &sighting.Origin.Position,
			&sighting.Target.Galaxy, &sighting.Target.System, &sighting.Target.Position,
			&arrivesText, &returnsText, &sighting.Ships); err != nil {
			return nil, fmt.Errorf("phalanx repository: scan mission: %w", err)
		}
		sighting.Mission = domainfleet.Mission(mission)
		sighting.ArrivesAt, err = time.Parse(time.RFC3339Nano, arrivesText)
		if err != nil {
			return nil, fmt.Errorf("phalanx repository: parse arrival: %w", err)
		}
		if returnsText.Valid {
			parsed, parseErr := time.Parse(time.RFC3339Nano, returnsText.String)
			if parseErr != nil {
				return nil, fmt.Errorf("phalanx repository: parse return: %w", parseErr)
			}
			sighting.ReturnsAt = &parsed
		}
		sightings = append(sightings, sighting)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("phalanx repository: iterate missions: %w", err)
	}
	return sightings, nil
}
