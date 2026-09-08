package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	appeconomy "universeatwar/internal/app/economy"
	appresearch "universeatwar/internal/app/research"
	"universeatwar/internal/domain/catalogue"
	"universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/prerequisite"
	"universeatwar/internal/domain/research"
	"universeatwar/internal/domain/rules"
)

// researchEventPriority orders research completion against the other events of
// the same deadline, as documented in the scheduled events architecture.
const researchEventPriority = 60

// ResearchRepository persists each research action in one write transaction.
type ResearchRepository struct {
	write      *sql.DB
	catalogues catalogue.Set
}

func NewResearchRepository(write *sql.DB, catalogues catalogue.Set) *ResearchRepository {
	return &ResearchRepository{write: write, catalogues: catalogues}
}

// RegisterHandlers plugs research completion into the shared event processor.
func (r *ResearchRepository) RegisterHandlers(processor *EventProcessor) {
	processor.Register("research_completed", func(ctx context.Context, tx *sql.Tx, event ScheduledEvent, now time.Time) error {
		return completeResearch(ctx, tx, event, now, r.catalogues)
	})
}

// State reads the research situation of a player from one of their planets.
func (r *ResearchRepository) State(ctx context.Context, accountID, planetID int64, now time.Time) (appresearch.State, error) {
	var state appresearch.State
	err := withWriteTx(ctx, r.write, "research repository: state", func(tx *sql.Tx) error {
		planet, _, production, err := loadPlanet(ctx, tx, accountID, planetID, now, r.catalogues.Buildings)
		if err != nil {
			return err
		}
		if err := persistProduction(ctx, tx, planet.ID, production); err != nil {
			return err
		}
		playerID, err := playerOfPlanet(ctx, tx, planet.ID)
		if err != nil {
			return err
		}
		laboratories, err := laboratories(ctx, tx, playerID, planet.ID)
		if err != nil {
			return err
		}
		entries, err := playerResearchQueue(ctx, tx, playerID)
		if err != nil {
			return err
		}
		estimateResearchQueue(entries, laboratories, planet.Rules, r.catalogues.Research, now)
		state = appresearch.State{
			Planet:       planet,
			Levels:       planet.Researches,
			Laboratories: laboratories,
			Queue:        entries,
		}
		return nil
	})
	if err != nil {
		return appresearch.State{}, err
	}
	return state, nil
}

