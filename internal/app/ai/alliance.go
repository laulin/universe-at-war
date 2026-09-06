package ai

import (
	"context"
	"errors"
	"time"

	domainai "universeatwar/internal/domain/ai"
)

// Shared is the common memory of an alliance of artificial players. It holds
// only what a member observed and shared on purpose, and it forgets a belief as
// soon as the report behind it stops being shared.
type Shared interface {
	Publish(context.Context, int64, int64, []domainai.Knowledge) error
	Recall(context.Context, int64, time.Time) ([]domainai.Knowledge, error)
	AllianceOf(context.Context, int64) (Alliance, error)
	AssignRoles(context.Context, int64, map[int64]domainai.Role, time.Time) error
	Roles(context.Context, int64) (map[int64]domainai.Role, error)
	OpenObjective(context.Context, int64, domainai.Objective) (domainai.Objective, error)
	Objective(context.Context, int64) (domainai.Objective, bool, error)
	AdvanceObjective(context.Context, domainai.Objective, domainai.ObjectiveState, int64, string, time.Time) error
}

// Alliance is what an artificial player knows of its own team: who is in it,
// and who leads an operation.
type Alliance struct {
	ID      int64
	Name    string
	Tag     string
	Members []Member
}

// Member is one player of the alliance, artificial or not.
type Member struct {
	PlayerID  int64
	AccountID int64
	Name      string
	Leads     bool
	// Artificial says whether this member is driven by the server. Only those
	// take part in a plan decided by a machine.
	Artificial bool
}

// Leader is the member that carries an operation for the alliance: the oldest
// one allowed to lead. An alliance without a leader plans nothing.
func (a Alliance) Leader() (Member, bool) {
	for _, member := range a.Members {
		if member.Leads && member.Artificial {
			return member, true
		}
	}
	return Member{}, false
}

// Teamwork gives the brain the common memory of an alliance and takes its
// contributions back. It reads nobody's truth: every belief it returns was
// shared by a member.
type Teamwork struct {
	Shared Shared
}

// Alliance returns the team of an artificial player, or nothing when it has
// none.
func (t Teamwork) Alliance(ctx context.Context, playerID int64) (Alliance, bool, error) {
	if t.Shared == nil {
		return Alliance{}, false, errors.New("ai: incomplete teamwork dependencies")
	}
	alliance, err := t.Shared.AllianceOf(ctx, playerID)
	if errors.Is(err, ErrNotFound) {
		return Alliance{}, false, nil
	}
	if err != nil {
		return Alliance{}, false, err
	}
	return alliance, true, nil
}

// Publish adds what a member observed to the common memory.
func (t Teamwork) Publish(ctx context.Context, allianceID, authorID int64, knowledge []domainai.Knowledge) error {
	if t.Shared == nil {
		return errors.New("ai: incomplete teamwork dependencies")
	}
	if len(knowledge) == 0 {
		return nil
	}
	return t.Shared.Publish(ctx, allianceID, authorID, knowledge)
}

// Recall reads what the alliance still believes at that instant.
func (t Teamwork) Recall(ctx context.Context, allianceID int64, now time.Time) ([]domainai.Knowledge, error) {
	if t.Shared == nil {
		return nil, errors.New("ai: incomplete teamwork dependencies")
	}
	return t.Shared.Recall(ctx, allianceID, now)
}

// AssignRoles writes down who does what for the alliance.
func (t Teamwork) AssignRoles(ctx context.Context, allianceID int64, roles map[int64]domainai.Role, now time.Time) error {
	if t.Shared == nil {
		return errors.New("ai: incomplete teamwork dependencies")
	}
	return t.Shared.AssignRoles(ctx, allianceID, roles, now)
}

// Roles reads who does what for the alliance.
func (t Teamwork) Roles(ctx context.Context, allianceID int64) (map[int64]domainai.Role, error) {
	if t.Shared == nil {
		return nil, errors.New("ai: incomplete teamwork dependencies")
	}
	return t.Shared.Roles(ctx, allianceID)
}

// OpenObjective starts the single plan an alliance pursues.
func (t Teamwork) OpenObjective(ctx context.Context, allianceID int64, objective domainai.Objective) (domainai.Objective, error) {
	if t.Shared == nil {
		return domainai.Objective{}, errors.New("ai: incomplete teamwork dependencies")
	}
	return t.Shared.OpenObjective(ctx, allianceID, objective)
}

// Objective returns the plan an alliance is pursuing, if it has one.
func (t Teamwork) Objective(ctx context.Context, allianceID int64) (domainai.Objective, bool, error) {
	if t.Shared == nil {
		return domainai.Objective{}, false, errors.New("ai: incomplete teamwork dependencies")
	}
	return t.Shared.Objective(ctx, allianceID)
}

// AdvanceObjective moves a plan along, or gives it up with a reason.
func (t Teamwork) AdvanceObjective(ctx context.Context, objective domainai.Objective,
	to domainai.ObjectiveState, groupID int64, reason string, now time.Time) error {
	if t.Shared == nil {
		return errors.New("ai: incomplete teamwork dependencies")
	}
	if !domainai.CanAdvance(objective.State, to) {
		return ErrInvalidRequest
	}
	return t.Shared.AdvanceObjective(ctx, objective, to, groupID, reason, now)
}
