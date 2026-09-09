// Package alliance orchestrates team membership, invitations and diplomacy.
package alliance

import (
	"context"
	"errors"
	"strings"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appeconomy "universeatwar/internal/app/economy"
	domainalliance "universeatwar/internal/domain/alliance"
	domainclock "universeatwar/internal/domain/clock"
)

var (
	ErrForbidden       = errors.New("alliance: this account cannot do that")
	ErrDisabled        = errors.New("alliance: alliances are disabled in this universe")
	ErrNotAMember      = errors.New("alliance: this account belongs to no alliance")
	ErrAlreadyAMember  = errors.New("alliance: this player already belongs to an alliance")
	ErrNameTaken       = errors.New("alliance: this name or tag is already used")
	ErrFull            = errors.New("alliance: this alliance is full")
	ErrNoSuchPlayer    = errors.New("alliance: no such player")
	ErrNoInvitation    = errors.New("alliance: no pending invitation")
	ErrInvitationEnded = errors.New("alliance: this invitation has expired")
	ErrLastFounder     = errors.New("alliance: hand the charge over before leaving")
	ErrNotFound        = errors.New("alliance: no such alliance")
)

// Member is one player of the team.
type Member struct {
	PlayerID int64
	Name     string
	Role     domainalliance.Role
	JoinedAt time.Time
}

// Invitation is a pending offer to join.
type Invitation struct {
	ID            int64
	AllianceID    int64
	AllianceName  string
	AllianceTag   string
	PlayerID      int64
	PlayerName    string
	InvitedByName string
	ExpiresAt     time.Time
	Expired       bool
}

// Relation is a declared intention towards another alliance.
type Relation struct {
	OtherAllianceID int64
	OtherName       string
	OtherTag        string
	Kind            domainalliance.Relation
	DeclaredAt      time.Time
}

// Entry is one line of the alliance history.
type Entry struct {
	Action     string
	ActorName  string
	TargetName string
	OccurredAt time.Time
	Details    string
}

// Profile is what a member sees of their own alliance.
type Profile struct {
	ID          int64
	Name        string
	Tag         string
	Description string
	Role        domainalliance.Role
	Members     []Member
	Invitations []Invitation
	Relations   []Relation
	// Received is what other alliances have declared about this one. A relation
	// announces, and an announcement nobody can hear announces nothing.
	Received    []Relation
	History     []Entry
	MaximumSize int
	Diplomacy   bool
}

// Repository is the atomic persistence boundary of the alliance use cases.
type Repository interface {
	Profile(context.Context, int64, time.Time) (Profile, error)
	InvitationsFor(context.Context, int64, time.Time) ([]Invitation, error)
	Create(context.Context, int64, string, string, string, time.Time) (Profile, error)
	Invite(context.Context, int64, string, time.Time) error
	Accept(context.Context, int64, int64, time.Time) error
	Decline(context.Context, int64, int64, time.Time) error
	Leave(context.Context, int64, time.Time) error
	Expel(context.Context, int64, int64, time.Time) error
	Promote(context.Context, int64, int64, domainalliance.Role, time.Time) error
	Declare(context.Context, int64, string, domainalliance.Relation, time.Time) error
	Describe(context.Context, int64, string, time.Time) error
}

// Service runs the alliance use cases of one account.
type Service struct {
	Clock      domainclock.Clock
	Repository Repository
}

// Profile returns the alliance of the account.
func (s Service) Profile(ctx context.Context, principal appauth.Principal) (Profile, error) {
	if err := s.validate(principal); err != nil {
		return Profile{}, err
	}
	return s.Repository.Profile(ctx, principal.AccountID, s.Clock.Now().UTC())
}

// Invitations lists the offers the account has received.
func (s Service) Invitations(ctx context.Context, principal appauth.Principal) ([]Invitation, error) {
	if err := s.validate(principal); err != nil {
		return nil, err
	}
	return s.Repository.InvitationsFor(ctx, principal.AccountID, s.Clock.Now().UTC())
}

