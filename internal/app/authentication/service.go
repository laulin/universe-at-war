// Package authentication implements login and opaque server-side sessions.
package authentication

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"time"

	domainclock "universeatwar/internal/domain/clock"
)

var (
	// ErrInvalidCredentials deliberately does not reveal whether an account
	// exists, is disabled, is banned, or has a different password.
	ErrInvalidCredentials = errors.New("authentication: invalid credentials")
	ErrInvalidSession     = errors.New("authentication: invalid session")
	ErrWeakPassword       = errors.New("authentication: password must contain at least 12 characters")
	ErrCredentialNotFound = errors.New("authentication: credential not found")
	ErrConflict           = errors.New("authentication: concurrent account change")
)

// Role is an authorization role resolved from server-side state.
type Role string

const (
	RoleAdmin     Role = "ADMIN"
	RoleModerator Role = "MODERATOR"
	RolePlayer    Role = "PLAYER"
)

// Passwords verifies and creates password credentials.
type Passwords interface {
	Hash(password string) (string, error)
	Verify(encoded, password string) (match, needsRehash bool, err error)
}

// Tokens creates opaque session tokens.
type Tokens interface {
	Generate() (string, error)
}

// Credential is the private authentication projection of an account.
type Credential struct {
	AccountID          int64
	Username           string
	EncodedHash        string
	MustChangePassword bool
	Version            int64
	Unavailable        bool
}

// Principal is the authorization projection attached to a valid request.
type Principal struct {
	AccountID          int64
	Username           string
	Roles              []Role
	MustChangePassword bool
}

// HasRole checks a server-resolved role.
func (p Principal) HasRole(want Role) bool {
	for _, role := range p.Roles {
		if role == want {
			return true
		}
	}
	return false
}

// SessionRecord contains a digest, never the clear-text token.
type SessionRecord struct {
	AccountID      int64
	AccountVersion int64
	TokenDigest    []byte
	IssuedAt       time.Time
	ExpiresAt      time.Time
	Rehashed       string
}

// PasswordChange atomically replaces the credential and all sessions.
type PasswordChange struct {
	AccountID      int64
	AccountVersion int64
	EncodedHash    string
	OldTokenDigest []byte
	NewTokenDigest []byte
	IssuedAt       time.Time
	ExpiresAt      time.Time
}

// Repository is the persistence boundary for authentication transactions.
type Repository interface {
	FindCredentialByUsername(context.Context, string, time.Time) (Credential, error)
	FindCredentialByID(context.Context, int64, time.Time) (Credential, error)
	RecordFailedLogin(context.Context, string, time.Time) error
	CreateSession(context.Context, SessionRecord) error
	ResolveSession(context.Context, []byte, time.Time) (Principal, error)
	ChangePassword(context.Context, PasswordChange) error
	RevokeSession(context.Context, []byte, time.Time) error
}

// LoginResult contains the only copy of the opaque token returned to the
// caller.
type LoginResult struct {
	Token              string
	ExpiresAt          time.Time
	MustChangePassword bool
}

// Service authenticates accounts and manages sessions.
type Service struct {
	Clock       domainclock.Clock
	Passwords   Passwords
	Tokens      Tokens
	Repository  Repository
	SessionLife time.Duration
}

// Login verifies credentials and creates a server-side session.
func (s Service) Login(ctx context.Context, username, password string) (LoginResult, error) {
	if err := s.validate(); err != nil {
		return LoginResult{}, err
	}
	normalized := normalizeUsername(username)
	now := s.Clock.Now().UTC()
	credential, err := s.Repository.FindCredentialByUsername(ctx, normalized, now)
	if err != nil {
		if errors.Is(err, ErrCredentialNotFound) {
			_ = s.Repository.RecordFailedLogin(ctx, normalized, now)
			return LoginResult{}, ErrInvalidCredentials
		}
		return LoginResult{}, err
	}
	if credential.Unavailable {
		_ = s.Repository.RecordFailedLogin(ctx, normalized, now)
		return LoginResult{}, ErrInvalidCredentials
	}
	match, needsRehash, err := s.Passwords.Verify(credential.EncodedHash, password)
	if err != nil {
		return LoginResult{}, err
	}
	if !match {
		_ = s.Repository.RecordFailedLogin(ctx, normalized, now)
		return LoginResult{}, ErrInvalidCredentials
	}

	var rehashed string
	if needsRehash {
		rehashed, err = s.Passwords.Hash(password)
		if err != nil {
			return LoginResult{}, err
		}
	}
	token, digest, err := s.newToken()
	if err != nil {
		return LoginResult{}, err
	}
	expiresAt := now.Add(s.SessionLife)
	if err := s.Repository.CreateSession(ctx, SessionRecord{
		AccountID:      credential.AccountID,
		AccountVersion: credential.Version,
		TokenDigest:    digest,
		IssuedAt:       now,
		ExpiresAt:      expiresAt,
		Rehashed:       rehashed,
	}); err != nil {
		if errors.Is(err, ErrConflict) {
			return LoginResult{}, ErrInvalidCredentials
		}
		return LoginResult{}, err
	}
	return LoginResult{Token: token, ExpiresAt: expiresAt, MustChangePassword: credential.MustChangePassword}, nil
}

