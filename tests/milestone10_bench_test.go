package tests

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appclock "universeatwar/internal/clock"
	"universeatwar/internal/observability"
	storagesqlite "universeatwar/internal/storage/sqlite"
)

// BenchmarkGalaxyPage measures the public map of a busy system, which is the
// page every player opens the most.
func BenchmarkGalaxyPage(b *testing.B) {
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC))
	database := benchmarkUniverse(b, ctx)
	universeWorld := newWorld(b, database, clock)
	if _, err := universeWorld.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: 1}, "Alice"); err != nil {
		b.Fatal(err)
	}
	// A full system, with a debris field on every position.
	for position := 1; position <= 15; position++ {
		if _, err := database.Write().ExecContext(ctx, `
			INSERT OR IGNORE INTO planets(owner_player_id, name, galaxy, system, position, total_fields,
				minimum_temperature, maximum_temperature, created_at)
			VALUES (1, ?, 1, 1, ?, 150, 10, 50, '2042-09-10T12:00:00Z')
		`, fmt.Sprintf("Colonie %d", position), position); err != nil {
			b.Fatal(err)
		}
		if _, err := database.Write().ExecContext(ctx, `
			INSERT OR IGNORE INTO debris_fields(galaxy, system, position, metal, crystal, created_at, updated_at)
			VALUES (1, 1, ?, 5000, 2500, '2042-09-10T12:00:00Z', '2042-09-10T12:00:00Z')
		`, position); err != nil {
			b.Fatal(err)
		}
	}
	principal := appauth.Principal{AccountID: 1}
	b.ResetTimer()
	for b.Loop() {
		if _, err := universeWorld.Galaxy.System(ctx, principal, 1, 1); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkEventStorm measures a backlog of thousands of due events, which is
// what a server meets after being off for a while.
func BenchmarkEventStorm(b *testing.B) {
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC))
	database := benchmarkUniverse(b, ctx)
	events := storagesqlite.NewEventProcessor(database.Write(), clock)
	events.Metrics = observability.NewMetrics()
	applied := 0
	events.Register("storm", func(context.Context, *sql.Tx, storagesqlite.ScheduledEvent, time.Time) error {
		applied++
		return nil
	})
	b.ResetTimer()
	for b.Loop() {
		b.StopTimer()
		if _, err := database.Write().ExecContext(ctx, "DELETE FROM scheduled_events"); err != nil {
			b.Fatal(err)
		}
		for index := 0; index < 2000; index++ {
			if _, err := database.Write().ExecContext(ctx, `
				INSERT INTO scheduled_events(event_type, due_at, priority, entity_type, entity_id, idempotency_key, created_at)
				VALUES ('storm', '2042-09-10T11:00:00Z', 50, 'test', '1', ?, '2042-09-10T11:00:00Z')
			`, fmt.Sprintf("storm-%d-%d", b.N, index)); err != nil {
				b.Fatal(err)
			}
		}
		b.StartTimer()
		if _, err := events.CompleteDue(ctx, 2000); err != nil {
			b.Fatal(err)
		}
	}
}