// EnqueueResearch debits the cost, appends the entry to the player's queue and
// writes its event, idempotency key and journal entry in a single transaction.
// Only the entry that lands at the head is scheduled.
func (r *ResearchRepository) EnqueueResearch(ctx context.Context, accountID, planetID int64, id research.ID, idempotencyKey string, now time.Time) (appresearch.Queue, error) {
	var queue appresearch.Queue
	err := withWriteTx(ctx, r.write, "research repository: start", func(tx *sql.Tx) error {
		digest := sha256.Sum256([]byte(fmt.Sprintf("%d:%s", planetID, id)))
		requestHash := hex.EncodeToString(digest[:])
		replayed, found, err := replayedResearch(ctx, tx, accountID, idempotencyKey, requestHash)
		if err != nil {
			return err
		}
		if found {
			queue = replayed
			return nil
		}

		planet, rulesetVersion, production, err := loadPlanet(ctx, tx, accountID, planetID, now, r.catalogues.Buildings)
		if err != nil {
			return err
		}
		if err := r.catalogues.Matches(planet.Rules); err != nil {
			return err
		}
		playerID, err := playerOfPlanet(ctx, tx, planet.ID)
		if err != nil {
			return err
		}
		entries, err := playerResearchQueue(ctx, tx, playerID)
		if err != nil {
			return err
		}
		if len(entries) >= planet.Rules.Progression.QueueLength {
			return appresearch.ErrQueueFull
		}
		if err := laboratoryIsIdle(ctx, tx, planet.ID); err != nil {
			return err
		}
		laboratories, err := laboratories(ctx, tx, playerID, planet.ID)
		if err != nil {
			return err
		}
		// Every research still queued counts as done, so a second order for the
		// same technology targets the level after the first.
		researched := planet.Researches.Generic()
		for _, entry := range entries {
			researched[string(entry.Research)] = entry.TargetLevel
		}
		plan, err := r.catalogues.Research.Plan(id, prerequisite.State{
			Buildings:  planet.Levels.Generic(),
			Researches: researched,
		}, laboratories, planet.Rules)
		if err != nil {
			return err
		}
		if plan.Energy > 0 {
			surplus := planet.Energy.Produced - planet.Energy.Consumed
			if surplus < plan.Energy {
				return appresearch.ErrInsufficientEnergy
			}
		}
		production.Stock, err = production.Stock.Debit(plan.Cost)
		if err != nil {
			return err
		}
		if err := persistProduction(ctx, tx, planet.ID, production); err != nil {
			return err
		}
		queuedAt := now.UTC().Truncate(time.Second)
		position := 0
		if last := len(entries); last > 0 {
			position = entries[last-1].Position + 1
		}
		head := len(entries) == 0
		entryState := "queued"
		var startedAt, completesAt time.Time
		var startedValue, completesValue any
		if head {
			entryState = "active"
			startedAt = queuedAt
			completesAt = startedAt.Add(plan.Duration)
			startedValue, completesValue = timestamp(startedAt), timestamp(completesAt)
		}
		result, err := tx.ExecContext(ctx, `
			INSERT INTO research_queue(player_id, planet_id, research_id, target_level, metal_cost, crystal_cost, deuterium_cost, energy_cost, effective_laboratory, ruleset_version, position, queued_at, started_at, completes_at, state)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, playerID, planet.ID, string(id), plan.TargetLevel, plan.Cost.Metal, plan.Cost.Crystal, plan.Cost.Deuterium,
			plan.Energy, plan.EffectiveLaboratory, rulesetVersion, position, timestamp(queuedAt), startedValue, completesValue, entryState)
		if err != nil {
			if strings.Contains(err.Error(), "research_queue_one_active_idx") || strings.Contains(err.Error(), "research_queue_position_idx") {
				return appresearch.ErrQueueBusy
			}
			return fmt.Errorf("research repository: enqueue research: %w", err)
		}
		queueID, err := result.LastInsertId()
		if err != nil {
			return fmt.Errorf("research repository: queue id: %w", err)
		}
		if head {
			if err := scheduleResearch(ctx, tx, queueID, rulesetVersion, startedAt, completesAt); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO idempotency_keys(actor_id, operation, key, request_hash, result_type, result_id, created_at)
			VALUES (?, 'start_research', ?, ?, 'research_queue', ?, ?)
		`, strconv.FormatInt(accountID, 10), idempotencyKey, requestHash, strconv.FormatInt(queueID, 10), timestamp(queuedAt)); err != nil {
			return fmt.Errorf("research repository: record idempotency: %w", err)
		}
		journal := "research_queued"
		if head {
			journal = "research_started"
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO game_event_log(event_type, actor_type, actor_id, entity_type, entity_id, occurred_at, payload)
			VALUES (?, 'account', ?, 'planet', ?, ?, json_object('research_id', ?, 'target_level', ?, 'queue_id', ?, 'position', ?))
		`, journal, accountID, planet.ID, timestamp(queuedAt), string(id), plan.TargetLevel, queueID, position); err != nil {
			return fmt.Errorf("research repository: log research order: %w", err)
		}
		queue = appresearch.Queue{
			ID: queueID, PlanetID: planet.ID, Research: id, TargetLevel: plan.TargetLevel,
			Cost: plan.Cost, Energy: plan.Energy, Position: position,
			StartedAt: startedAt, CompletesAt: completesAt, State: entryState,
		}
		return nil
	})
	if err != nil {
		return appresearch.Queue{}, err
	}
	return queue, nil
}

// CancelResearch drops one order and every level of the same technology queued
// above it, refunding all of them to the planet that paid for each.
func (r *ResearchRepository) CancelResearch(ctx context.Context, accountID, planetID, entryID int64, now time.Time) (appeconomy.Cancellation, error) {
	var cancellation appeconomy.Cancellation
	err := withWriteTx(ctx, r.write, "research repository: cancel research", func(tx *sql.Tx) error {
		planet, _, production, err := loadPlanet(ctx, tx, accountID, planetID, now, r.catalogues.Buildings)
		if err != nil {
			return err
		}
		playerID, err := playerOfPlanet(ctx, tx, planet.ID)
		if err != nil {
			return err
		}
		entries, err := playerResearchQueue(ctx, tx, playerID)
		if err != nil {
			return err
		}
		dropped, headWasDropped := droppedFromResearchQueue(entries, planet, entryID, r.catalogues.Research)
		if len(dropped) == 0 {
			return appresearch.ErrQueueEntryNotFound
		}
		// Settle the planet the page was opened from, whether or not it paid for
		// anything: every read of a body is also a settlement.
		if err := persistProduction(ctx, tx, planet.ID, production); err != nil {
			return err
		}
		// A research is paid by the planet it was launched from, and the queue
		// belongs to the empire, so each order goes back to its own payer rather
		// than to whichever page the player happened to cancel it from.
		owed := map[int64]economy.Resources{}
		for _, entry := range dropped {
			if err := cancelQueueEntry(ctx, tx, "research_queue", entry.ID, fmt.Sprintf("research-complete:%d", entry.ID), now); err != nil {
				return err
			}
			owed[entry.PlanetID] = owed[entry.PlanetID].Plus(entry.Cost)
		}
		var refund, lost economy.Resources
		for _, payerID := range slices.Sorted(maps.Keys(owed)) {
			payer, _, stock, err := loadPlanetByID(ctx, tx, payerID, now, r.catalogues.Buildings)
			if err != nil {
				return err
			}
			var lostHere economy.Resources
			stock.Stock, lostHere = stock.Stock.Refund(owed[payerID], payer.Capacity)
			if err := persistProduction(ctx, tx, payerID, stock); err != nil {
				return err
			}
			refund, lost = refund.Plus(owed[payerID]), lost.Plus(lostHere)
		}
		if headWasDropped {
			if err := promoteNextResearch(ctx, tx, playerID, now, r.catalogues); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO game_event_log(event_type, actor_type, actor_id, entity_type, entity_id, occurred_at, payload)
			VALUES ('research_cancelled', 'account', ?, 'player', ?, ?, json_object('queue_id', ?, 'cancelled', ?))
		`, accountID, playerID, timestamp(now), entryID, len(dropped)); err != nil {
			return fmt.Errorf("research repository: log cancellation: %w", err)
		}
		cancellation = appeconomy.Cancellation{
			Cancelled: len(dropped), Refunded: refund.Minus(lost), Lost: lost,
		}
		return nil
	})
	if err != nil {
		return appeconomy.Cancellation{}, err
	}
	return cancellation, nil
}

