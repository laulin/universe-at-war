# Partie 0 — Socle transverse

> **Pour les agents d'implémentation :** exécuter tâche par tâche, chaque tâche
> laissant l'arbre compilable et les tests verts. Les étapes utilisent la syntaxe
> `- [ ]` pour le suivi. La feuille de route et les décisions transverses se
> trouvent dans [`2026-09-05-roadmap-m3-m10.md`](2026-09-05-roadmap-m3-m10.md).

Objectif : lever les six limites listées plus haut sans changer le comportement observable du M2. Chaque tâche laisse `make check` vert. Ordre obligatoire : F0 → F1 → F2 → F3 → F4 → F5 → F6 → F7.

### Task F0 : Publier les plans dans le dépôt

**Files:**
- Create: `docs/plans/README.md`
- Create: `docs/plans/2026-09-05-roadmap-m3-m10.md` (en-tête, contexte, écarts, décisions, feuille de route de ce document)
- Create: `docs/plans/2026-09-05-partie-0-socle.md`, `docs/plans/2026-09-05-milestone-03.md`, `docs/plans/2026-09-05-milestone-04.md`, `docs/plans/2026-09-05-milestone-05.md` (une partie de ce document par fichier, en-tête `> For agentic workers` répété)
- Modify: `README.md` (paragraphe « Plans d'implémentation » pointant vers `docs/plans/README.md`)

- [ ] **Step 1: Créer l'index**

```markdown
# Plans d'implémentation

| Fichier | Contenu |
| --- | --- |
| `2026-09-05-roadmap-m3-m10.md` | écarts spec/code, décisions transverses D1–D10, feuille de route |
| `2026-09-05-partie-0-socle.md` | tâches F0–F7 (dispatch d'événements, ruleset évolutif, multi-planètes, layout, inscription, logs) |
| `2026-09-05-milestone-03.md`, `-04.md`, `-05.md` | tâches TDD par jalon (M6–M10 : à rédiger au démarrage de chaque jalon) |

Exécution : `superpowers:subagent-driven-development`, une tâche = un commit.
```

- [ ] **Step 2: Scinder ce document** en respectant les titres de parties ; ne rien reformuler.

- [ ] **Step 3: Vérifier** `test -z "$(gofmt -l .)" && go build ./...` (aucun code touché, sanity).

- [ ] **Step 4: Commit**

```bash
git add docs/plans README.md
git commit -m "docs: publish implementation plans for milestones 3 to 10"
```

---

### Task F1 : Helper transactionnel `withWriteTx`

**Files:**
- Create: `internal/storage/sqlite/tx.go`
- Test: `internal/storage/sqlite/tx_test.go`

**Interfaces:**
- Produces: `func withWriteTx(ctx context.Context, db *sql.DB, operation string, fn func(*sql.Tx) error) error` — ouvre `BeginTx`, `Rollback` différé, `Commit` si `fn` renvoie `nil`. Les erreurs de `fn` sont renvoyées telles quelles (pour `errors.Is`), les erreurs techniques sont préfixées `"<operation>: begin|commit: "`.

- [ ] **Step 1: Écrire le test rouge**

```go
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func TestWithWriteTxRollsBackOnErrorAndCommitsOnSuccess(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "tx.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("boom")
	err = withWriteTx(ctx, database.Write(), "test", func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "INSERT INTO idempotency_keys(actor_id, operation, key, request_hash, created_at) VALUES ('1', 'op', 'k', 'h', '2042-01-01T00:00:00Z')"); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want sentinel", err)
	}
	var count int
	if err := database.Read().QueryRowContext(ctx, "SELECT COUNT(*) FROM idempotency_keys").Scan(&count); err != nil || count != 0 {
		t.Fatalf("rows after rollback = %d (%v), want 0", count, err)
	}
	err = withWriteTx(ctx, database.Write(), "test", func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO idempotency_keys(actor_id, operation, key, request_hash, created_at) VALUES ('1', 'op', 'k', 'h', '2042-01-01T00:00:00Z')")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Read().QueryRowContext(ctx, "SELECT COUNT(*) FROM idempotency_keys").Scan(&count); err != nil || count != 1 {
		t.Fatalf("rows after commit = %d (%v), want 1", count, err)
	}
}
```

- [ ] **Step 2: Vérifier l'échec** — `go test ./internal/storage/sqlite/ -run TestWithWriteTx -v` → `undefined: withWriteTx`.

- [ ] **Step 3: Implémenter**

```go
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
)

// withWriteTx runs fn inside one serialized write transaction and commits only
// when fn succeeds. Business errors returned by fn are passed through unchanged.
func withWriteTx(ctx context.Context, db *sql.DB, operation string, fn func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%s: begin: %w", operation, err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%s: commit: %w", operation, err)
	}
	return nil
}
```

- [ ] **Step 4: Vérifier** — `go test ./internal/storage/sqlite/ -run TestWithWriteTx -v` → PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/storage/sqlite/tx.go internal/storage/sqlite/tx_test.go
git commit -m "refactor: add shared write transaction helper"
```

---

### Task F2 : Dispatcher d'événements par type

**Files:**
- Create: `internal/storage/sqlite/events.go`
- Modify: `internal/storage/sqlite/economy.go` (supprimer `NextDue`, `CompleteDue`, transformer `completeOne` en handler)
- Modify: `internal/app/economy/service.go` (retirer `NextDue`/`CompleteDue`, ajouter le port `Completer`)
- Modify: `internal/cli/cli.go:192-198` (câblage)
- Modify: `docs/architecture/scheduled-events.md` (table des priorités, politique d'échec)
- Test: `tests/events_test.go`, mise à jour de `tests/economy_test.go` et `tests/web_economy_test.go` (construction du service)

**Interfaces:**
- Consumes: `withWriteTx` (F1), `simulation.Processor` (existant).
- Produces:

```go
package sqlite

type ScheduledEvent struct {
	ID             int64
	Type           string
	DueAt          time.Time
	Priority       int
	EntityType     string
	EntityID       string
	RulesetVersion int64 // 0 si NULL
	PayloadVersion int
	Payload        []byte
	Attempts       int
}

// EventHandler applies one due event inside the processor's transaction.
// Returning nil marks the event completed; returning an error rolls back.
type EventHandler func(ctx context.Context, tx *sql.Tx, event ScheduledEvent, now time.Time) error

type EventProcessor struct { /* write *sql.DB; clock; handlers map[string]EventHandler; MaxAttempts int (5); Logger *slog.Logger */ }

func NewEventProcessor(write *sql.DB, clock domainclock.Clock) *EventProcessor
func (p *EventProcessor) Register(eventType string, handler EventHandler)          // panique si doublon
func (p *EventProcessor) CompleteDue(ctx context.Context, limit int) (int, error)  // simulation.Processor
func (p *EventProcessor) NextDue(ctx context.Context) (time.Time, bool, error)     // simulation.Processor, tous types
func (p *EventProcessor) ProcessOne(ctx context.Context, now time.Time) (bool, error)
var ErrUnknownEventType = errors.New("sqlite: no handler registered for event type")
```

  et côté économie : `func (r *EconomyRepository) RegisterHandlers(processor *EventProcessor)` (enregistre `building_completed`), `appeconomy.Completer interface{ CompleteDue(context.Context, int) (int, error) }`, champ `Service.Completer Completer`.

- [ ] **Step 1: Écrire les tests rouges** (`tests/events_test.go`)

```go
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

func insertEvent(t *testing.T, database *storagesqlite.Database, eventType, dueAt string, priority int, key string) {
	t.Helper()
	if _, err := database.Write().ExecContext(context.Background(), `INSERT INTO scheduled_events(event_type, due_at, priority, entity_type, entity_id, idempotency_key, created_at) VALUES (?, ?, ?, 'test', '1', ?, ?)`, eventType, dueAt, priority, key, dueAt); err != nil {
		t.Fatal(err)
	}
}

func TestEventProcessorDispatchesByTypeInDueOrder(t *testing.T) {
	ctx := context.Background()
	database := economyDatabase(t, ctx, 0)
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
	insertEvent(t, database, "beta", "2042-09-10T11:00:00Z", 70, "b1")
	insertEvent(t, database, "alpha", "2042-09-10T11:00:00Z", 50, "a1")
	insertEvent(t, database, "alpha", "2042-09-10T10:00:00Z", 50, "a0")
	insertEvent(t, database, "alpha", "2042-09-10T13:00:00Z", 50, "future")

	processed, err := processor.CompleteDue(ctx, 10)
	if err != nil || processed != 3 {
		t.Fatalf("CompleteDue() = %d, %v", processed, err)
	}
	if len(order) != 3 || order[0] != "alpha:1" || order[1] != "alpha:1" || order[2] != "beta:1" {
		t.Fatalf("order = %v", order)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM scheduled_events WHERE state = 'completed'", 3)
	next, ok, err := processor.NextDue(ctx)
	if err != nil || !ok || !next.Equal(time.Date(2042, time.September, 10, 13, 0, 0, 0, time.UTC)) {
		t.Fatalf("NextDue() = %v %v %v", next, ok, err)
	}
}

func TestEventProcessorRecordsFailuresAndSkipsPoisonEvents(t *testing.T) {
	ctx := context.Background()
	database := economyDatabase(t, ctx, 0)
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC))
	processor := storagesqlite.NewEventProcessor(database.Write(), clock)
	calls := 0
	processor.Register("failing", func(_ context.Context, tx *sql.Tx, _ storagesqlite.ScheduledEvent, _ time.Time) error {
		calls++
		if _, err := tx.ExecContext(ctx, "INSERT INTO game_event_log(event_type, entity_type, entity_id, occurred_at) VALUES ('side_effect', 'x', '1', '2042-09-10T12:00:00Z')"); err != nil {
			return err
		}
		return errors.New("simulated crash before commit")
	})
	insertEvent(t, database, "failing", "2042-09-10T11:00:00Z", 50, "f1")
	insertEvent(t, database, "unknown_type", "2042-09-10T11:30:00Z", 50, "u1")

	processed, err := processor.CompleteDue(ctx, 100)
	if err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}
	if processed != 0 || calls != 5 {
		t.Fatalf("processed = %d, calls = %d, want 0 and 5", processed, calls)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM game_event_log WHERE event_type = 'side_effect'", 0)
	assertSingleValue(t, database, "SELECT attempts FROM scheduled_events WHERE idempotency_key = 'f1'", 5)
	assertSingleValue(t, database, "SELECT attempts FROM scheduled_events WHERE idempotency_key = 'u1'", 5)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM scheduled_events WHERE state = 'pending' AND last_error IS NOT NULL", 2)
	if _, ok, _ := processor.NextDue(ctx); ok {
		t.Fatal("poison events must not keep the worker awake")
	}
}

