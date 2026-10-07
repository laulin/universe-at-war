package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"universeatwar/internal/domain/server"
)

// ServerStateRepository persists the singleton server lifecycle state.
type ServerStateRepository struct {
	read  *sql.DB
	write *sql.DB
}

// NewServerStateRepository creates a lifecycle repository.
func NewServerStateRepository(read, write *sql.DB) *ServerStateRepository {
	return &ServerStateRepository{read: read, write: write}
}

// Current returns the singleton lifecycle state.
func (r *ServerStateRepository) Current(ctx context.Context) (server.State, error) {
	var state server.State
	if err := r.read.QueryRowContext(ctx, "SELECT state FROM server_state WHERE id = 1").Scan(&state); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", errors.New("server state repository: server is not bootstrapped")
		}
		return "", fmt.Errorf("server state repository: read current state: %w", err)
	}
	return state, nil
}

// Timezone reads the display fallback from the active immutable ruleset.
func (r *ServerStateRepository) Timezone(ctx context.Context) (string, error) {
	var timezone string
	if err := r.read.QueryRowContext(ctx, `
		SELECT json_extract(document, '$.identity.timezone')
		FROM ruleset_versions WHERE status = 'active'
		ORDER BY version DESC LIMIT 1
	`).Scan(&timezone); err != nil {
		return "", fmt.Errorf("server state repository: read active timezone: %w", err)
	}
	return timezone, nil
}