// droppedFromResearchQueue picks the order the player named and every later
// order that can no longer stand once it is gone: the levels of the same
// technology, and anything whose prerequisite it was going to supply. What
// survives is exactly what completion would still accept.
func droppedFromResearchQueue(queue []appresearch.Queue, planet appeconomy.Planet, entryID int64, catalogue research.Catalogue) ([]appresearch.Queue, bool) {
	projected := research.Levels{}
	for id, level := range planet.Researches {
		projected[id] = level
	}
	buildings := planet.Levels.Generic()
	var dropped []appresearch.Queue
	found, head := false, false
	for index, entry := range queue {
		if entry.ID == entryID {
			found, head = true, index == 0
		} else if !found || researchSurvivesCancellation(entry, projected, buildings, catalogue) {
			projected[entry.Research] = entry.TargetLevel
			continue
		}
		dropped = append(dropped, entry)
	}
	return dropped, head
}

// researchSurvivesCancellation applies the two rules completion enforces: an
// order raises exactly one level, and its prerequisites hold when it runs.
func researchSurvivesCancellation(entry appresearch.Queue, projected research.Levels, buildings prerequisite.Levels, catalogue research.Catalogue) bool {
	if projected[entry.Research]+1 != entry.TargetLevel {
		return false
	}
	definition, known := catalogue.Definition(entry.Research)
	if !known {
		return false
	}
	return prerequisite.Check(definition.Prerequisites, prerequisite.State{
		Buildings: buildings, Researches: projected.Generic(),
	}) == nil
}

