// Package alliance holds the rules of a persistent team: who may do what, and
// what a name looks like. It knows nothing of storage or of the web.
package alliance

import (
	"errors"
	"regexp"
	"strings"
)

var (
	ErrInvalidName     = errors.New("alliance: a name holds 3 to 32 characters")
	ErrInvalidTag      = errors.New("alliance: a tag holds 2 to 8 characters among A-Z and 0-9")
	ErrForbidden       = errors.New("alliance: this role cannot do that")
	ErrUnknownRole     = errors.New("alliance: unknown role")
	ErrUnknownRelation = errors.New("alliance: unknown diplomatic relation")
)

// Role is the rank a member holds inside the alliance. It never grants any
// authority over the server itself.
type Role string

const (
	Founder Role = "founder"
	Officer Role = "officer"
	Member  Role = "member"
)

// Permission is one thing a member may attempt.
type Permission string

const (
	Invite         Permission = "invite"
	Expel          Permission = "expel"
	EditProfile    Permission = "edit_profile"
	Diplomacy      Permission = "diplomacy"
	LeadOperation  Permission = "lead_operation"
	TransferCharge Permission = "transfer_charge"
)

// Relation is a declared intention towards another alliance. It announces, it
// does not change any combat rule.
type Relation string

const (
	Pact Relation = "pact"
	War  Relation = "war"
)

var tagPattern = regexp.MustCompile(`^[A-Z0-9]{2,8}$`)

// Valid reports whether the role is one this build knows.
func (r Role) Valid() bool {
	switch r {
	case Founder, Officer, Member:
		return true
	default:
		return false
	}
}

// Can reports whether a role may perform an action.
func (r Role) Can(permission Permission) bool {
	switch r {
	case Founder:
		return true
	case Officer:
		return permission == Invite || permission == Expel || permission == LeadOperation
	default:
		return false
	}
}

// CanExpel reports whether a role may expel a member of another role. Expelling
// never climbs the hierarchy.
func (r Role) CanExpel(target Role) bool {
	if !r.Can(Expel) || !target.Valid() {
		return false
	}
	if r == Founder {
		return target != Founder
	}
	return target == Member
}

// Valid reports whether the relation is one this build knows.
func (rel Relation) Valid() bool {
	return rel == Pact || rel == War
}

// NormalizeName trims a name and checks its length.
func NormalizeName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if length := len([]rune(trimmed)); length < 3 || length > 32 {
		return "", ErrInvalidName
	}
	return trimmed, nil
}

// NormalizeTag upper-cases a tag and checks its shape.
func NormalizeTag(tag string) (string, error) {
	trimmed := strings.ToUpper(strings.TrimSpace(tag))
	if !tagPattern.MatchString(trimmed) {
		return "", ErrInvalidTag
	}
	return trimmed, nil
}
