package tests

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appreports "universeatwar/internal/app/reports"
	appclock "universeatwar/internal/clock"
	"universeatwar/internal/domain/combat"
	"universeatwar/internal/domain/random"
	"universeatwar/internal/domain/rules"
	"universeatwar/internal/domain/unit"
	storagesqlite "universeatwar/internal/storage/sqlite"
)

// TestCombatCanBeReplayedFromItsStoredSeed proves the battle is auditable: the
// report, the compositions and the seed are enough to obtain the same outcome.
func TestCombatCanBeReplayedFromItsStoredSeed(t *testing.T) {
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC))
	database, universe, _, attack := launchedAttack(t, ctx, clock)

	clock.Set(attack.ArrivesAt)
	if _, err := universe.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}

	var seed int64
	var outcome string
	if err := database.Read().QueryRowContext(ctx, "SELECT seed FROM fleets WHERE id = 1").Scan(&seed); err != nil {
		t.Fatal(err)
	}
	if err := database.Read().QueryRowContext(ctx,
		`SELECT json_extract(payload, '$.outcome') FROM game_event_log WHERE event_type = 'combat_resolved'`).Scan(&outcome); err != nil {
		t.Fatal(err)
	}
	var document string
	if err := database.Read().QueryRowContext(ctx,
		"SELECT payload FROM reports WHERE kind = 'combat_attack'").Scan(&document); err != nil {
		t.Fatal(err)
	}
	var stored struct {
		Attackers []struct {
			Initial map[string]int64 `json:"initial"`
		} `json:"attackers"`
		Defenders []struct {
			Initial map[string]int64 `json:"initial"`
		} `json:"defenders"`
	}
	if err := json.Unmarshal([]byte(document), &stored); err != nil {
		t.Fatal(err)
	}

	replayed, err := combat.Resolve(combat.Input{
		Attackers:   []combat.Party{{PlayerID: 1, Units: unitsOf(stored.Attackers[0].Initial), Technologies: combat.Factors{Weapons: 1, Shield: 1, Armour: 1}}},
		Defenders:   []combat.Party{{PlayerID: 2, Units: unitsOf(stored.Defenders[0].Initial), Technologies: combat.Factors{Weapons: 1, Shield: 1, Armour: 1}}},
		Rules:       rules.Default().Combat,
		Multipliers: combat.CostMultipliers{Ship: 1, Defense: 1},
		Catalogue:   unit.DefaultCatalogue(),
	}, random.NewSeeded(uint64(seed)))
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if string(replayed.Outcome) != outcome {
		t.Fatalf("replay outcome = %q, want %q", replayed.Outcome, outcome)
	}
}

func unitsOf(document map[string]int64) map[unit.ID]int64 {
	units := make(map[unit.ID]int64, len(document))
	for id, quantity := range document {
		units[unit.ID(id)] = quantity
	}
	return units
}

func BenchmarkReportsPage(b *testing.B) {
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC))
	database := benchmarkUniverse(b, ctx)
	game := newBenchmarkWorld(b, database, clock)
	if _, err := game.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: 1}, "Alice"); err != nil {
		b.Fatal(err)
	}
	reports := storagesqlite.NewReportsRepository(database.Read(), database.Write())
	for index := range 5000 {
		if _, err := database.Write().ExecContext(ctx, `
			INSERT INTO reports(recipient_player_id, kind, subject_type, subject_id, galaxy, system, position,
				occurred_at, payload_version, payload, created_at)
			VALUES (1, 'espionage', 'planet', 1, 1, 1, ?, '2042-09-10T11:12:13Z', 1, '{}', '2042-09-10T11:12:13Z')
		`, index%15+1); err != nil {
			b.Fatal(err)
		}
	}
	for b.Loop() {
		if _, err := reports.List(ctx, 1, appreports.Filter{Page: 1}, clock.Now()); err != nil {
			b.Fatal(err)
		}
	}
}
