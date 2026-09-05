package tests

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	appclock "universeatwar/internal/clock"
	storagesqlite "universeatwar/internal/storage/sqlite"
)

func TestEventProcessorDispatchesByTypeInDueOrder(t *testing.T) {
	ctx := context.Background()
	database := economyDatabase(t, ctx, 1)
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC))
	processor := storagesqlite.NewEventProcessor(database.Write(), clock)

	var order []string
	record := func(name string) storagesqlite.EventHandler {
		return func(_ context.Context, _ *sql.Tx, event storagesqlite.ScheduledEvent, _ time.Time) error {
			order = append(order, name+":"+event.EntityID)
			return nil
		}
	}
	processor.Register("alpha", record("alpha"))
	processor.Register("beta", record("beta"))
	insertScheduledEvent(t, database, "beta", "2042-09-10T11:00:00Z", 70, "1", "b1")
	insertScheduledEvent(t, database, "alpha", "2042-09-10T11:00:00Z", 50, "2", "a1")
	insertScheduledEvent(t, database, "alpha", "2042-09-10T10:00:00Z", 50, "3", "a0")
	insertScheduledEvent(t, database, "alpha", "2042-09-10T13:00:00Z", 50, "4", "future")

	processed, err := processor.CompleteDue(ctx, 10)
	if err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}
	if processed != 3 {
		t.Fatalf("processed = %d, want 3", processed)
	}
	want := []string{"alpha:3", "alpha:2", "beta:1"}
	if len(order) != len(want) {
		t.Fatalf("handler order = %v, want %v", order, want)
	}
	for index := range want {
		if order[index] != want[index] {
			t.Fatalf("handler order = %v, want %v", order, want)
		}
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM scheduled_events WHERE state = 'completed'", 3)

	next, ok, err := processor.NextDue(ctx)
	if err != nil || !ok {
		t.Fatalf("NextDue() = %v %v %v", next, ok, err)
	}
	if !next.Equal(time.Date(2042, time.September, 10, 13, 0, 0, 0, time.UTC)) {
		t.Fatalf("NextDue() = %v, want the pending future event", next)
	}
}

func TestEventProcessorRecordsFailuresAndSkipsPoisonedEvents(t *testing.T) {
	ctx := context.Background()
	database := economyDatabase(t, ctx, 1)
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC))
	processor := storagesqlite.NewEventProcessor(database.Write(), clock)

	calls := 0
	processor.Register("failing", func(handlerContext context.Context, transaction *sql.Tx, _ storagesqlite.ScheduledEvent, _ time.Time) error {
		calls++
		if _, err := transaction.ExecContext(handlerContext, insertSideEffect); err != nil {
			return err
		}
		return errors.New("simulated crash before commit")
	})
	insertScheduledEvent(t, database, "failing", "2042-09-10T11:00:00Z", 50, "1", "f1")
	insertScheduledEvent(t, database, "unknown_type", "2042-09-10T11:30:00Z", 50, "2", "u1")

	processed, err := processor.CompleteDue(ctx, 100)
	if err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}
	if processed != 0 {
		t.Fatalf("processed = %d, want 0", processed)
	}
	if calls != 5 {
		t.Fatalf("handler calls = %d, want 5 attempts", calls)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM game_event_log WHERE event_type = 'side_effect'", 0)
	assertSingleValue(t, database, "SELECT attempts FROM scheduled_events WHERE idempotency_key = 'f1'", 5)
	assertSingleValue(t, database, "SELECT attempts FROM scheduled_events WHERE idempotency_key = 'u1'", 5)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM scheduled_events WHERE state = 'pending' AND last_error IS NOT NULL", 2)
	if _, ok, err := processor.NextDue(ctx); err != nil || ok {
		t.Fatalf("NextDue() = %v %v, want no pending work once events are poisoned", ok, err)
	}
}

func TestScenarioFCrashBeforeCommitReplaysExactlyOnce(t *testing.T) {
	ctx := context.Background()
	database := economyDatabase(t, ctx, 1)
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC))
	processor := storagesqlite.NewEventProcessor(database.Write(), clock)

	attempt := 0
	processor.Register("effect", func(handlerContext context.Context, transaction *sql.Tx, _ storagesqlite.ScheduledEvent, _ time.Time) error {
		attempt++
		if _, err := transaction.ExecContext(handlerContext, insertAppliedEffect); err != nil {
			return err
		}
		if attempt == 1 {
			return errors.New("crash before commit")
		}
		return nil
	})
	insertScheduledEvent(t, database, "effect", "2042-09-10T11:00:00Z", 50, "1", "e1")

	if _, err := processor.CompleteDue(ctx, 100); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM game_event_log WHERE event_type = 'effect_applied'", 1)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM scheduled_events WHERE state = 'completed'", 1)

	if _, err := processor.CompleteDue(ctx, 100); err != nil {
		t.Fatalf("second CompleteDue() error = %v", err)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM game_event_log WHERE event_type = 'effect_applied'", 1)
}

const (
	insertSideEffect = `
		INSERT INTO game_event_log(event_type, entity_type, entity_id, occurred_at)
		VALUES ('side_effect', 'test', '1', '2042-09-10T12:00:00Z')
	`
	insertAppliedEffect = `
		INSERT INTO game_event_log(event_type, entity_type, entity_id, occurred_at)
		VALUES ('effect_applied', 'test', '1', '2042-09-10T12:00:00Z')
	`
)

func insertScheduledEvent(t *testing.T, database *storagesqlite.Database, eventType, dueAt string, priority int, entityID, key string) {
	t.Helper()
	if _, err := database.Write().ExecContext(context.Background(), `
		INSERT INTO scheduled_events(event_type, due_at, priority, entity_type, entity_id, idempotency_key, created_at)
		VALUES (?, ?, ?, 'test', ?, ?, ?)
	`, eventType, dueAt, priority, entityID, key, dueAt); err != nil {
		t.Fatalf("insert scheduled event %q: %v", key, err)
	}
}
