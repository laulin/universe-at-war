// Package bootstrap orchestrates the one-time creation of the local server
// administrator.
package bootstrap

import (
	"context"
	"errors"
	"time"

	domainclock "universeatwar/internal/domain/clock"
)

const bootstrapUsername = "admin"

// Passwords hashes bootstrap credentials.
type Passwords interface {
	Hash(password string) (string, error)
}

// Secrets generates a high-entropy copyable secret.
type Secrets interface {
	Generate() (string, error)
}

// Repository atomically persists the complete bootstrap state.
type Repository interface {
	Initialize(context.Context, Record) (bool, error)
}

// Record contains only values safe or necessary to persist. It never contains
// the clear-text bootstrap password.
type Record struct {
	Username       string
	NormalizedName string
	EncodedHash    string
	Application    string
	OccurredAt     time.Time
}

// Result returns the clear-text password only when this invocation created the
// account.
type Result struct {
	Created  bool
	Username string
	Password string
}

// Service owns the bootstrap use case.
type Service struct {
	Clock      domainclock.Clock
	Passwords  Passwords
	Secrets    Secrets
	Repository Repository
	Version    string
}

// Initialize creates the initial administrator exactly once.
func (s Service) Initialize(ctx context.Context) (Result, error) {
	if s.Clock == nil || s.Passwords == nil || s.Secrets == nil || s.Repository == nil {
		return Result{}, errors.New("bootstrap: incomplete service dependencies")
	}
	secret, err := s.Secrets.Generate()
	if err != nil {
		return Result{}, err
	}
	encodedHash, err := s.Passwords.Hash(secret)
	if err != nil {
		return Result{}, err
	}
	created, err := s.Repository.Initialize(ctx, Record{
		Username:       bootstrapUsername,
		NormalizedName: bootstrapUsername,
		EncodedHash:    encodedHash,
		Application:    s.Version,
		OccurredAt:     s.Clock.Now().UTC(),
	})
	if err != nil {
		return Result{}, err
	}
	if !created {
		return Result{}, nil
	}
	return Result{Created: true, Username: bootstrapUsername, Password: secret}, nil
}
