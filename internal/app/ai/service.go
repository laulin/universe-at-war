// Package ai runs the administration of the server-driven players. It creates
// them, retires them and reports on them; it never gives them anything a human
// player could not obtain through the ordinary use cases.
package ai

import (
	"context"
	"errors"
	"strings"
	"time"

	appalliance "universeatwar/internal/app/alliance"
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
	ErrConflict       = errors.New("ai: profile was modified concurrently")
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

// Teamview is what an administrator sees of the team of an artificial player:
// its role, the plan of the alliance and every belief that plan rests on, each
// with the member it came from.
type Teamview struct {
	Name      string
	Tag       string
	Role      domainai.Role
	Objective *domainai.Objective
	Beliefs   []domainai.Knowledge
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
	Team        *Teamview
}

// Request is what an administrator asks for when adding a player.
type Request struct {
	Name      string
	Archetype domainai.Archetype
	Window    domainai.Window
	Interval  time.Duration
	// Home is the corner of the map the empire aims for, and settles at the
	// first free position from there. A coordinate that names nothing takes the
	// first free position of the universe, which is what the administration
	// form asks for.
	Home universe.Coordinate
}

// UpdateRequest is the editable character of an existing artificial player.
// A nil Custom returns it to the defaults of its archetype.
type UpdateRequest struct {
	Version   int64
	Archetype domainai.Archetype
	Window    domainai.Window
	Interval  time.Duration
	Custom    *domainai.Tuning
}

// Seeds hands out the unpredictable seed each artificial player keeps for life.
type Seeds interface {
	Seed() (int64, error)
}

// Empires creates the empire of a new artificial player through the same use
// case a human goes through.
type Empires interface {
	CreateEmpireNear(context.Context, appauth.Principal, string, universe.Coordinate) (appeconomy.Planet, error)
}

// Repository is the atomic persistence boundary of the artificial players.
type Repository interface {
	CreateAccount(context.Context, string, time.Time) (int64, error)
	DisableAccount(context.Context, int64, time.Time) error
	CreateProfile(context.Context, int64, domainai.Profile, time.Time, time.Time) (Profile, error)
	Retire(context.Context, int64, time.Time) error
	Enlistment(context.Context, int64, string) (Enlistment, error)
	List(context.Context, time.Time) ([]Profile, error)
	Inspect(context.Context, int64, int, time.Time) (Profile, error)
	UpdateProfile(context.Context, int64, int64, domainai.Profile, time.Time) error
}

// Service runs the administration use cases of the artificial players.
type Service struct {
	Clock      domainclock.Clock
	Repository Repository
	// Census answers what the ruleset ordered. Without it the administration
	// still works; it simply cannot say how far the universe has filled up.
	Census    Censuses
	Empires   Empires
	Alliances Alliances
	Seeds     Seeds
	Completer appeconomy.Completer
}

// DiaryLength is how many decisions an inspection brings back.
const DiaryLength = 25

// Create adds an artificial player at the request of an administrator: an
// account without any credential, an empire founded like any other, and a
// character.
func (s Service) Create(ctx context.Context, principal appauth.Principal, request Request) (Profile, error) {
	if err := s.validate(principal); err != nil {
		return Profile{}, err
	}
	return s.create(ctx, request)
}

// create is the birth itself, held apart from the question of who asked for it.
// Two callers reach it: the administration form, which asks in the name of a
// logged-in administrator, and the population the ruleset ordered, which nobody
// is logged in for. Both are administration, neither is play, and the player
// that comes out of either has exactly the privileges of a human — none.
func (s Service) create(ctx context.Context, request Request) (Profile, error) {
	name := strings.TrimSpace(request.Name)
	if len(name) < 3 || len(name) > 32 {
		return Profile{}, ErrInvalidRequest
	}
	// The clock and the repository are checked here rather than only where a
	// principal is, because a caller that needs no principal still needs them.
	if s.Clock == nil || s.Repository == nil || s.Seeds == nil || s.Empires == nil {
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
	if _, err := s.Empires.CreateEmpireNear(ctx, appauth.Principal{AccountID: accountID}, name, request.Home); err != nil {
		// A player without a world would only ever record its own impotence.
		_ = s.Repository.DisableAccount(ctx, accountID, now)
		return Profile{}, err
	}
	// The first reflection is one interval away, spread like any other.
	first := domainai.NextThink(now, profile.Interval, seed, 0)
	created, err := s.Repository.CreateProfile(ctx, accountID, profile, first, now)
	if err != nil {
		// The empire is founded and stands on the map; disabling the account
		// here would strand it there for good, owned by a player that can never
		// think. It is left as it is, and the reconciler finishes the birth.
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
	return s.retire(ctx, playerID)
}

// retire is the common departure path for an administrator and for the
// population reconciler noticing an empire with no world left.
func (s Service) retire(ctx context.Context, playerID int64) error {
	if s.Clock == nil || s.Repository == nil {
		return errors.New("ai: incomplete service dependencies")
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

// Configured is the population the ruleset ordered. Beside the list of players
// it is what tells an administrator whether the universe is still filling up or
// has already settled, which the list alone cannot say.
func (s Service) Configured(ctx context.Context, principal appauth.Principal) (int, error) {
	if err := s.validate(principal); err != nil {
		return 0, err
	}
	if s.Census == nil {
		return 0, nil
	}
	census, err := s.Census.Census(ctx)
	if err != nil {
		return 0, err
	}
	if !census.Running {
		return 0, nil
	}
	return PopulationFrom(census.Rules).Total, nil
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

// Update changes how one active artificial player behaves from its next
// reflection. It changes neither its empire nor its accumulated memory.
func (s Service) Update(ctx context.Context, principal appauth.Principal, playerID int64,
	request UpdateRequest) (Profile, error) {
	if err := s.validate(principal); err != nil {
		return Profile{}, err
	}
	if playerID <= 0 || request.Version <= 0 {
		return Profile{}, ErrInvalidRequest
	}
	profile := domainai.Profile{
		PlayerID: playerID, Archetype: request.Archetype, Window: request.Window,
		Interval: request.Interval, Custom: request.Custom,
	}
	if err := profile.Validate(); err != nil {
		return Profile{}, err
	}
	if err := s.Repository.UpdateProfile(ctx, principal.AccountID, request.Version, profile, s.Clock.Now().UTC()); err != nil {
		return Profile{}, err
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

// Alliances is the team-building surface an artificial player uses. Every call
// goes through the ordinary alliance use cases: an invitation is still an
// invitation, and a founder still needs the right to send it.
type Alliances interface {
	Create(context.Context, appauth.Principal, string, string, string) (appalliance.Profile, error)
	Invite(context.Context, appauth.Principal, string) error
	Invitations(context.Context, appauth.Principal) ([]appalliance.Invitation, error)
	Accept(context.Context, appauth.Principal, int64) error
}

// Enlistment is what the administration needs to know to bring an artificial
// player into a team: who it is, and who could invite it.
type Enlistment struct {
	AccountID       int64
	Name            string
	Exists          bool
	LeaderAccountID int64
}

// Enlist puts an artificial player into an alliance. It founds the alliance
// when it does not exist yet; otherwise a member who may invite does so, and
// the artificial player accepts. No shortcut exists: an alliance that will not
// have it does not get it.
func (s Service) Enlist(ctx context.Context, principal appauth.Principal, playerID int64, name, tag string) error {
	if err := s.validate(principal); err != nil {
		return err
	}
	return s.enlist(ctx, playerID, name, tag)
}

// enlist is the enrolment itself, held apart from who asked for it, exactly as
// create is. It takes no shortcut either: the alliance is founded, or a member
// who may invite invites and the recruit accepts.
func (s Service) enlist(ctx context.Context, playerID int64, name, tag string) error {
	if s.Repository == nil {
		return errors.New("ai: incomplete service dependencies")
	}
	if playerID <= 0 || strings.TrimSpace(name) == "" || strings.TrimSpace(tag) == "" {
		return ErrInvalidRequest
	}
	if s.Alliances == nil {
		return errors.New("ai: no alliance service")
	}
	enlistment, err := s.Repository.Enlistment(ctx, playerID, tag)
	if err != nil {
		return err
	}
	recruit := appauth.Principal{AccountID: enlistment.AccountID, Roles: []appauth.Role{appauth.RolePlayer}}
	if !enlistment.Exists {
		_, err := s.Alliances.Create(ctx, recruit, name, tag, "")
		return err
	}
	if enlistment.LeaderAccountID == 0 {
		return ErrNotFound
	}
	leader := appauth.Principal{AccountID: enlistment.LeaderAccountID, Roles: []appauth.Role{appauth.RolePlayer}}
	if err := s.Alliances.Invite(ctx, leader, enlistment.Name); err != nil {
		return err
	}
	invitations, err := s.Alliances.Invitations(ctx, recruit)
	if err != nil {
		return err
	}
	for _, invitation := range invitations {
		if invitation.AllianceTag == strings.ToUpper(strings.TrimSpace(tag)) {
			return s.Alliances.Accept(ctx, recruit, invitation.ID)
		}
	}
	return ErrNotFound
}