// completeResearch raises the finished level exactly once, then hands the
// laboratory to whatever the player queued behind it.
func completeResearch(ctx context.Context, tx *sql.Tx, event ScheduledEvent, now time.Time, catalogues catalogue.Set) error {
	queueID, err := strconv.ParseInt(event.EntityID, 10, 64)
	if err != nil {
		return errors.New("research repository: invalid queue event reference")
	}
	var playerID, planetID int64
	var researchID, state string
	var targetLevel int
	err = tx.QueryRowContext(ctx, `
		SELECT player_id, planet_id, research_id, target_level, state FROM research_queue WHERE id = ?
	`, queueID).Scan(&playerID, &planetID, &researchID, &targetLevel, &state)
	if err != nil {
		return fmt.Errorf("research repository: read due queue: %w", err)
	}
	if state != "active" {
		return nil
	}
	var current int
	err = tx.QueryRowContext(ctx, "SELECT COALESCE(level, 0) FROM player_research WHERE player_id = ? AND research_id = ?", playerID, researchID).Scan(&current)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("research repository: read current level: %w", err)
	}
	if current+1 != targetLevel {
		return errors.New("research repository: research queue target is stale")
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO player_research(player_id, research_id, level) VALUES (?, ?, ?)
		ON CONFLICT(player_id, research_id) DO UPDATE SET level = excluded.level
	`, playerID, researchID, targetLevel); err != nil {
		return fmt.Errorf("research repository: complete research level: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE research_queue SET state = 'completed', completed_at = ? WHERE id = ? AND state = 'active'
	`, timestamp(now), queueID); err != nil {
		return fmt.Errorf("research repository: complete queue: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO game_event_log(event_type, entity_type, entity_id, occurred_at, payload)
		VALUES ('research_completed', 'player', ?, ?, json_object('research_id', ?, 'level', ?, 'queue_id', ?, 'planet_id', ?))
	`, playerID, timestamp(now), researchID, targetLevel, queueID, planetID); err != nil {
		return fmt.Errorf("research repository: log completion: %w", err)
	}
	// The queue must not gain idle time from a late settlement, so the next
	// research starts at the instant this one was due.
	return promoteNextResearch(ctx, tx, playerID, event.DueAt, catalogues)
}

// scheduleResearch books the completion of the entry now at the head.
func scheduleResearch(ctx context.Context, tx *sql.Tx, queueID, rulesetVersion int64, startedAt, completesAt time.Time) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO scheduled_events(event_type, due_at, priority, entity_type, entity_id, ruleset_version, payload, idempotency_key, created_at)
		VALUES ('research_completed', ?, ?, 'research_queue', ?, ?, json_object('queue_id', ?), ?, ?)
	`, timestamp(completesAt), researchEventPriority, strconv.FormatInt(queueID, 10), rulesetVersion, queueID,
		fmt.Sprintf("research-complete:%d", queueID), timestamp(startedAt)); err != nil {
		return fmt.Errorf("research repository: schedule research: %w", err)
	}
	return nil
}