func TestScenarioFCrashBeforeCommitReplaysOnce(t *testing.T) {
	ctx := context.Background()
	database := economyDatabase(t, ctx, 0)
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC))
	processor := storagesqlite.NewEventProcessor(database.Write(), clock)
	attempt := 0
	processor.Register("effect", func(_ context.Context, tx *sql.Tx, _ storagesqlite.ScheduledEvent, _ time.Time) error {
		attempt++
		if _, err := tx.ExecContext(ctx, "INSERT INTO game_event_log(event_type, entity_type, entity_id, occurred_at) VALUES ('effect_applied', 'x', '1', '2042-09-10T12:00:00Z')"); err != nil {
			return err
		}
		if attempt == 1 {
			return errors.New("crash")
		}
		return nil
	})
	insertEvent(t, database, "effect", "2042-09-10T11:00:00Z", 50, "e1")
	if _, err := processor.CompleteDue(ctx, 100); err != nil {
		t.Fatal(err)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM game_event_log WHERE event_type = 'effect_applied'", 1)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM scheduled_events WHERE state = 'completed'", 1)
	if _, err := processor.CompleteDue(ctx, 100); err != nil {
		t.Fatal(err)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM game_event_log WHERE event_type = 'effect_applied'", 1)
}
```

  (`economyDatabase(t, ctx, 0)` fonctionne déjà avec zéro compte.)

- [ ] **Step 2: Vérifier l'échec** — `go test ./tests/ -run 'TestEventProcessor|TestScenarioF' -v` → `undefined: storagesqlite.NewEventProcessor`.

- [ ] **Step 3: Implémenter `events.go`**

```go
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

