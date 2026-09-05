// Package registration creates player accounts according to the universe policy.
package registration

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	domainclock "universeatwar/internal/domain/clock"
)

var (
	ErrClosed           = errors.New("registration: registrations are closed")
	ErrInvalidUsername  = errors.New("registration: username must contain 3 to 32 characters among a-z, 0-9 and _")
	ErrWeakPassword     = errors.New("registration: password must contain at least 12 characters")
	ErrUsernameTaken    = errors.New("registration: username is already used")
	ErrServerNotRunning = errors.New("registration: universe is not running")
)

// Policies accepted by the ruleset. Invitations arrive with the release
// milestone; until then they are as closed as a closed universe.
const (
	PolicyClosed     = "closed"
	PolicyOpen       = "open"
	PolicyInvitation = "invitation"
)

const minimumPasswordRunes = 12

var usernamePattern = regexp.MustCompile(`^[a-z0-9_]{3,32}$`)

// Passwords hashes a new credential.
type Passwords interface {
	Hash(string) (string, error)
}

// Record is the account to create.
type Record struct {
	Username       string
	NormalizedName string
	EncodedHash    string
	OccurredAt     time.Time
}

// Repository reads the active policy and creates accounts atomically.
type Repository interface {
	RegistrationPolicy(context.Context) (string, error)
	CreateAccount(context.Context, Record) (int64, error)
}

// Service applies the universe registration policy.
type Service struct {
	Clock      domainclock.Clock
	Passwords  Passwords
	Repository Repository
}

// Policy reports how the universe currently accepts new players.
func (s Service) Policy(ctx context.Context) (string, error) {
	if s.Repository == nil {
		return "", errors.New("registration: incomplete service dependencies")
	}
	return s.Repository.RegistrationPolicy(ctx)
}

// Register creates a player account and grants it the player role only.
func (s Service) Register(ctx context.Context, username, password string) (int64, error) {
	if s.Clock == nil || s.Passwords == nil || s.Repository == nil {
		return 0, errors.New("registration: incomplete service dependencies")
	}
	normalized := strings.ToLower(strings.TrimSpace(username))
	if !usernamePattern.MatchString(normalized) {
		return 0, ErrInvalidUsername
	}
	if len([]rune(password)) < minimumPasswordRunes {
		return 0, ErrWeakPassword
	}
	policy, err := s.Repository.RegistrationPolicy(ctx)
	if err != nil {
		return 0, err
	}
	if policy != PolicyOpen {
		return 0, ErrClosed
	}
	encoded, err := s.Passwords.Hash(password)
	if err != nil {
		return 0, err
	}
	return s.Repository.CreateAccount(ctx, Record{
		Username:       strings.TrimSpace(username),
		NormalizedName: normalized,
		EncodedHash:    encoded,
		OccurredAt:     s.Clock.Now().UTC(),
	})
}
