package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"universeatwar/internal/domain/debris"
	"universeatwar/internal/domain/universe"
)

// loadDebris reads the field of one position, which may not exist.
func loadDebris(ctx context.Context, tx *sql.Tx, at universe.Coordinate) (debris.Field, error) {
	var field debris.Field
	err := tx.QueryRowContext(ctx,
		"SELECT metal, crystal FROM debris_fields WHERE galaxy = ? AND system = ? AND position = ?",
		at.Galaxy, at.System, at.Position).Scan(&field.Metal, &field.Crystal)
	if errors.Is(err, sql.ErrNoRows) {
		return debris.Field{}, nil
	}
	if err != nil {
		return debris.Field{}, fmt.Errorf("debris repository: read field: %w", err)
	}
	return field, nil
}

// addDebris creates or grows the field of one position. A field that would hold
// nothing is not created at all.
func addDebris(ctx context.Context, tx *sql.Tx, at universe.Coordinate, field debris.Field, now time.Time) error {
	if field.Empty() {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO debris_fields(galaxy, system, position, metal, crystal, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(galaxy, system, position) DO UPDATE SET
			metal = metal + excluded.metal,
			crystal = crystal + excluded.crystal,
			updated_at = excluded.updated_at,
			version = version + 1
	`, at.Galaxy, at.System, at.Position, field.Metal, field.Crystal, timestamp(now), timestamp(now)); err != nil {
		return fmt.Errorf("debris repository: add field: %w", err)
	}
	return nil
}

// takeDebris removes a harvest from a field. The update is guarded so two
// recyclers can never take more than the field holds, and an emptied field is
// deleted rather than left at zero.
func takeDebris(ctx context.Context, tx *sql.Tx, at universe.Coordinate, harvest debris.Field, now time.Time) error {
	if harvest.Empty() {
		return nil
	}
	field, err := loadDebris(ctx, tx, at)
	if err != nil {
		return err
	}
	if harvest.Metal > field.Metal || harvest.Crystal > field.Crystal {
		return errors.New("debris repository: harvest exceeds the field")
	}
	remaining := debris.Field{Metal: field.Metal - harvest.Metal, Crystal: field.Crystal - harvest.Crystal}
	var result sql.Result
	if remaining.Empty() {
		result, err = tx.ExecContext(ctx, `
			DELETE FROM debris_fields WHERE galaxy = ? AND system = ? AND position = ? AND metal = ? AND crystal = ?
		`, at.Galaxy, at.System, at.Position, field.Metal, field.Crystal)
	} else {
		result, err = tx.ExecContext(ctx, `
			UPDATE debris_fields SET metal = metal - ?, crystal = crystal - ?, updated_at = ?, version = version + 1
			WHERE galaxy = ? AND system = ? AND position = ? AND metal >= ? AND crystal >= ?
		`, harvest.Metal, harvest.Crystal, timestamp(now), at.Galaxy, at.System, at.Position, harvest.Metal, harvest.Crystal)
	}
	if err != nil {
		return fmt.Errorf("debris repository: take field: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("debris repository: take field: %w", err)
	}
	if affected != 1 {
		return errors.New("debris repository: the field changed during the harvest")
	}
	return nil
}
