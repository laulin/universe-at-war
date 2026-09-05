// Package administration contains privileged local and web use cases.
package administration

import (
	"context"
	"errors"
	"strings"
	"time"

	domainclock "universeatwar/internal/domain/clock"
)

var (
	ErrAccountNotFound  = errors.New("administration: account not found")
	ErrNotAdministrator = errors.New("administration: target account is not an administrator")
)

type Passwords interface {
	Hash(string) (string, error)
}

type Secrets interface {
	Generate() (string, error)
}

type PasswordResetRepository interface {
	ResetAdministratorPassword(context.Context, string, string, time.Time) error
}

type PasswordResetResult struct {
	Username string
	Password string
}

// PasswordResetService performs machine-local administrator recovery.
type PasswordResetService struct {
	Clock      domainclock.Clock
	Passwords  Passwords
	Secrets    Secrets
	Repository PasswordResetRepository
}

// Reset replaces an administrator credential and revokes every session.
func (s PasswordResetService) Reset(ctx context.Context, username string) (PasswordResetResult, error) {
	if s.Clock == nil || s.Passwords == nil || s.Secrets == nil || s.Repository == nil {
		return PasswordResetResult{}, errors.New("administration: incomplete password reset dependencies")
	}
	normalized := strings.ToLower(strings.TrimSpace(username))
	if normalized == "" {
		return PasswordResetResult{}, ErrAccountNotFound
	}
	secret, err := s.Secrets.Generate()
	if err != nil {
		return PasswordResetResult{}, err
	}
	encoded, err := s.Passwords.Hash(secret)
	if err != nil {
		return PasswordResetResult{}, err
	}
	if err := s.Repository.ResetAdministratorPassword(ctx, normalized, encoded, s.Clock.Now().UTC()); err != nil {
		return PasswordResetResult{}, err
	}
	return PasswordResetResult{Username: normalized, Password: secret}, nil
}