var ErrUnknownEventType = errors.New("sqlite: no handler registered for event type")

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

type EventHandler func(ctx context.Context, tx *sql.Tx, event ScheduledEvent, now time.Time) error

// EventProcessor selects the next due event of any type and delegates it to
// the handler registered for its type inside one write transaction.
type EventProcessor struct {
	write       *sql.DB
	clock       domainclock.Clock
	handlers    map[string]EventHandler
	MaxAttempts int
	Logger      *slog.Logger
}

func NewEventProcessor(write *sql.DB, clock domainclock.Clock) *EventProcessor {
	return &EventProcessor{write: write, clock: clock, handlers: map[string]EventHandler{}, MaxAttempts: 5, Logger: slog.New(slog.DiscardHandler)}
}

func (p *EventProcessor) Register(eventType string, handler EventHandler) {
	if _, exists := p.handlers[eventType]; exists {
		panic("sqlite: duplicate event handler for " + eventType)
	}
	p.handlers[eventType] = handler
}

func (p *EventProcessor) NextDue(ctx context.Context) (time.Time, bool, error) {
	var value string
	err := p.write.QueryRowContext(ctx, `SELECT due_at FROM scheduled_events WHERE state = 'pending' AND attempts < ? ORDER BY due_at, priority, id LIMIT 1`, p.MaxAttempts).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("event processor: next due: %w", err)
	}
	dueAt, err := time.Parse(time.RFC3339Nano, value)
	return dueAt, err == nil, err
}