// Create founds an alliance, whose founder is the account itself.
func (s Service) Create(ctx context.Context, principal appauth.Principal, name, tag, description string) (Profile, error) {
	if err := s.validate(principal); err != nil {
		return Profile{}, err
	}
	normalizedName, err := domainalliance.NormalizeName(name)
	if err != nil {
		return Profile{}, err
	}
	normalizedTag, err := domainalliance.NormalizeTag(tag)
	if err != nil {
		return Profile{}, err
	}
	return s.Repository.Create(ctx, principal.AccountID, normalizedName, normalizedTag,
		strings.TrimSpace(description), s.Clock.Now().UTC())
}

// Invite offers a seat to a player who belongs to no alliance.
func (s Service) Invite(ctx context.Context, principal appauth.Principal, playerName string) error {
	if err := s.validate(principal); err != nil {
		return err
	}
	if strings.TrimSpace(playerName) == "" {
		return ErrNoSuchPlayer
	}
	return s.Repository.Invite(ctx, principal.AccountID, strings.TrimSpace(playerName), s.Clock.Now().UTC())
}

// Accept joins the alliance of an invitation. Accepting twice changes nothing.
func (s Service) Accept(ctx context.Context, principal appauth.Principal, invitationID int64) error {
	if err := s.validate(principal); err != nil {
		return err
	}
	if invitationID <= 0 {
		return ErrNoInvitation
	}
	return s.Repository.Accept(ctx, principal.AccountID, invitationID, s.Clock.Now().UTC())
}

// Decline refuses an invitation. Refusing twice changes nothing.
func (s Service) Decline(ctx context.Context, principal appauth.Principal, invitationID int64) error {
	if err := s.validate(principal); err != nil {
		return err
	}
	if invitationID <= 0 {
		return ErrNoInvitation
	}
	return s.Repository.Decline(ctx, principal.AccountID, invitationID, s.Clock.Now().UTC())
}

// Leave gives up membership. A founder must hand the charge over first.
func (s Service) Leave(ctx context.Context, principal appauth.Principal) error {
	if err := s.validate(principal); err != nil {
		return err
	}
	return s.Repository.Leave(ctx, principal.AccountID, s.Clock.Now().UTC())
}

// Expel removes a member, never one of a higher rank.
func (s Service) Expel(ctx context.Context, principal appauth.Principal, playerID int64) error {
	if err := s.validate(principal); err != nil {
		return err
	}
	if playerID <= 0 {
		return ErrNoSuchPlayer
	}
	return s.Repository.Expel(ctx, principal.AccountID, playerID, s.Clock.Now().UTC())
}

// Promote changes the role of a member, the founder charge included.
func (s Service) Promote(ctx context.Context, principal appauth.Principal, playerID int64, role domainalliance.Role) error {
	if err := s.validate(principal); err != nil {
		return err
	}
	if playerID <= 0 {
		return ErrNoSuchPlayer
	}
	if !role.Valid() {
		return domainalliance.ErrUnknownRole
	}
	return s.Repository.Promote(ctx, principal.AccountID, playerID, role, s.Clock.Now().UTC())
}

// Declare states an intention towards another alliance.
func (s Service) Declare(ctx context.Context, principal appauth.Principal, tag string, relation domainalliance.Relation) error {
	if err := s.validate(principal); err != nil {
		return err
	}
	normalizedTag, err := domainalliance.NormalizeTag(tag)
	if err != nil {
		return err
	}
	if !relation.Valid() {
		return domainalliance.ErrUnknownRelation
	}
	return s.Repository.Declare(ctx, principal.AccountID, normalizedTag, relation, s.Clock.Now().UTC())
}

// Describe rewrites the public description of the alliance.
func (s Service) Describe(ctx context.Context, principal appauth.Principal, description string) error {
	if err := s.validate(principal); err != nil {
		return err
	}
	return s.Repository.Describe(ctx, principal.AccountID, strings.TrimSpace(description), s.Clock.Now().UTC())
}

func (s Service) validate(principal appauth.Principal) error {
	if s.Clock == nil || s.Repository == nil {
		return errors.New("alliance: incomplete service dependencies")
	}
	if principal.AccountID <= 0 || principal.MustChangePassword {
		return ErrForbidden
	}
	return nil
}

// ensure the economy errors stay part of the contract of this package.
var _ = appeconomy.ErrNoEmpire
