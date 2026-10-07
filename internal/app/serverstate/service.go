// Package serverstate exposes the lifecycle state through an application
// boundary suitable for HTTP and administrative use cases.
package serverstate

import (
	"context"
	"errors"
	"time"

	"universeatwar/internal/domain/server"
)

// Repository reads persisted lifecycle state.
type Repository interface {
	Current(context.Context) (server.State, error)
	Timezone(context.Context) (string, error)
}

// Timezone returns the validated IANA timezone of the active universe. Game
// instants remain UTC; this setting is only the server-side display fallback
// until a browser can render them in the player's own timezone.
func (s Service) Timezone(ctx context.Context) (string, error) {
	if s.Repository == nil {
		return "", errors.New("server state: repository is nil")
	}
	name, err := s.Repository.Timezone(ctx)
	if err != nil {
		return "", err
	}
	if _, err := time.LoadLocation(name); err != nil {
		return "", errors.New("server state: active timezone is invalid")
	}
	return name, nil
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
