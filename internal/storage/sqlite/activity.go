package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	appactivity "universeatwar/internal/app/activity"
)

// ActivityRepository reads the queues and movements that feed the permanent
// body column. It owns no state and therefore needs no migration.
type ActivityRepository struct {
	database *sql.DB
}

func NewActivityRepository(database *sql.DB) *ActivityRepository {
	return &ActivityRepository{database: database}
}

// Snapshot reads counters and attacks in one transaction so the warning count
// cannot disagree with the approaches listed alongside it.
func (r *ActivityRepository) Snapshot(ctx context.Context, accountID int64) (appactivity.Snapshot, error) {
	transaction, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return appactivity.Snapshot{}, fmt.Errorf("activity repository: begin snapshot: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	playerID, err := playerByAccount(ctx, transaction, accountID)
	if err != nil {
		return appactivity.Snapshot{}, err
	}
	snapshot := appactivity.Snapshot{Bodies: map[int64]appactivity.Body{}}
	rows, err := transaction.QueryContext(ctx, `
		SELECT p.id,
			(SELECT COUNT(*) FROM building_queue b
			 WHERE b.planet_id = p.id AND b.state IN ('active', 'queued')),
			(SELECT COUNT(*) FROM research_queue r
			 WHERE r.planet_id = p.id AND r.state IN ('active', 'queued')),
			COALESCE((SELECT SUM(o.quantity - o.delivered) FROM production_orders o
			 WHERE o.planet_id = p.id AND o.family = 'ship' AND o.state IN ('active', 'queued')), 0),
			COALESCE((SELECT SUM(o.quantity - o.delivered) FROM production_orders o
			 WHERE o.planet_id = p.id AND o.family = 'defense' AND o.state IN ('active', 'queued')), 0),
			(SELECT COUNT(*) FROM fleets f
			 WHERE f.owner_player_id = ? AND f.origin_planet_id = p.id
			 AND f.state IN ('outbound', 'holding', 'returning', 'recalled'))
		FROM planets p WHERE p.owner_player_id = ? ORDER BY p.id
	`, playerID, playerID)
	if err != nil {
		return appactivity.Snapshot{}, fmt.Errorf("activity repository: read body counters: %w", err)
	}
	for rows.Next() {
		var body appactivity.Body
		if err := rows.Scan(&body.PlanetID, &body.Buildings, &body.Researches, &body.Ships,
			&body.Defenses, &body.OutboundFleets); err != nil {
			_ = rows.Close()
			return appactivity.Snapshot{}, fmt.Errorf("activity repository: scan body counters: %w", err)
		}
		snapshot.Bodies[body.PlanetID] = body
	}
	if err := rows.Close(); err != nil {
		return appactivity.Snapshot{}, fmt.Errorf("activity repository: close body counters: %w", err)
	}
	if err := rows.Err(); err != nil {
		return appactivity.Snapshot{}, fmt.Errorf("activity repository: iterate body counters: %w", err)
	}

	incoming, err := incomingAttacks(ctx, transaction, playerID)
	if err != nil {
		return appactivity.Snapshot{}, err
	}
	snapshot.Incoming = incoming
	for _, approach := range incoming {
		body := snapshot.Bodies[approach.TargetPlanetID]
		body.IncomingAttacks++
		snapshot.Bodies[approach.TargetPlanetID] = body
	}
	if err := transaction.Commit(); err != nil {
		return appactivity.Snapshot{}, fmt.Errorf("activity repository: commit snapshot: %w", err)
	}
	return snapshot, nil
}

// incomingAttacks exposes only outbound attacks whose current target belongs
// to the account. Espionage and traffic aimed at another player stay private.
func incomingAttacks(ctx context.Context, transaction *sql.Tx, playerID int64) ([]appactivity.Approach, error) {
	rows, err := transaction.QueryContext(ctx, `
		SELECT f.id, target.id, attacker.display_name,
			f.origin_galaxy, f.origin_system, f.origin_position,
			f.target_galaxy, f.target_system, f.target_position, f.arrives_at
		FROM fleets f
		JOIN players attacker ON attacker.id = f.owner_player_id
		JOIN planets target ON target.id = f.target_planet_id
		WHERE target.owner_player_id = ? AND f.owner_player_id <> ?
			AND f.mission = 'attack' AND f.state = 'outbound'
		ORDER BY f.arrives_at, f.id
	`, playerID, playerID)
	if err != nil {
		return nil, fmt.Errorf("activity repository: list incoming attacks: %w", err)
	}
	type row struct {
		approach appactivity.Approach
		arrival  string
	}
	var pending []row
	for rows.Next() {
		var item row
		if err := rows.Scan(&item.approach.FleetID, &item.approach.TargetPlanetID,
			&item.approach.AttackerName,
			&item.approach.Origin.Galaxy, &item.approach.Origin.System, &item.approach.Origin.Position,
			&item.approach.Target.Galaxy, &item.approach.Target.System, &item.approach.Target.Position,
			&item.arrival); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("activity repository: scan incoming attack: %w", err)
		}
		pending = append(pending, item)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("activity repository: close incoming attacks: %w", err)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("activity repository: iterate incoming attacks: %w", err)
	}

	approaches := make([]appactivity.Approach, 0, len(pending))
	for _, item := range pending {
		item.approach.ArrivesAt, err = time.Parse(time.RFC3339Nano, item.arrival)
		if err != nil {
			return nil, fmt.Errorf("activity repository: parse attack arrival: %w", err)
		}
		composition, err := loadComposition(ctx, transaction, item.approach.FleetID)
		if err != nil {
			return nil, err
		}
		item.approach.Composition = composition
		approaches = append(approaches, item.approach)
	}
	return approaches, nil
}