// promoteNextResearch starts whichever research now waits at the front of the
// queue, timing it with the laboratories the player owns at this instant.
func promoteNextResearch(ctx context.Context, tx *sql.Tx, playerID int64, now time.Time, catalogues catalogue.Set) error {
	var entryID, planetID int64
	var researchID string
	var cost economy.Resources
	err := tx.QueryRowContext(ctx, `
		SELECT id, planet_id, research_id, metal_cost, crystal_cost, deuterium_cost FROM research_queue
		WHERE player_id = ? AND state = 'queued' ORDER BY position, id LIMIT 1
	`, playerID).Scan(&entryID, &planetID, &researchID, &cost.Metal, &cost.Crystal, &cost.Deuterium)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("research repository: read waiting research: %w", err)
	}
	configured, rulesetVersion, err := activeRuleset(ctx, tx)
	if err != nil {
		return err
	}
	available, err := laboratories(ctx, tx, playerID, planetID)
	if err != nil {
		return err
	}
	duration, effective, err := catalogues.Research.DurationFor(research.ID(researchID), cost, available, configured)
	if err != nil {
		return err
	}
	startedAt := now.UTC().Truncate(time.Second)
	completesAt := startedAt.Add(duration)
	result, err := tx.ExecContext(ctx, `
		UPDATE research_queue SET state = 'active', started_at = ?, completes_at = ?, effective_laboratory = ?, ruleset_version = ?
		WHERE id = ? AND state = 'queued'
	`, timestamp(startedAt), timestamp(completesAt), effective, rulesetVersion, entryID)
	if err != nil {
		return fmt.Errorf("research repository: promote research: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("research repository: promote research: %w", err)
	}
	if affected != 1 {
		return errors.New("research repository: the waiting research changed during promotion")
	}
	return scheduleResearch(ctx, tx, entryID, rulesetVersion, startedAt, completesAt)
}

// replayedResearch returns the queue a previous identical request created.
func replayedResearch(ctx context.Context, tx *sql.Tx, accountID int64, idempotencyKey, requestHash string) (appresearch.Queue, bool, error) {
	var storedHash, resultID string
	err := tx.QueryRowContext(ctx, `
		SELECT request_hash, result_id FROM idempotency_keys
		WHERE actor_id = ? AND operation = 'start_research' AND key = ?
	`, strconv.FormatInt(accountID, 10), idempotencyKey).Scan(&storedHash, &resultID)
	if errors.Is(err, sql.ErrNoRows) {
		return appresearch.Queue{}, false, nil
	}
	if err != nil {
		return appresearch.Queue{}, false, fmt.Errorf("research repository: inspect idempotency: %w", err)
	}
	if storedHash != requestHash {
		return appresearch.Queue{}, false, appresearch.ErrInvalidRequest
	}
	queueID, err := strconv.ParseInt(resultID, 10, 64)
	if err != nil {
		return appresearch.Queue{}, false, errors.New("research repository: corrupt idempotency result")
	}
	queue, err := loadResearchQueue(ctx, tx, "q.id = ?", queueID)
	if err != nil {
		return appresearch.Queue{}, false, err
	}
	return *queue, true, nil
}

