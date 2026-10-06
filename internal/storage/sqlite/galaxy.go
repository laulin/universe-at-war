package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	appgalaxy "universeatwar/internal/app/galaxy"
	"universeatwar/internal/domain/debris"
	"universeatwar/internal/domain/universe"
)

// GalaxyRepository reads the public map. It deliberately selects nothing but
// what every player is allowed to see.
type GalaxyRepository struct {
	read *sql.DB
}

func NewGalaxyRepository(read *sql.DB) *GalaxyRepository {
	return &GalaxyRepository{read: read}
}

// System lists one system: planet names, owners and debris fields, nothing else.
func (r *GalaxyRepository) System(ctx context.Context, accountID int64, galaxy, system int) (appgalaxy.View, error) {
	configured, err := activeRulesetFrom(ctx, r.read)
	if err != nil {
		return appgalaxy.View{}, err
	}
	limits := universe.Limits{
		Galaxies:  configured.Topology.Galaxies,
		Systems:   configured.Topology.SystemsPerGalaxy,
		Positions: configured.Topology.PositionsPerSystem,
	}
	if galaxy < 1 || galaxy > limits.Galaxies || system < 1 || system > limits.Systems {
		return appgalaxy.View{}, appgalaxy.ErrOutsideUniverse
	}
	rows := make([]appgalaxy.Row, 0, limits.Positions)
	for position := 1; position <= limits.Positions; position++ {
		rows = append(rows, appgalaxy.Row{Position: position})
	}

	planets, err := r.read.QueryContext(ctx, `
		SELECT p.position, p.id, p.name, pl.id, pl.display_name, pl.account_id
		FROM planets p JOIN players pl ON pl.id = p.owner_player_id
		WHERE p.galaxy = ? AND p.system = ?
	`, galaxy, system)
	if err != nil {
		return appgalaxy.View{}, fmt.Errorf("galaxy repository: read planets: %w", err)
	}
	defer planets.Close()
	for planets.Next() {
		var position int
		var planetID, ownerPlayerID, ownerAccountID int64
		var planetName, ownerName string
		if err := planets.Scan(&position, &planetID, &planetName, &ownerPlayerID, &ownerName, &ownerAccountID); err != nil {
			return appgalaxy.View{}, fmt.Errorf("galaxy repository: scan planet: %w", err)
		}
		if position < 1 || position > limits.Positions {
			continue
		}
		rows[position-1].PlanetID = planetID
		rows[position-1].PlanetName = planetName
		rows[position-1].OwnerPlayerID = ownerPlayerID
		rows[position-1].OwnerName = ownerName
		rows[position-1].Own = ownerAccountID == accountID
	}
	if err := planets.Err(); err != nil {
		return appgalaxy.View{}, fmt.Errorf("galaxy repository: iterate planets: %w", err)
	}

	fields, err := r.read.QueryContext(ctx,
		"SELECT position, metal, crystal FROM debris_fields WHERE galaxy = ? AND system = ?", galaxy, system)
	if err != nil {
		return appgalaxy.View{}, fmt.Errorf("galaxy repository: read debris: %w", err)
	}
	defer fields.Close()
	for fields.Next() {
		var position int
		var field debris.Field
		if err := fields.Scan(&position, &field.Metal, &field.Crystal); err != nil {
			return appgalaxy.View{}, fmt.Errorf("galaxy repository: scan debris: %w", err)
		}
		if position < 1 || position > limits.Positions {
			continue
		}
		wreckage := field
		rows[position-1].Debris = &wreckage
	}
	if err := fields.Err(); err != nil {
		return appgalaxy.View{}, fmt.Errorf("galaxy repository: iterate debris: %w", err)
	}

	home, err := r.homePlanets(ctx, accountID)
	if err != nil {
		return appgalaxy.View{}, err
	}
	return appgalaxy.View{Galaxy: galaxy, System: system, Limits: limits, Rows: rows, HomePlanets: home}, nil
}

// homePlanets lists the planets of the viewer, so the page can offer to send a
// fleet from one of them.
func (r *GalaxyRepository) homePlanets(ctx context.Context, accountID int64) ([]int64, error) {
	rows, err := r.read.QueryContext(ctx, `
		SELECT p.id FROM planets p JOIN players pl ON pl.id = p.owner_player_id
		WHERE pl.account_id = ? ORDER BY p.id
	`, accountID)
	if err != nil {
		return nil, fmt.Errorf("galaxy repository: read own planets: %w", err)
	}
	defer rows.Close()
	var identifiers []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("galaxy repository: scan own planet: %w", err)
		}
		identifiers = append(identifiers, id)
	}
	return identifiers, rows.Err()
}
