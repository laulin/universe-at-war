// Package reports gives a player the narrative of what happened to them.
package reports

import (
	"context"
	"errors"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appeconomy "universeatwar/internal/app/economy"
	domainclock "universeatwar/internal/domain/clock"
	"universeatwar/internal/domain/report"
	"universeatwar/internal/domain/universe"
)

var (
	ErrForbidden = errors.New("reports: authenticated account required")
	ErrNotFound  = errors.New("reports: no such report for this account")
)

// PageSize is how many reports one page shows.
const PageSize = 50

// Summary is one line of the report list.
type Summary struct {
	ID         int64
	Kind       report.Kind
	Coordinate universe.Coordinate
	OccurredAt time.Time
	Read       bool
	Freshness  report.Freshness
}

// Detail is one report with its decoded payload.
type Detail struct {
	Summary
	Payload any
}

// Filter narrows the list.
type Filter struct {
	Kind       report.Kind
	UnreadOnly bool
	Page       int
}

// Repository is the persistence boundary of the reports.
type Repository interface {
	List(context.Context, int64, Filter, time.Time) ([]Summary, error)
	Get(context.Context, int64, int64, time.Time) (Detail, error)
	MarkRead(context.Context, int64, int64, time.Time) error
	UnreadHostile(context.Context, int64) (int, error)
}

// Service reads and marks the reports of one player.
type Service struct {
	Clock      domainclock.Clock
	Repository Repository
	Completer  appeconomy.Completer
}

// List returns one page of reports, newest first.
func (s Service) List(ctx context.Context, principal appauth.Principal, filter Filter) ([]Summary, error) {
	if err := s.validate(principal); err != nil {
		return nil, err
	}
	if s.Completer != nil {
		if _, err := s.Completer.CompleteDue(ctx, 100); err != nil {
			return nil, err
		}
	}
	if filter.Page < 1 {
		filter.Page = 1
	}
	return s.Repository.List(ctx, principal.AccountID, filter, s.Clock.Now().UTC())
}

// Get returns one report of the player. A report of somebody else is reported
// as missing, never as forbidden.
func (s Service) Get(ctx context.Context, principal appauth.Principal, id int64) (Detail, error) {
	if err := s.validate(principal); err != nil {
		return Detail{}, err
	}
	if id <= 0 {
		return Detail{}, ErrNotFound
	}
	return s.Repository.Get(ctx, principal.AccountID, id, s.Clock.Now().UTC())
}

// MarkRead is idempotent: reading twice changes nothing.
func (s Service) MarkRead(ctx context.Context, principal appauth.Principal, id int64) error {
	if err := s.validate(principal); err != nil {
		return err
	}
	if id <= 0 {
		return ErrNotFound
	}
	return s.Repository.MarkRead(ctx, principal.AccountID, id, s.Clock.Now().UTC())
}

// UnreadHostile counts the unread reports that tell the player they are under
// threat, which the layout shows as an alert.
func (s Service) UnreadHostile(ctx context.Context, principal appauth.Principal) (int, error) {
	if err := s.validate(principal); err != nil {
		return 0, err
	}
	return s.Repository.UnreadHostile(ctx, principal.AccountID)
}

func (s Service) validate(principal appauth.Principal) error {
	if s.Clock == nil || s.Repository == nil {
		return errors.New("reports: incomplete service dependencies")
	}
	if principal.AccountID <= 0 || principal.MustChangePassword {
		return ErrForbidden
	}
	return nil
}