// playerResearchQueue reads every research the player ordered and has not
// finished, head first. Only the head carries a schedule.
func playerResearchQueue(ctx context.Context, tx *sql.Tx, playerID int64) ([]appresearch.Queue, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, planet_id, research_id, target_level, metal_cost, crystal_cost, deuterium_cost, energy_cost, effective_laboratory, position, started_at, completes_at, state
		FROM research_queue WHERE player_id = ? AND state IN ('active', 'queued')
		ORDER BY position, id
	`, playerID)
	if err != nil {
		return nil, fmt.Errorf("research repository: read research queue: %w", err)
	}
	defer rows.Close()
	var entries []appresearch.Queue
	for rows.Next() {
		entry, err := scanResearchEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

// estimateResearchQueue dates the entries still waiting. Their laboratory is
// the one the player owns now, which is the best forecast available.
func estimateResearchQueue(entries []appresearch.Queue, available research.Laboratories, configured rules.Ruleset, catalogue research.Catalogue, now time.Time) {
	cursor := now
	for index, entry := range entries {
		if !entry.Waiting() {
			if entry.CompletesAt.After(cursor) {
				cursor = entry.CompletesAt
			}
			continue
		}
		duration, _, err := catalogue.DurationFor(entry.Research, entry.Cost, available, configured)
		if err != nil {
			return
		}
		entries[index].EstimatedStartAt = cursor
		cursor = cursor.Add(duration)
		entries[index].EstimatedCompletesAt = cursor
	}
}

type researchScanner interface {
	Scan(destination ...any) error
}

func scanResearchEntry(row researchScanner) (appresearch.Queue, error) {
	var entry appresearch.Queue
	var researchID string
	var effective int
	var startedText, completesText sql.NullString
	if err := row.Scan(&entry.ID, &entry.PlanetID, &researchID, &entry.TargetLevel, &entry.Cost.Metal,
		&entry.Cost.Crystal, &entry.Cost.Deuterium, &entry.Energy, &effective, &entry.Position,
		&startedText, &completesText, &entry.State); err != nil {
		return appresearch.Queue{}, err
	}
	entry.Research = research.ID(researchID)
	if !startedText.Valid {
		return entry, nil
	}
	var err error
	if entry.StartedAt, err = time.Parse(time.RFC3339Nano, startedText.String); err != nil {
		return appresearch.Queue{}, fmt.Errorf("research repository: parse start time: %w", err)
	}
	if entry.CompletesAt, err = time.Parse(time.RFC3339Nano, completesText.String); err != nil {
		return appresearch.Queue{}, fmt.Errorf("research repository: parse completion time: %w", err)
	}
	return entry, nil
}

func loadResearchQueue(ctx context.Context, tx *sql.Tx, condition string, argument any) (*appresearch.Queue, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT q.id, q.planet_id, q.research_id, q.target_level, q.metal_cost, q.crystal_cost, q.deuterium_cost, q.energy_cost, q.effective_laboratory, q.position, q.started_at, q.completes_at, q.state
		FROM research_queue q WHERE `+condition, argument)
	entry, err := scanResearchEntry(row)
	if err != nil {
		return nil, err
	}
	return &entry, nil
}

// laboratories reads the laboratory of the launching planet and those of the
// other planets of the player, which the research network may add.
func laboratories(ctx context.Context, tx *sql.Tx, playerID, planetID int64) (research.Laboratories, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT p.id, COALESCE(b.level, 0)
		FROM planets p
		LEFT JOIN planet_buildings b ON b.planet_id = p.id AND b.building_id = 'research_lab'
		WHERE p.owner_player_id = ?
		ORDER BY p.id
	`, playerID)
	if err != nil {
		return research.Laboratories{}, fmt.Errorf("research repository: read laboratories: %w", err)
	}
	defer rows.Close()
	var laboratories research.Laboratories
	for rows.Next() {
		var id int64
		var level int
		if err := rows.Scan(&id, &level); err != nil {
			return research.Laboratories{}, fmt.Errorf("research repository: scan laboratory: %w", err)
		}
		if id == planetID {
			laboratories.Local = level
			continue
		}
		laboratories.Remote = append(laboratories.Remote, level)
	}
	if err := rows.Err(); err != nil {
		return research.Laboratories{}, fmt.Errorf("research repository: iterate laboratories: %w", err)
	}
	var network int
	err = tx.QueryRowContext(ctx, `
		SELECT COALESCE(level, 0) FROM player_research WHERE player_id = ? AND research_id = ?
	`, playerID, string(research.IntergalacticResearchNetwork)).Scan(&network)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return research.Laboratories{}, fmt.Errorf("research repository: read research network: %w", err)
	}
	laboratories.NetworkLevel = network
	return laboratories, nil
}

// laboratoryIsIdle refuses a research while the laboratory sits anywhere in the
// building queue of the planet, running or waiting. Looking at the whole queue
// keeps the exclusion true at every instant, without a queue ever stalling.
func laboratoryIsIdle(ctx context.Context, tx *sql.Tx, planetID int64) error {
	var busy bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM building_queue WHERE planet_id = ? AND state IN ('active', 'queued') AND building_id = 'research_lab')
	`, planetID).Scan(&busy); err != nil {
		return fmt.Errorf("research repository: inspect laboratory: %w", err)
	}
	if busy {
		return appresearch.ErrLaboratoryBusy
	}
	return nil
}
