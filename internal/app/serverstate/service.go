// Package serverstate exposes the lifecycle state through an application
// boundary suitable for HTTP and administrative use cases.
package serverstate

import (
	"context"
	"errors"

	"universeatwar/internal/domain/server"
)

// Repository reads persisted lifecycle state.
type Repository interface {
	Current(context.Context) (server.State, error)
}

// Service exposes lifecycle queries without leaking SQLite into the web layer.
type Service struct {
	Repository Repository
}

// Current returns the validated persisted state.
func (s Service) Current(ctx context.Context) (server.State, error) {
	if s.Repository == nil {
		return "", errors.New("server state: repository is nil")
	}
	state, err := s.Repository.Current(ctx)
	if err != nil {
		return "", err
	}
	if err := state.Validate(); err != nil {
		return "", err
	}
	return state, nil
}
