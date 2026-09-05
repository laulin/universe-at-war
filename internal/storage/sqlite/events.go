// Package sqlite persists every mutation inside short serialized transactions.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	domainclock "universeatwar/internal/domain/clock"
)

// ErrUnknownEventType reports a scheduled event whose type has no handler.
var ErrUnknownEventType = errors.New("sqlite: no handler registered for event type")

// ScheduledEvent is the durable envelope handed to a handler.
type ScheduledEvent struct {
	ID             int64
	Type           string
	DueAt          time.Time
	Priority       int
	EntityType     string
	EntityID       string
	RulesetVersion int64
	PayloadVersion int
	Payload        []byte
	Attempts       int
}

// EventHandler applies exactly one due event inside the transaction that
// selected it. Returning nil marks the event completed; returning an error
// rolls the whole transaction back. A handler whose aggregate is already in a
// terminal state must return nil so that a redelivery stays a no-op.
type EventHandler func(ctx context.Context, transaction *sql.Tx, event ScheduledEvent, now time.Time) error

// EventProcessor selects the next due event of any type and delegates it to the
// handler registered for that type.
type EventProcessor struct {
	write       *sql.DB
	clock       domainclock.Clock
	handlers    map[string]EventHandler
	MaxAttempts int
	Logger      *slog.Logger
}

// NewEventProcessor builds a processor without any handler.
func NewEventProcessor(write *sql.DB, clock domainclock.Clock) *EventProcessor {
	return &EventProcessor{
		write:       write,
		clock:       clock,
		handlers:    map[string]EventHandler{},
		MaxAttempts: 5,
		Logger:      slog.New(slog.DiscardHandler),
	}
}

// Register binds a handler to an event type. Registering twice is a programming
// error because two handlers would both claim the same events.
func (p *EventProcessor) Register(eventType string, handler EventHandler) {
	if _, exists := p.handlers[eventType]; exists {
		panic("sqlite: duplicate event handler for " + eventType)
	}
	p.handlers[eventType] = handler
}

// NextDue returns the deadline of the next event still worth processing.
func (p *EventProcessor) NextDue(ctx context.Context) (time.Time, bool, error) {
	var value string
	err := p.write.QueryRowContext(ctx, `
		SELECT due_at FROM scheduled_events
		WHERE state = 'pending' AND attempts < ?
		ORDER BY due_at, priority, id LIMIT 1
	`, p.MaxAttempts).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("event processor: next due event: %w", err)
	}
	dueAt, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("event processor: parse next due event: %w", err)
	}
	return dueAt, true, nil
}

// CompleteDue applies due events until the limit is reached or nothing is left.
func (p *EventProcessor) CompleteDue(ctx context.Context, limit int) (int, error) {
	if p.write == nil || p.clock == nil || limit <= 0 {
		return 0, errors.New("event processor: incomplete dependencies")
	}
	completed := 0
	for completed < limit {
		now := p.clock.Now().UTC()
		event, found, err := p.selectDue(ctx, now)
		if err != nil {
			return completed, err
		}
		if !found {
			return completed, nil
		}
		applied, err := p.apply(ctx, event, now)
		if err != nil {
			return completed, err
		}
		if applied {
			completed++
		}
	}
	return completed, nil
}

func (p *EventProcessor) selectDue(ctx context.Context, now time.Time) (ScheduledEvent, bool, error) {
	var event ScheduledEvent
	var dueText, payload string
	var rulesetVersion sql.NullInt64
	err := p.write.QueryRowContext(ctx, `
		SELECT id, event_type, due_at, priority, entity_type, entity_id, ruleset_version, payload_version, payload, attempts
		FROM scheduled_events
		WHERE state = 'pending' AND due_at <= ? AND attempts < ?
		ORDER BY due_at, priority, id LIMIT 1
	`, timestamp(now), p.MaxAttempts).Scan(
		&event.ID, &event.Type, &dueText, &event.Priority, &event.EntityType,
		&event.EntityID, &rulesetVersion, &event.PayloadVersion, &payload, &event.Attempts,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ScheduledEvent{}, false, nil
	}
	if err != nil {
		return ScheduledEvent{}, false, fmt.Errorf("event processor: select due event: %w", err)
	}
	event.DueAt, err = time.Parse(time.RFC3339Nano, dueText)
	if err != nil {
		return ScheduledEvent{}, false, fmt.Errorf("event processor: parse due time: %w", err)
	}
	event.RulesetVersion = rulesetVersion.Int64
	event.Payload = []byte(payload)
	return event, true, nil
}

// apply runs the handler in one transaction. A failure is recorded in a short
// separate transaction so a poisoned event is eventually skipped instead of
// stopping the whole simulation.
func (p *EventProcessor) apply(ctx context.Context, event ScheduledEvent, now time.Time) (bool, error) {
	handler, known := p.handlers[event.Type]
	err := ErrUnknownEventType
	if known {
		err = withWriteTx(ctx, p.write, "event processor", func(transaction *sql.Tx) error {
			if handlerErr := handler(ctx, transaction, event, now); handlerErr != nil {
				return handlerErr
			}
			result, execErr := transaction.ExecContext(ctx, `
				UPDATE scheduled_events
				SET state = 'completed', processed_at = ?, attempts = attempts + 1, last_error = NULL
				WHERE id = ? AND state = 'pending'
			`, timestamp(now), event.ID)
			if execErr != nil {
				return fmt.Errorf("event processor: complete event: %w", execErr)
			}
			affected, execErr := result.RowsAffected()
			if execErr != nil {
				return fmt.Errorf("event processor: complete event: %w", execErr)
			}
			if affected != 1 {
				return errors.New("event processor: event changed during processing")
			}
			return nil
		})
	}
	if err == nil {
		return true, nil
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	p.Logger.Error("scheduled event failed",
		"event_id", event.ID, "event_type", event.Type,
		"entity_id", event.EntityID, "attempt", event.Attempts+1, "error", err.Error())
	if _, updateErr := p.write.ExecContext(ctx, `
		UPDATE scheduled_events SET attempts = attempts + 1, last_error = ?
		WHERE id = ? AND state = 'pending'
	`, err.Error(), event.ID); updateErr != nil {
		return false, fmt.Errorf("event processor: record failure: %w", updateErr)
	}
	return false, nil
}
