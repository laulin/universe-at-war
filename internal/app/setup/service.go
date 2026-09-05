// Package setup orchestrates the initial versioned universe configuration.
package setup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	appauth "universeatwar/internal/app/authentication"
	"universeatwar/internal/domain/catalogue"
	domainclock "universeatwar/internal/domain/clock"
	"universeatwar/internal/domain/rules"
)

var (
	ErrForbidden        = errors.New("setup: administrator access required")
	ErrConflict         = errors.New("setup: draft was modified concurrently")
	ErrAlreadyCompleted = errors.New("setup: universe is already configured")
)

// Draft is the application projection of the persisted setup draft.
type Draft struct {
	Rules       rules.Ruleset
	CurrentStep int
	Version     int64
}

// StoredDraft is the persistence representation.
type StoredDraft struct {
	Document    []byte
	CurrentStep int
	Version     int64
}

// Repository atomically stores setup progress and activation.
type Repository interface {
	LoadOrCreate(context.Context, int64, []byte, time.Time) (StoredDraft, error)
	Save(context.Context, int64, int, int64, []byte, time.Time) (StoredDraft, error)
	Activate(context.Context, int64, int64, []byte, string, time.Time) error
}

// Service manages the initial ruleset wizard.
type Service struct {
	Clock      domainclock.Clock
	Repository Repository
}

// Load starts setup if needed and returns the current draft.
func (s Service) Load(ctx context.Context, principal appauth.Principal) (Draft, error) {
	if err := s.authorize(principal); err != nil {
		return Draft{}, err
	}
	defaultDocument, err := rules.Encode(rules.Default())
	if err != nil {
		return Draft{}, err
	}
	stored, err := s.Repository.LoadOrCreate(ctx, principal.AccountID, defaultDocument, s.Clock.Now().UTC())
	if err != nil {
		return Draft{}, err
	}
	return decodeDraft(stored)
}

// Save validates the complete draft and advances exactly one wizard step.
func (s Service) Save(ctx context.Context, principal appauth.Principal, step int, expectedVersion int64, updated rules.Ruleset) (Draft, error) {
	if err := s.authorize(principal); err != nil {
		return Draft{}, err
	}
	if step < 1 || step > 9 {
		return Draft{}, errors.New("setup: only steps 1 through 9 can be saved")
	}
	if err := catalogue.ValidateRuleset(updated); err != nil {
		return Draft{}, err
	}
	document, err := rules.Encode(updated)
	if err != nil {
		return Draft{}, err
	}
	stored, err := s.Repository.Save(ctx, principal.AccountID, step, expectedVersion, document, s.Clock.Now().UTC())
	if err != nil {
		return Draft{}, err
	}
	return decodeDraft(stored)
}

// Activate creates an immutable ruleset version and starts the universe.
func (s Service) Activate(ctx context.Context, principal appauth.Principal, expectedVersion int64, configured rules.Ruleset) error {
	if err := s.authorize(principal); err != nil {
		return err
	}
	if err := catalogue.ValidateRuleset(configured); err != nil {
		return err
	}
	document, err := rules.Encode(configured)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(document)
	return s.Repository.Activate(
		ctx,
		principal.AccountID,
		expectedVersion,
		document,
		hex.EncodeToString(digest[:]),
		s.Clock.Now().UTC(),
	)
}

func (s Service) authorize(principal appauth.Principal) error {
	if s.Clock == nil || s.Repository == nil {
		return errors.New("setup: incomplete service dependencies")
	}
	if !principal.HasRole(appauth.RoleAdmin) || principal.MustChangePassword {
		return ErrForbidden
	}
	return nil
}

func decodeDraft(stored StoredDraft) (Draft, error) {
	configured, err := rules.Decode(stored.Document)
	if err != nil {
		return Draft{}, err
	}
	return Draft{Rules: configured, CurrentStep: stored.CurrentStep, Version: stored.Version}, nil
}
