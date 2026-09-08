package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	appresearch "universeatwar/internal/app/research"
	"universeatwar/internal/domain/catalogue"
	"universeatwar/internal/domain/prerequisite"
	"universeatwar/internal/domain/research"
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
		return completeResearch(ctx, tx, event, now)
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
		active, err := activeResearch(ctx, tx, playerID)
		if err != nil {
			return err
		}
		state = appresearch.State{
			Planet:       planet,
			Levels:       planet.Researches,
			Laboratories: laboratories,
			Active:       active,
		}
		return nil
	})
	if err != nil {
		return appresearch.State{}, err
	}
	return state, nil
}

// Start debits the cost, creates the queue, its event, its idempotency key and
// its journal entry in a single transaction.
func (r *ResearchRepository) Start(ctx context.Context, accountID, planetID int64, id research.ID, idempotencyKey string, now time.Time) (appresearch.Queue, error) {
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
		active, err := activeResearch(ctx, tx, playerID)
		if err != nil {
			return err
		}
		if active != nil {
			return appresearch.ErrQueueBusy
		}
		if err := laboratoryIsIdle(ctx, tx, planet.ID); err != nil {
			return err
		}
		laboratories, err := laboratories(ctx, tx, playerID, planet.ID)
		if err != nil {
			return err
		}
		plan, err := r.catalogues.Research.Plan(id, prerequisite.State{
			Buildings:  planet.Levels.Generic(),
			Researches: planet.Researches.Generic(),
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
		startedAt := now.UTC().Truncate(time.Second)
		completesAt := startedAt.Add(plan.Duration)
		result, err := tx.ExecContext(ctx, `
			INSERT INTO research_queue(player_id, planet_id, research_id, target_level, metal_cost, crystal_cost, deuterium_cost, energy_cost, effective_laboratory, ruleset_version, position, queued_at, started_at, completes_at, state)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?, 'active')
		`, playerID, planet.ID, string(id), plan.TargetLevel, plan.Cost.Metal, plan.Cost.Crystal, plan.Cost.Deuterium,
			plan.Energy, plan.EffectiveLaboratory, rulesetVersion, timestamp(startedAt), timestamp(startedAt), timestamp(completesAt))
		if err != nil {
			if strings.Contains(err.Error(), "research_queue_one_active_idx") || strings.Contains(err.Error(), "UNIQUE") {
				return appresearch.ErrQueueBusy
			}
			return fmt.Errorf("research repository: enqueue research: %w", err)
		}
		queueID, err := result.LastInsertId()
		if err != nil {
			return fmt.Errorf("research repository: queue id: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO scheduled_events(event_type, due_at, priority, entity_type, entity_id, ruleset_version, payload, idempotency_key, created_at)
			VALUES ('research_completed', ?, ?, 'research_queue', ?, ?, json_object('queue_id', ?), ?, ?)
		`, timestamp(completesAt), researchEventPriority, strconv.FormatInt(queueID, 10), rulesetVersion, queueID,
			fmt.Sprintf("research-complete:%d", queueID), timestamp(startedAt)); err != nil {
			return fmt.Errorf("research repository: schedule research: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO idempotency_keys(actor_id, operation, key, request_hash, result_type, result_id, created_at)
			VALUES (?, 'start_research', ?, ?, 'research_queue', ?, ?)
		`, strconv.FormatInt(accountID, 10), idempotencyKey, requestHash, strconv.FormatInt(queueID, 10), timestamp(startedAt)); err != nil {
			return fmt.Errorf("research repository: record idempotency: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO game_event_log(event_type, actor_type, actor_id, entity_type, entity_id, occurred_at, payload)
			VALUES ('research_started', 'account', ?, 'planet', ?, ?, json_object('research_id', ?, 'target_level', ?, 'queue_id', ?))
		`, accountID, planet.ID, timestamp(startedAt), string(id), plan.TargetLevel, queueID); err != nil {
			return fmt.Errorf("research repository: log research start: %w", err)
		}
		queue = appresearch.Queue{
			ID: queueID, PlanetID: planet.ID, Research: id, TargetLevel: plan.TargetLevel,
			Cost: plan.Cost, Energy: plan.Energy, StartedAt: startedAt, CompletesAt: completesAt, State: "active",
		}
		return nil
	})
	if err != nil {
		return appresearch.Queue{}, err
	}
	return queue, nil
}

// completeResearch raises the finished level exactly once.
func completeResearch(ctx context.Context, tx *sql.Tx, event ScheduledEvent, now time.Time) error {
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
	return nil
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

// activeResearch returns the running research of a player, if any.
func activeResearch(ctx context.Context, tx *sql.Tx, playerID int64) (*appresearch.Queue, error) {
	queue, err := loadResearchQueue(ctx, tx, "q.player_id = ? AND q.state = 'active'", playerID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return queue, err
}

func loadResearchQueue(ctx context.Context, tx *sql.Tx, condition string, argument any) (*appresearch.Queue, error) {
	var queue appresearch.Queue
	var researchID, startedText, completesText string
	err := tx.QueryRowContext(ctx, `
		SELECT q.id, q.planet_id, q.research_id, q.target_level, q.metal_cost, q.crystal_cost, q.deuterium_cost, q.energy_cost, q.started_at, q.completes_at, q.state
		FROM research_queue q WHERE `+condition, argument).
		Scan(&queue.ID, &queue.PlanetID, &researchID, &queue.TargetLevel, &queue.Cost.Metal, &queue.Cost.Crystal,
			&queue.Cost.Deuterium, &queue.Energy, &startedText, &completesText, &queue.State)
	if err != nil {
		return nil, err
	}
	queue.Research = research.ID(researchID)
	queue.StartedAt, err = time.Parse(time.RFC3339Nano, startedText)
	if err != nil {
		return nil, fmt.Errorf("research repository: parse start time: %w", err)
	}
	queue.CompletesAt, err = time.Parse(time.RFC3339Nano, completesText)
	if err != nil {
		return nil, fmt.Errorf("research repository: parse completion time: %w", err)
	}
	return &queue, nil
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

// laboratoryIsIdle refuses a research while the laboratory is being upgraded.
func laboratoryIsIdle(ctx context.Context, tx *sql.Tx, planetID int64) error {
	var busy bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM building_queue WHERE planet_id = ? AND state = 'active' AND building_id = 'research_lab')
	`, planetID).Scan(&busy); err != nil {
		return fmt.Errorf("research repository: inspect laboratory: %w", err)
	}
	if busy {
		return appresearch.ErrLaboratoryBusy
	}
	return nil
}