// Resolve returns the principal associated with a live opaque token.
func (s Service) Resolve(ctx context.Context, token string) (Principal, error) {
	if err := s.validate(); err != nil {
		return Principal{}, err
	}
	if token == "" {
		return Principal{}, ErrInvalidSession
	}
	digest := sha256.Sum256([]byte(token))
	principal, err := s.Repository.ResolveSession(ctx, digest[:], s.Clock.Now().UTC())
	if errors.Is(err, ErrCredentialNotFound) {
		return Principal{}, ErrInvalidSession
	}
	return principal, err
}

// ChangePassword verifies the current password, upgrades the credential, and
// rotates all sessions atomically.
func (s Service) ChangePassword(ctx context.Context, token, currentPassword, newPassword string) (LoginResult, error) {
	if len([]rune(newPassword)) < 12 {
		return LoginResult{}, ErrWeakPassword
	}
	principal, err := s.Resolve(ctx, token)
	if err != nil {
		return LoginResult{}, err
	}
	now := s.Clock.Now().UTC()
	credential, err := s.Repository.FindCredentialByID(ctx, principal.AccountID, now)
	if err != nil || credential.Unavailable {
		if errors.Is(err, ErrCredentialNotFound) || credential.Unavailable {
			return LoginResult{}, ErrInvalidCredentials
		}
		return LoginResult{}, err
	}
	match, _, err := s.Passwords.Verify(credential.EncodedHash, currentPassword)
	if err != nil {
		return LoginResult{}, err
	}
	if !match || currentPassword == newPassword {
		return LoginResult{}, ErrInvalidCredentials
	}
	encodedHash, err := s.Passwords.Hash(newPassword)
	if err != nil {
		return LoginResult{}, err
	}
	newToken, newDigest, err := s.newToken()
	if err != nil {
		return LoginResult{}, err
	}
	oldDigest := sha256.Sum256([]byte(token))
	expiresAt := now.Add(s.SessionLife)
	if err := s.Repository.ChangePassword(ctx, PasswordChange{
		AccountID:      credential.AccountID,
		AccountVersion: credential.Version,
		EncodedHash:    encodedHash,
		OldTokenDigest: oldDigest[:],
		NewTokenDigest: newDigest,
		IssuedAt:       now,
		ExpiresAt:      expiresAt,
	}); err != nil {
		return LoginResult{}, err
	}
	return LoginResult{Token: newToken, ExpiresAt: expiresAt}, nil
}

// Logout revokes an opaque session. Repeating the operation is safe.
func (s Service) Logout(ctx context.Context, token string) error {
	if err := s.validate(); err != nil {
		return err
	}
	if token == "" {
		return nil
	}
	digest := sha256.Sum256([]byte(token))
	return s.Repository.RevokeSession(ctx, digest[:], s.Clock.Now().UTC())
}

func (s Service) newToken() (string, []byte, error) {
	token, err := s.Tokens.Generate()
	if err != nil {
		return "", nil, err
	}
	digest := sha256.Sum256([]byte(token))
	return token, digest[:], nil
}

func (s Service) validate() error {
	if s.Clock == nil || s.Passwords == nil || s.Tokens == nil || s.Repository == nil {
		return errors.New("authentication: incomplete service dependencies")
	}
	if s.SessionLife <= 0 {
		return errors.New("authentication: session lifetime must be positive")
	}
	return nil
}

func normalizeUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}
