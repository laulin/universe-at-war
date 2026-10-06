package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	appranking "universeatwar/internal/app/ranking"
)

// RankingRepository reads only the public identity of every player and the
// assets needed to derive a score. Resources, cargo and queue contents stay
// private and do not contribute to the standings.
type RankingRepository struct {
	read *sql.DB
}

func NewRankingRepository(read *sql.DB) *RankingRepository {
	return &RankingRepository{read: read}
}

func (r *RankingRepository) Snapshot(ctx context.Context) (appranking.Snapshot, error) {
	configured, err := activeRulesetFrom(ctx, r.read)
	if err != nil {
		return appranking.Snapshot{}, err
	}
	rows, err := r.read.QueryContext(ctx, `
		SELECT pl.id, pl.account_id, pl.display_name, a.kind,
		       COALESCE(al.name, ''), COALESCE(al.tag, '')
		FROM players pl
		JOIN accounts a ON a.id = pl.account_id
		LEFT JOIN alliance_members am ON am.player_id = pl.id
		LEFT JOIN alliances al ON al.id = am.alliance_id
		ORDER BY pl.id
	`)
	if err != nil {
		return appranking.Snapshot{}, fmt.Errorf("ranking repository: read players: %w", err)
	}
	defer rows.Close()
	snapshot := appranking.Snapshot{Rules: configured}
	for rows.Next() {
		var player appranking.PlayerAssets
		var kind string
		if err := rows.Scan(&player.PlayerID, &player.AccountID, &player.Name, &kind, &player.Alliance, &player.AllianceTag); err != nil {
			return appranking.Snapshot{}, fmt.Errorf("ranking repository: scan player: %w", err)
		}
		player.Artificial = kind == "ai"
		snapshot.Players = append(snapshot.Players, player)
	}
	if err := rows.Err(); err != nil {
		return appranking.Snapshot{}, fmt.Errorf("ranking repository: iterate players: %w", err)
	}
	players := make(map[int64]*appranking.PlayerAssets, len(snapshot.Players))
	for index := range snapshot.Players {
		players[snapshot.Players[index].PlayerID] = &snapshot.Players[index]
	}
	if err := r.readLevels(ctx, players, `
		SELECT p.owner_player_id, b.building_id, b.level
		FROM planet_buildings b JOIN planets p ON p.id = b.planet_id
		WHERE b.level > 0 ORDER BY p.owner_player_id, p.id, b.building_id
	`, func(player *appranking.PlayerAssets, level appranking.Level) {
		player.Buildings = append(player.Buildings, level)
	}); err != nil {
		return appranking.Snapshot{}, err
	}
	if err := r.readLevels(ctx, players, `
		SELECT player_id, research_id, level FROM player_research
		WHERE level > 0 ORDER BY player_id, research_id
	`, func(player *appranking.PlayerAssets, level appranking.Level) {
		player.Research = append(player.Research, level)
	}); err != nil {
		return appranking.Snapshot{}, err
	}
	if err := r.readUnits(ctx, players); err != nil {
		return appranking.Snapshot{}, err
	}
	return snapshot, nil
}

func (r *RankingRepository) readLevels(ctx context.Context, players map[int64]*appranking.PlayerAssets,
	query string, assign func(*appranking.PlayerAssets, appranking.Level)) error {
	rows, err := r.read.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("ranking repository: read levels: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var playerID int64
		var level appranking.Level
		if err := rows.Scan(&playerID, &level.ID, &level.Level); err != nil {
			return fmt.Errorf("ranking repository: scan level: %w", err)
		}
		if player := players[playerID]; player != nil {
			assign(player, level)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("ranking repository: iterate levels: %w", err)
	}
	return nil
}

func (r *RankingRepository) readUnits(ctx context.Context, players map[int64]*appranking.PlayerAssets) error {
	rows, err := r.read.QueryContext(ctx, `
		SELECT owner_player_id, unit_id, SUM(quantity)
		FROM (
			SELECT p.owner_player_id, u.unit_id, u.quantity
			FROM planet_units u JOIN planets p ON p.id = u.planet_id
			WHERE u.quantity > 0
			UNION ALL
			SELECT f.owner_player_id, s.unit_id, s.quantity
			FROM fleet_ships s JOIN fleets f ON f.id = s.fleet_id
			WHERE f.state IN ('outbound', 'holding', 'returning', 'recalled')
		)
		GROUP BY owner_player_id, unit_id
		ORDER BY owner_player_id, unit_id
	`)
	if err != nil {
		return fmt.Errorf("ranking repository: read units: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var playerID int64
		var quantity appranking.Quantity
		if err := rows.Scan(&playerID, &quantity.ID, &quantity.Quantity); err != nil {
			return fmt.Errorf("ranking repository: scan unit: %w", err)
		}
		if player := players[playerID]; player != nil {
			player.Units = append(player.Units, quantity)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("ranking repository: iterate units: %w", err)
	}
	return nil
}
