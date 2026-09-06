package ai

import (
	"context"
	"errors"
	"time"

	domainai "universeatwar/internal/domain/ai"
	domainclock "universeatwar/internal/domain/clock"
)

// Thoughts is the persistence an artificial player needs to think: who owes a
// reflection, and where its conclusions go.
type Thoughts interface {
	Due(context.Context, time.Time, int) ([]domainai.Profile, error)
	Complete(context.Context, int64, []domainai.Decision, time.Time) error
}

// Thinking hands the brain the players that owe a reflection and writes back
// what they decided. It grants no reading of anybody else's truth.
type Thinking struct {
	Clock   domainclock.Clock
	Thought Thoughts
}

// Due lists the artificial players whose reflection has come round, at most
// `limit` of them so one batch never runs away.
func (t Thinking) Due(ctx context.Context, limit int) ([]domainai.Profile, error) {
	if t.Clock == nil || t.Thought == nil {
		return nil, errors.New("ai: incomplete thinking dependencies")
	}
	if limit <= 0 {
		return nil, nil
	}
	return t.Thought.Due(ctx, t.Clock.Now().UTC(), limit)
}

// Complete records what a reflection concluded and closes it.
func (t Thinking) Complete(ctx context.Context, playerID int64, decisions []domainai.Decision) error {
	if t.Clock == nil || t.Thought == nil {
		return errors.New("ai: incomplete thinking dependencies")
	}
	if playerID <= 0 {
		return ErrNotFound
	}
	return t.Thought.Complete(ctx, playerID, decisions, t.Clock.Now().UTC())
}