func (p *EventProcessor) CompleteDue(ctx context.Context, limit int) (int, error) {
	completed := 0
	for completed < limit {
		now := p.clock.Now().UTC()
		event, found, err := p.selectDue(ctx, now)
		if err != nil || !found {
			return completed, err
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

// ProcessOne applies at most one due event and reports whether one was found.
func (p *EventProcessor) ProcessOne(ctx context.Context, now time.Time) (bool, error) {
	event, found, err := p.selectDue(ctx, now)
	if err != nil || !found {
		return false, err
	}
	_, err = p.apply(ctx, event, now)
	return true, err
}

func (p *EventProcessor) selectDue(ctx context.Context, now time.Time) (ScheduledEvent, bool, error) {
	var event ScheduledEvent
	var dueText string
	var rulesetVersion sql.NullInt64
	var payload string
	err := p.write.QueryRowContext(ctx, `SELECT id, event_type, due_at, priority, entity_type, entity_id, ruleset_version, payload_version, payload, attempts FROM scheduled_events WHERE state = 'pending' AND due_at <= ? AND attempts < ? ORDER BY due_at, priority, id LIMIT 1`, timestamp(now), p.MaxAttempts).
		Scan(&event.ID, &event.Type, &dueText, &event.Priority, &event.EntityType, &event.EntityID, &rulesetVersion, &event.PayloadVersion, &payload, &event.Attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return ScheduledEvent{}, false, nil
	}
	if err != nil {
		return ScheduledEvent{}, false, fmt.Errorf("event processor: select due: %w", err)
	}
	event.DueAt, err = time.Parse(time.RFC3339Nano, dueText)
	if err != nil {
		return ScheduledEvent{}, false, fmt.Errorf("event processor: parse due_at: %w", err)
	}
	event.RulesetVersion = rulesetVersion.Int64
	event.Payload = []byte(payload)
	return event, true, nil
}

// apply runs the handler in one transaction; on failure it records the attempt
// in a separate short transaction so the event is eventually skipped.
func (p *EventProcessor) apply(ctx context.Context, event ScheduledEvent, now time.Time) (bool, error) {
	handler, ok := p.handlers[event.Type]
	err := ErrUnknownEventType
	if ok {
		err = withWriteTx(ctx, p.write, "event processor", func(tx *sql.Tx) error {
			if err := handler(ctx, tx, event, now); err != nil {
				return err
			}
			result, err := tx.ExecContext(ctx, `UPDATE scheduled_events SET state = 'completed', processed_at = ?, attempts = attempts + 1, last_error = NULL WHERE id = ? AND state = 'pending'`, timestamp(now), event.ID)
			if err != nil {
				return fmt.Errorf("event processor: complete event: %w", err)
			}
			if affected, _ := result.RowsAffected(); affected != 1 {
				return errors.New("event processor: event vanished during processing")
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
	p.Logger.Error("scheduled event failed", "event_id", event.ID, "event_type", event.Type, "attempt", event.Attempts+1, "error", err.Error())
	if _, updateErr := p.write.ExecContext(ctx, `UPDATE scheduled_events SET attempts = attempts + 1, last_error = ? WHERE id = ? AND state = 'pending'`, err.Error(), event.ID); updateErr != nil {
		return false, fmt.Errorf("event processor: record failure: %w", updateErr)
	}
	return false, nil
}
```

  Note : `slog.DiscardHandler` existe depuis Go 1.24.

- [ ] **Step 4: Migrer le handler économie** dans `economy.go` :

```go
// RegisterHandlers plugs building completion into the shared event processor.
func (r *EconomyRepository) RegisterHandlers(processor *EventProcessor) {
	processor.Register("building_completed", func(ctx context.Context, tx *sql.Tx, event ScheduledEvent, now time.Time) error {
		return completeBuilding(ctx, tx, event, now, r.catalogue)
	})
}
```

  `EconomyRepository` reçoit le catalogue à la construction : `NewEconomyRepository(write *sql.DB, catalogue building.Catalogue)`. Le corps de `completeOne` (lignes 281-324 actuelles, sans la sélection ni la mise à jour de `scheduled_events`) devient `completeBuilding(ctx, tx, event, now, catalogue) error` ; `queueID` vient de `event.EntityID`, `dueAt` de `event.DueAt`. Supprimer `NextDue` et `CompleteDue` du repository et de l'interface `appeconomy.Repository`.

- [ ] **Step 5: Adapter le service économie**

```go
// Completer settles due events before an authoritative read.
type Completer interface {
	CompleteDue(context.Context, int) (int, error)
}

type Service struct {
	Clock      domainclock.Clock
	Repository Repository
	Catalogue  building.Catalogue
	Completer  Completer
	Wake       func()
}

func (s Service) Planet(ctx context.Context, principal appauth.Principal) (Planet, error) {
	if err := s.validatePrincipal(principal); err != nil {
		return Planet{}, err
	}
	if s.Completer != nil {
		if _, err := s.Completer.CompleteDue(ctx, 100); err != nil {
			return Planet{}, err
		}
	}
	return s.Repository.Planet(ctx, principal.AccountID, s.Clock.Now().UTC(), s.Catalogue)
}
```

  Supprimer `Service.NextDue`/`Service.CompleteDue`. Dans `cli.go` :

```go
economyRepository := storagesqlite.NewEconomyRepository(database.Write(), building.DefaultCatalogue())
processor := storagesqlite.NewEventProcessor(database.Write(), clock)
economyRepository.RegisterHandlers(processor)
worker := appsimulation.NewWorker(clock, processor)
economy := appeconomy.Service{Clock: clock, Repository: economyRepository, Catalogue: building.DefaultCatalogue(), Completer: processor, Wake: worker.Wake}
```

  Mettre à jour les tests existants qui construisent `appeconomy.Service` (`tests/economy_test.go`, `tests/web_economy_test.go`) : créer le processor, `RegisterHandlers`, passer `Completer: processor`, remplacer `service.CompleteDue(ctx, n)` par `processor.CompleteDue(ctx, n)`.

- [ ] **Step 6: Vérifier** — `go test ./... && go test -race ./tests/` → PASS, y compris `TestEconomyProgressionFromEmpireToCompletedBuilding` (rejeu idempotent conservé).

- [ ] **Step 7: Documenter** — remplacer la section « Ordre déterministe » de `docs/architecture/scheduled-events.md` par la table des priorités de D1 et ajouter « Échecs : un handler en erreur annule sa transaction ; `attempts` et `last_error` sont enregistrés ; après `MaxAttempts` (5) l'événement n'est plus sélectionné et reste `pending` pour inspection administrative. »

- [ ] **Step 8: Commit**

```bash
git add internal/storage/sqlite internal/app/economy internal/cli tests docs/architecture/scheduled-events.md
git commit -m "refactor: dispatch scheduled events by type"
```

---

### Task F3 : Ruleset évolutif (schema_version + décodage sur défauts)

**Files:**
- Modify: `internal/domain/rules/ruleset.go`
- Test: `internal/domain/rules/ruleset_test.go` (ajouter deux tests)
- Modify: `web/templates/setup.html` (étape 6 : champ `catalogue_version` en lecture seule), `internal/web/setup_form.go` (étape 6 lit `catalogue_version`)

**Interfaces:**
- Produces: `rules.CurrentSchemaVersion = 2`, `Ruleset.SchemaVersion int` (`schema_version`), `ProgressionSettings.CatalogueVersion string` (`catalogue_version`, défaut `"classic-1"`), `rules.ErrFutureSchema`.

- [ ] **Step 1: Tests rouges**

```go
func TestDecodeFillsMissingSectionsWithDefaults(t *testing.T) {
	document, err := Encode(Default())
	if err != nil {
		t.Fatal(err)
	}
	var generic map[string]any
	if err := json.Unmarshal(document, &generic); err != nil {
		t.Fatal(err)
	}
	delete(generic, "schema_version")
	delete(generic["progression"].(map[string]any), "catalogue_version")
	legacy, _ := json.Marshal(generic)
	decoded, err := Decode(legacy)
	if err != nil {
		t.Fatalf("Decode(legacy) error = %v", err)
	}
	if decoded.SchemaVersion != CurrentSchemaVersion || decoded.Progression.CatalogueVersion != "classic-1" {
		t.Fatalf("decoded = %+v", decoded.Progression)
	}
}

func TestDecodeRejectsFutureSchemaAndUnknownFields(t *testing.T) {
	document, _ := Encode(Default())
	future := bytes.Replace(document, []byte(`"schema_version":2`), []byte(`"schema_version":99`), 1)
	if _, err := Decode(future); !errors.Is(err, ErrFutureSchema) {
		t.Fatalf("future schema error = %v", err)
	}
	unknown := bytes.Replace(document, []byte(`"schema_version":2`), []byte(`"schema_version":2,"mystery":1`), 1)
	if _, err := Decode(unknown); err == nil {
		t.Fatal("unknown field accepted")
	}
}
```

- [ ] **Step 2: Vérifier l'échec** — `go test ./internal/domain/rules/ -v` → `undefined: CurrentSchemaVersion`.

- [ ] **Step 3: Implémenter**

```go
const CurrentSchemaVersion = 2

var ErrFutureSchema = errors.New("rules: document schema is newer than this application")

type Ruleset struct {
	SchemaVersion int                 `json:"schema_version"`
	Identity      IdentitySettings    `json:"identity"`
	// … sections existantes inchangées …
}

type ProgressionSettings struct {
	// … champs existants …
	CatalogueVersion string `json:"catalogue_version"`
}

// Default : SchemaVersion: CurrentSchemaVersion ; Progression.CatalogueVersion: "classic-1".

// Decode strictly parses a persisted ruleset on top of the current defaults so
// documents written by older versions keep decoding.
func Decode(document []byte) (Ruleset, error) {
	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.DisallowUnknownFields()
	ruleset := Default()
	ruleset.SchemaVersion = 1 // absent => first schema
	if err := decoder.Decode(&ruleset); err != nil {
		return Ruleset{}, fmt.Errorf("rules: decode: %w", err)
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return Ruleset{}, err
	}
	if ruleset.SchemaVersion > CurrentSchemaVersion {
		return Ruleset{}, ErrFutureSchema
	}
	ruleset.SchemaVersion = CurrentSchemaVersion
	if err := ruleset.Validate(); err != nil {
		return Ruleset{}, err
	}
	return ruleset, nil
}
```

  Dans `Validate()` : `if r.SchemaVersion < 1 || r.SchemaVersion > CurrentSchemaVersion { return ErrFutureSchema }` et `if strings.TrimSpace(r.Progression.CatalogueVersion) == "" { return errors.New("rules: catalogue version is required") }`. Étape 6 du wizard : `configured.Progression.CatalogueVersion = requiredText(request, "catalogue_version")` avec `<input type="hidden" name="catalogue_version" value="{{.Rules.Progression.CatalogueVersion}}">` et un texte « Catalogue : classic-1 ».

- [ ] **Step 4: Vérifier** — `go test ./... ` → PASS (les tests setup existants voient la nouvelle valeur par défaut).

- [ ] **Step 5: Commit**

```bash
git add internal/domain/rules internal/web/setup_form.go web/templates/setup.html
git commit -m "feat: version ruleset documents"
```

---

### Task F4 : Projection multi-planètes et vue globale

**Files:**
- Modify: `internal/app/economy/service.go` (`Planets`, `PlanetByID`, `Buildings(ctx, principal, planetID)`)
- Modify: `internal/storage/sqlite/economy.go` (`Planets`, `Planet(ctx, accountID, planetID, now, catalogue)`, `playerByAccount`)
- Create: `web/templates/overview.html`
- Modify: `internal/web/handler.go` (routes `GET /{$}` → overview, `GET /planets/{planet}` → planète, redirections PRG vers `/planets/{id}`)
- Test: `tests/economy_test.go::TestPlanetsListsEveryOwnedBodySettled`, `tests/web_economy_test.go` (parcours mis à jour)

**Interfaces:**
- Produces:

```go
// app/economy
type Repository interface {
	CreateEmpire(context.Context, int64, string, time.Time) (Planet, error)
	Planet(context.Context, int64, int64, time.Time, building.Catalogue) (Planet, error)      // accountID, planetID (0 = première)
	Planets(context.Context, int64, time.Time, building.Catalogue) ([]Planet, error)
	StartConstruction(context.Context, int64, int64, building.ID, string, time.Time, building.Catalogue) (Queue, error)
}
func (s Service) Planet(ctx, principal appauth.Principal, planetID int64) (Planet, error)
func (s Service) Planets(ctx, principal appauth.Principal) ([]Planet, error)
func (s Service) Buildings(ctx, principal appauth.Principal, planetID int64) (Planet, []BuildingChoice, error)
// storage/sqlite (interne, réutilisé par tous les jalons)
func playerByAccount(ctx context.Context, tx *sql.Tx, accountID int64) (int64, error) // ErrNoEmpire
func ownedPlanet(ctx context.Context, tx *sql.Tx, accountID, planetID int64) error   // ErrForbidden si autre propriétaire
```

- [ ] **Step 1: Test rouge (repository)**

```go
func TestPlanetsListsEveryOwnedBodySettled(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC)
	clock := appclock.NewFake(now)
	database := economyDatabase(t, ctx, 2)
	repository := storagesqlite.NewEconomyRepository(database.Write(), building.DefaultCatalogue())
	service := appeconomy.Service{Clock: clock, Repository: repository, Catalogue: building.DefaultCatalogue()}
	if _, err := service.CreateEmpire(ctx, appauth.Principal{AccountID: 1}, "Alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateEmpire(ctx, appauth.Principal{AccountID: 2}, "Beta"); err != nil {
		t.Fatal(err)
	}
	// second body for account 1, inserted directly until colonisation exists
	if _, err := database.Write().ExecContext(ctx, `INSERT INTO planets(owner_player_id, name, galaxy, system, position, total_fields, minimum_temperature, maximum_temperature, created_at) VALUES (1, 'Colonie', 1, 2, 4, 150, 10, 50, '2042-09-10T11:12:13Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Write().ExecContext(ctx, `INSERT INTO planet_resources(planet_id, produced_at) VALUES (3, '2042-09-10T11:12:13Z')`); err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Hour)
	planets, err := service.Planets(ctx, appauth.Principal{AccountID: 1})
	if err != nil || len(planets) != 2 {
		t.Fatalf("Planets() = %d, %v", len(planets), err)
	}
	if planets[0].Name != "Planète mère" || planets[1].Name != "Colonie" || planets[1].Stock.Metal != 530 {
		t.Fatalf("planets = %#v", planets)
	}
	if _, err := service.Planet(ctx, appauth.Principal{AccountID: 2}, planets[1].ID); !errors.Is(err, appeconomy.ErrForbidden) {
		t.Fatalf("foreign planet error = %v", err)
	}
}
```

- [ ] **Step 2: Vérifier l'échec** — `go test ./tests/ -run TestPlanetsLists -v` → `service.Planets undefined`.

- [ ] **Step 3: Implémenter** — `Planets` : `withWriteTx` ; `playerByAccount` ; `SELECT id FROM planets WHERE owner_player_id = ? ORDER BY id` ; pour chaque id `loadPlanetByID` + `persistProduction`. `Planet(ctx, accountID, planetID, …)` : si `planetID > 0`, `ownedPlanet` d'abord (renvoie `appeconomy.ErrForbidden` quand la planète existe mais appartient à un autre joueur, `ErrNoEmpire` si absente) puis `loadPlanet`. Ajouter `ErrForbidden` déjà existant dans `app/economy`.

- [ ] **Step 4: Vue globale** — `overview.html` (document complet pour l'instant, converti au layout en F5) : tableau des corps (nom, coordonnées, métal/cristal/deutérium, construction en cours et échéance) avec lien `/planets/{{.ID}}`. Handler `home` : `Planets` → si `ErrNoEmpire` → `empire.html` ; sinon `overview.html`. Handler `planetPage` sur `GET /planets/{planet}` : `Buildings(ctx, principal, planetID)` → `economy.html`. `startBuilding` redirige vers `/planets/{planet}`. Erreur `ErrForbidden` → 404 (ne pas révéler l'existence).

- [ ] **Step 5: Mettre à jour `tests/web_economy_test.go`** : après création de l'empire, `GET /` contient « Planète mère » et un lien `/planets/1` ; `GET /planets/1` contient « Mine de métal » ; `POST /planets/1/buildings/metal_mine` → 303 vers `/planets/1` ; `GET /planets/2` (planète d'un autre joueur) → 404.

- [ ] **Step 6: Vérifier** — `go test ./... && go vet ./...` → PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/app/economy internal/storage/sqlite/economy.go internal/web web/templates tests
git commit -m "feat: expose multi-planet empire overview"
```

---

### Task F5 : Layout partagé, navigation, CSS/JS embarqués

**Files:**
- Create: `web/templates/layout.html`, `web/static/app.js`
- Modify: `web/templates/{login,password-change,setup,empire,overview,economy}.html` (chaque page = `{{define "title"}}` + `{{define "content"}}`)
- Modify: `web/assets.go` (embed `static/*` inchangé), `web/static/app.css` (nav, grille responsive, tokens)
- Modify: `internal/web/handler.go` (`templates map[string]*template.Template`, `render(name, data)`, `pageShell`)
- Test: `tests/web_layout_test.go`

**Interfaces:**
- Produces:

```go
type bodyLink struct{ ID int64; Name string; Coordinate string; Kind string; Current bool }
type pageShell struct {
	CSRFToken string
	Error     string
	Username  string
	Bodies    []bodyLink
	Current   *bodyLink
	Now       time.Time
	Section   string // "overview" | "planet" | "research" | "shipyard" | "defense" | "fleet" | "galaxy" | "reports" | "alliance" | "admin"
}
func (h *Handler) shell(ctx, response, request, principal appauth.Principal, section string, planetID int64) (pageShell, bool)
func (h *Handler) render(response http.ResponseWriter, status int, page string, data any)
```

  Chaque `xxxPageData` embarque `pageShell` (champ anonyme) ; le layout lit `.CSRFToken`, `.Bodies`, `.Section`, `.Now`.

- [ ] **Step 1: Test rouge**

```go
func TestGamePagesShareLayoutWithoutInlineScripts(t *testing.T) {
	handler, session, csrfCookie := playableHandler(t) // helper : empire créé, cookies prêts (extraire de TestWebEconomyFlow…)
	for _, path := range []string{"/", "/planets/1"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.AddCookie(session)
		request.AddCookie(csrfCookie)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		body := recorder.Body.String()
		if recorder.Code != http.StatusOK || !strings.Contains(body, `<nav aria-label="Navigation principale">`) || !strings.Contains(body, `href="/planets/1"`) {
			t.Fatalf("%s = %d %q", path, recorder.Code, body)
		}
		if strings.Contains(body, "<script>") || !strings.Contains(body, `<script src="/static/app.js" defer></script>`) {
			t.Fatalf("%s: inline script or missing app.js", path)
		}
		assertSecurityHeaders(t, recorder.Header())
	}
	static := httptest.NewRecorder()
	handler.ServeHTTP(static, httptest.NewRequest(http.MethodGet, "/static/app.js", nil))
	if static.Code != http.StatusOK || !strings.Contains(static.Header().Get("Content-Type"), "javascript") {
		t.Fatalf("app.js = %d %q", static.Code, static.Header().Get("Content-Type"))
	}
}
```

- [ ] **Step 2: Vérifier l'échec** — `go test ./tests/ -run TestGamePagesShareLayout -v` → FAIL (pas de `<nav`).

- [ ] **Step 3: Implémenter le layout**

```html
{{define "layout"}}<!doctype html>
<html lang="fr">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{template "title" .}} — Universe At War</title>
  <link rel="stylesheet" href="/static/app.css">
  <script src="/static/app.js" defer></script>
</head>
<body>
  <header class="game-header">
    <a class="brand" href="/">Universe At War</a>
    {{if .Bodies}}
    <nav aria-label="Navigation principale">
      <a href="/" {{if eq .Section "overview"}}aria-current="page"{{end}}>Empire</a>
      {{with .Current}}
      <a href="/planets/{{.ID}}" {{if eq $.Section "planet"}}aria-current="page"{{end}}>Planète</a>
      {{end}}
    </nav>
    <form class="body-picker" method="get" action="/planets/switch">
      <label>Corps actif
        <select name="planet" onchange="">{{range .Bodies}}<option value="{{.ID}}" {{if .Current}}selected{{end}}>{{.Name}} [{{.Coordinate}}]</option>{{end}}</select>
      </label>
      <button type="submit">Aller</button>
    </form>
    {{end}}
    <div class="session"><span>{{.Username}}</span>
      <form method="post" action="/logout"><input type="hidden" name="csrf_token" value="{{.CSRFToken}}"><button type="submit">Déconnexion</button></form>
    </div>
  </header>
  <main class="game-shell">
    {{if .Error}}<div class="alert" role="alert">{{.Error}}</div>{{end}}
    {{template "content" .}}
  </main>
  <footer class="game-footer"><time datetime="{{.Now.Format "2006-01-02T15:04:05Z07:00"}}" data-server-time>{{.Now.Format "15:04:05"}} UTC</time></footer>
</body>
</html>{{end}}
```

  Supprimer l'attribut `onchange=""` (CSP) : le bouton « Aller » suffit ; `GET /planets/switch?planet=ID` redirige 303 vers `/planets/ID`. Les liens Recherche/Chantier/Défense/Flotte/Galaxie/Rapports/Alliance sont ajoutés par les jalons qui créent les routes.

  Construction des templates dans `New` :

```go
layout, err := template.ParseFS(webassets.Files, "templates/layout.html")
pages := map[string]*template.Template{}
for _, name := range []string{"login", "password-change", "setup", "empire", "overview", "economy"} {
	page, err := template.Must(layout.Clone()).ParseFS(webassets.Files, "templates/"+name+".html")
	pages[name] = page
}
// render: pages[name].ExecuteTemplate(response, "layout", data)
```

  Les pages d'authentification et de setup gardent leur mise en page (`auth-shell`) via `{{define "content"}}` ; `.Bodies` vide masque la navigation.

- [ ] **Step 4: `app.js`** (vanilla, sans dépendance)

```js
(() => {
  const format = (ms) => {
    const total = Math.max(0, Math.floor(ms / 1000));
    const h = String(Math.floor(total / 3600)).padStart(2, "0");
    const m = String(Math.floor((total % 3600) / 60)).padStart(2, "0");
    const s = String(total % 60).padStart(2, "0");
    return `${h}:${m}:${s}`;
  };
  const server = document.querySelector("[data-server-time]");
  const skew = server ? Date.parse(server.getAttribute("datetime")) - Date.now() : 0;
  const nodes = [...document.querySelectorAll("time[data-countdown]")];
  if (nodes.length === 0) return;
  const tick = () => {
    const now = Date.now() + skew;
    for (const node of nodes) {
      const remaining = Date.parse(node.getAttribute("datetime")) - now;
      node.textContent = remaining <= 0 ? node.dataset.done || "terminé" : format(remaining);
    }
  };
  tick();
  setInterval(tick, 1000);
})();
```

  Les éléments `<time data-countdown datetime="…" aria-live="off">` gardent leur texte serveur (heure absolue) : sans JavaScript la page reste correcte ; `aria-live="off"` évite les annonces chaque seconde (spec §54).

- [ ] **Step 5: Vérifier** — `go test ./...` → PASS (tous les tests web existants passent avec le layout).

- [ ] **Step 6: Commit**

```bash
git add web internal/web tests/web_layout_test.go
git commit -m "feat: add shared layout and navigation"
```

---

### Task F6 : Inscription minimale (politiques `closed` et `open`)

**Files:**
- Create: `internal/app/registration/service.go`, `internal/storage/sqlite/registration.go`
- Create: `web/templates/register.html`
- Modify: `internal/web/handler.go` (routes `GET /register`, `POST /register`, dépendance `Registration`), `web/templates/login.html` (lien « Créer un compte »)
- Modify: `internal/cli/cli.go` (câblage)
- Test: `tests/registration_test.go`, `tests/web_registration_test.go`
- Modify: `docs/architecture/server-state.md` (ligne : `/register` accessible seulement en `RUNNING`)

**Interfaces:**
- Produces:

```go
package registration

var (
	ErrClosed          = errors.New("registration: registrations are closed")
	ErrInvalidUsername = errors.New("registration: username must contain 3 to 32 characters among a-z, 0-9 and _")
	ErrWeakPassword    = errors.New("registration: password must contain at least 12 characters")
	ErrUsernameTaken   = errors.New("registration: username is already used")
	ErrServerNotRunning = errors.New("registration: universe is not running")
)

type Passwords interface{ Hash(string) (string, error) }
type Record struct{ Username, NormalizedName, EncodedHash string; OccurredAt time.Time }
type Repository interface {
	RegistrationPolicy(context.Context) (string, error)             // lit server_state + ruleset actif ; ErrServerNotRunning
	CreateAccount(context.Context, Record) (int64, error)          // ErrUsernameTaken
}
type Service struct{ Clock domainclock.Clock; Passwords Passwords; Repository Repository }
func (s Service) Policy(ctx context.Context) (string, error)
func (s Service) Register(ctx context.Context, username, password string) (int64, error)
```

- [ ] **Step 1: Tests rouges**

```go
func TestRegistrationFollowsRulesetPolicy(t *testing.T) {
	ctx := context.Background()
	database := economyDatabase(t, ctx, 1) // ruleset actif = closed
	if _, err := database.Write().ExecContext(ctx, "INSERT INTO server_state(id, state, updated_at) VALUES (1, 'RUNNING', '2042-09-10T11:12:13Z')"); err != nil {
		t.Fatal(err)
	}
	service := appregistration.Service{Clock: appclock.NewFake(time.Date(2042, 9, 10, 12, 0, 0, 0, time.UTC)), Passwords: auth.NewPasswordHasher(auth.Parameters{MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}, rand.Reader), Repository: storagesqlite.NewRegistrationRepository(database.Read(), database.Write())}
	if _, err := service.Register(ctx, "newcomer", "a-strong-password"); !errors.Is(err, appregistration.ErrClosed) {
		t.Fatalf("closed policy error = %v", err)
	}
	if _, err := database.Write().ExecContext(ctx, `UPDATE ruleset_versions SET document = json_set(document, '$.identity.registration_policy', 'open')`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Register(ctx, "Newcomer", "short"); !errors.Is(err, appregistration.ErrWeakPassword) {
		t.Fatalf("weak password error = %v", err)
	}
	id, err := service.Register(ctx, "Newcomer", "a-strong-password")
	if err != nil || id <= 0 {
		t.Fatalf("Register() = %d, %v", id, err)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM account_roles WHERE account_id = 2 AND role = 'PLAYER'", 1)
	assertSingleValue(t, database, "SELECT must_change_password FROM password_credentials WHERE account_id = 2", 0)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM audit_log WHERE action = 'account_registered'", 1)
	if _, err := service.Register(ctx, "newcomer", "another-strong-one"); !errors.Is(err, appregistration.ErrUsernameTaken) {
		t.Fatalf("duplicate error = %v", err)
	}
}

func TestConcurrentRegistrationsWithSameNameCreateOneAccount(t *testing.T) {
	// deux goroutines gated par close(start) → exactement un succès, l'autre ErrUsernameTaken
}
```

- [ ] **Step 2: Vérifier l'échec** — `go test ./tests/ -run TestRegistration -v` → package manquant.

- [ ] **Step 3: Implémenter** — `Register` : normaliser (`strings.ToLower(strings.TrimSpace)`), regex `^[a-z0-9_]{3,32}$`, mot de passe ≥ 12 runes, `Policy` (`closed`/`invitation` → `ErrClosed`), `Hash`, `CreateAccount` dans `withWriteTx` (INSERT `accounts`, `account_roles('PLAYER')`, `password_credentials(must_change_password=0)`, `audit_log('account_registered', target_type='account')`) ; violation UNIQUE → `ErrUsernameTaken`. `RegistrationPolicy` lit `server_state.state` (≠ RUNNING → `ErrServerNotRunning`) puis `json_extract(document, '$.identity.registration_policy')` du ruleset actif (évite un décodage complet).

- [ ] **Step 4: Web** — `GET /register` : si `Policy() != "open"` → 404 ; sinon formulaire (username, password, confirmation, CSRF). `POST /register` : erreurs métier → 400 avec message générique « Inscription impossible avec ces informations. » (pas d'énumération) ; succès → 303 `/login?registered=1` ; `login.html` affiche « Compte créé, connectez-vous. » et le lien « Créer un compte » uniquement si la politique est `open`. Test HTTP : 404 en `closed`, 200/303 en `open`, CSRF 403.

- [ ] **Step 5: Vérifier** — `go test ./... && go test -race ./tests/` → PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/app/registration internal/storage/sqlite/registration.go internal/web web/templates internal/cli tests docs/architecture/server-state.md
git commit -m "feat: add minimal open registration"
```

---

### Task F7 : Corrélation des requêtes et journalisation

**Files:**
- Create: `internal/web/middleware.go`, `internal/web/middleware_test.go`
- Modify: `internal/web/handler.go` (`Dependencies.Logger *slog.Logger`, chaîne `securityHeaders(requestID(requestLogger(mux)))`)
- Modify: `internal/cli/cli.go` (`Runner.LogLevel string`, `Runner.LogOutput io.Writer`, création du logger, passage au handler et à `EventProcessor.Logger`), `cmd/universe-at-war/main.go` (passe `config.LogLevel`)
- Modify: `internal/app/simulation/worker.go` (`Logger *slog.Logger` ; `Run` journalise et **continue** après une erreur de `CompleteDue` au lieu de s'arrêter, avec backoff = `RescanInterval`)

**Interfaces:**
- Produces: `func requestID(next http.Handler) http.Handler` (contexte clé `requestIDKey`, header `X-Request-Id`), `func RequestIDFrom(ctx context.Context) string`, `func requestLogger(logger *slog.Logger, next http.Handler) http.Handler` (attributs `method`, `path`, `status`, `duration_ms`, `request_id`).

- [ ] **Step 1: Test rouge** (`internal/web/middleware_test.go`)

```go
func TestRequestLoggerCorrelatesWithoutLeakingSecrets(t *testing.T) {
	var buffer bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buffer, nil))
	handler := requestID(requestLogger(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if RequestIDFrom(r.Context()) == "" {
			t.Fatal("missing request id in context")
		}
		w.WriteHeader(http.StatusTeapot)
	})))
	request := httptest.NewRequest(http.MethodGet, "/planets/1?token=secret", nil)
	request.AddCookie(&http.Cookie{Name: "uaw_session", Value: "super-secret-session"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	id := recorder.Header().Get("X-Request-Id")
	if len(id) != 32 {
		t.Fatalf("X-Request-Id = %q", id)
	}
	line := buffer.String()
	if !strings.Contains(line, `"status":418`) || !strings.Contains(line, `"request_id":"`+id+`"`) || !strings.Contains(line, `"path":"/planets/1"`) {
		t.Fatalf("log = %s", line)
	}
	if strings.Contains(line, "super-secret-session") || strings.Contains(line, "token=secret") {
		t.Fatalf("log leaks secrets: %s", line)
	}
}
```

- [ ] **Step 2: Vérifier l'échec** — `go test ./internal/web/ -run TestRequestLogger -v` → `undefined: requestID`.

- [ ] **Step 3: Implémenter** — `requestID` : 16 octets `crypto/rand` → hex ; `requestLogger` : wrapper `statusRecorder{http.ResponseWriter; status int}` ; journaliser `r.URL.Path` seulement (jamais `RawQuery`, jamais les cookies). Worker : sur erreur, `Logger.Error("simulation batch failed", "error", err)` puis attente `RescanInterval` (la boucle ne meurt plus : un incident DB transitoire ne stoppe pas la simulation). `serve` : `logger, _ := observability.NewLogger(r.logOutput(), r.LogLevel)` ; `logger.Info("server starting", "version", r.Version, "listen", *listenAddress)`.

- [ ] **Step 4: Vérifier** — `go test ./... && go vet ./...` → PASS ; `go run ./cmd/universe-at-war serve` affiche des lignes JSON sans secret.

- [ ] **Step 5: Commit**

```bash
git add internal/web internal/cli cmd internal/app/simulation
git commit -m "feat: correlate requests and worker errors in logs"
```

**Sortie de la Partie 0 :** `make check`, `go test -race ./...`, `staticcheck ./...`, `govulncheck ./...` verts ; `README.md` mentionne l'inscription ouverte, la vue empire et les logs ; `docs/architecture/overview.md` liste `internal/app/registration` et `internal/storage/sqlite/events.go`.

---
