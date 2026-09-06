// Package ai runs the administration of the server-driven players. It creates
// them, retires them and reports on them; it never gives them anything a human
// player could not obtain through the ordinary use cases.
package ai

import (
	"context"
	"errors"
	"strings"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appeconomy "universeatwar/internal/app/economy"
	domainai "universeatwar/internal/domain/ai"
	domainclock "universeatwar/internal/domain/clock"
	"universeatwar/internal/domain/universe"
)

var (
	ErrForbidden      = errors.New("ai: administrator role required")
	ErrNotFound       = errors.New("ai: no such artificial player")
	ErrInvalidRequest = errors.New("ai: invalid request")
	ErrNameTaken      = errors.New("ai: this name is already used")
)

// Decision is one line of the diary of an artificial player, as an
// administrator reads it.
type Decision struct {
	DecidedAt time.Time
	Layer     domainai.Layer
	Action    string
	Outcome   domainai.Outcome
	Reason    string
	Score     float64
	BodyID    int64
	Target    string
}

// Memory is one thing an artificial player believes it knows.
type Memory struct {
	Kind       string
	Coordinate universe.Coordinate
	ObservedAt time.Time
	Score      float64
	Summary    string
}

// Profile is the administration view of one artificial player.
type Profile struct {
	domainai.Profile
	CreatedAt   time.Time
	NextThinkAt *time.Time
	LastThinkAt *time.Time
	Awake       bool
	Bodies      int
	Decisions   []Decision
	Memories    []Memory
}

// Request is what an administrator asks for when adding a player.
type Request struct {
	Name      string
	Archetype domainai.Archetype
	Window    domainai.Window
	Interval  time.Duration
}

// Seeds hands out the unpredictable seed each artificial player keeps for life.
type Seeds interface {
	Seed() (int64, error)
}

// Empires creates the empire of a new artificial player through the same use
// case a human goes through.
type Empires interface {
	CreateEmpire(context.Context, appauth.Principal, string) (appeconomy.Planet, error)
}

// Repository is the atomic persistence boundary of the artificial players.
type Repository interface {
	CreateAccount(context.Context, string, time.Time) (int64, error)
	DisableAccount(context.Context, int64, time.Time) error
	CreateProfile(context.Context, int64, domainai.Profile, time.Time, time.Time) (Profile, error)
	Retire(context.Context, int64, time.Time) error
	List(context.Context, time.Time) ([]Profile, error)
	Inspect(context.Context, int64, int, time.Time) (Profile, error)
}

// Service runs the administration use cases of the artificial players.
type Service struct {
	Clock      domainclock.Clock
	Repository Repository
	Empires    Empires
	Seeds      Seeds
	Completer  appeconomy.Completer
}

// DiaryLength is how many decisions an inspection brings back.
const DiaryLength = 25

// Create adds an artificial player: an account without any credential, an
// empire founded like any other, and a character.
func (s Service) Create(ctx context.Context, principal appauth.Principal, request Request) (Profile, error) {
	if err := s.validate(principal); err != nil {
		return Profile{}, err
	}
	name := strings.TrimSpace(request.Name)
	if len(name) < 3 || len(name) > 32 {
		return Profile{}, ErrInvalidRequest
	}
	if s.Seeds == nil || s.Empires == nil {
		return Profile{}, errors.New("ai: incomplete service dependencies")
	}
	seed, err := s.Seeds.Seed()
	if err != nil {
		return Profile{}, err
	}
	profile := domainai.Profile{
		Name:      name,
		Archetype: request.Archetype,
		Window:    request.Window,
		Interval:  request.Interval,
		Seed:      seed,
	}
	if err := profile.Validate(); err != nil {
		return Profile{}, err
	}
	now := s.Clock.Now().UTC()
	accountID, err := s.Repository.CreateAccount(ctx, name, now)
	if err != nil {
		return Profile{}, err
	}
	profile.AccountID = accountID
	if _, err := s.Empires.CreateEmpire(ctx, appauth.Principal{AccountID: accountID}, name); err != nil {
		// A player without a world would only ever record its own impotence.
		_ = s.Repository.DisableAccount(ctx, accountID, now)
		return Profile{}, err
	}
	// The first reflection is one interval away, spread like any other.
	first := domainai.NextThink(now, profile.Interval, seed, 0)
	created, err := s.Repository.CreateProfile(ctx, accountID, profile, first, now)
	if err != nil {
		_ = s.Repository.DisableAccount(ctx, accountID, now)
		return Profile{}, err
	}
	return created, nil
}

// Retire stops an artificial player for good. Its empire stays where it is:
// removing a world would rewrite the map under the other players.
func (s Service) Retire(ctx context.Context, principal appauth.Principal, playerID int64) error {
	if err := s.validate(principal); err != nil {
		return err
	}
	if playerID <= 0 {
		return ErrNotFound
	}
	return s.Repository.Retire(ctx, playerID, s.Clock.Now().UTC())
}

// List reports on every artificial player of the universe.
func (s Service) List(ctx context.Context, principal appauth.Principal) ([]Profile, error) {
	if err := s.validate(principal); err != nil {
		return nil, err
	}
	if s.Completer != nil {
		if _, err := s.Completer.CompleteDue(ctx, 100); err != nil {
			return nil, err
		}
	}
	return s.Repository.List(ctx, s.Clock.Now().UTC())
}

// Inspect opens the diary of one artificial player. It is the omniscient view,
// reserved for administrators and never served to a player.
func (s Service) Inspect(ctx context.Context, principal appauth.Principal, playerID int64) (Profile, error) {
	if err := s.validate(principal); err != nil {
		return Profile{}, err
	}
	if playerID <= 0 {
		return Profile{}, ErrNotFound
	}
	return s.Repository.Inspect(ctx, playerID, DiaryLength, s.Clock.Now().UTC())
}

func (s Service) validate(principal appauth.Principal) error {
	if s.Clock == nil || s.Repository == nil {
		return errors.New("ai: incomplete service dependencies")
	}
	if principal.AccountID <= 0 || principal.MustChangePassword || !principal.HasRole(appauth.RoleAdmin) {
		return ErrForbidden
	}
	return nil
}
