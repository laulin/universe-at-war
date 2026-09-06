package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	appai "universeatwar/internal/app/ai"
	domainai "universeatwar/internal/domain/ai"
	"universeatwar/internal/domain/universe"
)

// thinkEventPriority puts a reflection after every consequence of the same
// instant: an artificial player thinks once the world has already changed.
const thinkEventPriority = 90

// AIRepository persists the artificial players, their diary and their memory.
type AIRepository struct {
	write *sql.DB
}

func NewAIRepository(write *sql.DB) *AIRepository {
	return &AIRepository{write: write}
}

// RegisterHandlers plugs the reflection clock into the shared event processor.
// The handler never deliberates: it moves the schedule forward and, outside the
// activity hours, it puts the player back to sleep without a single decision.
func (r *AIRepository) RegisterHandlers(processor *EventProcessor) {
	processor.Register("ai_think", func(ctx context.Context, tx *sql.Tx, event ScheduledEvent, now time.Time) error {
		return r.tick(ctx, tx, event, now)
	})
}

func (r *AIRepository) tick(ctx context.Context, tx *sql.Tx, event ScheduledEvent, now time.Time) error {
	playerID, err := strconv.ParseInt(event.EntityID, 10, 64)
	if err != nil {
		return errors.New("ai repository: invalid player reference")
	}
	row, found, err := profileRow(ctx, tx, playerID)
	if err != nil || !found {
		// A retired or deleted player owes nothing any more.
		return err
	}
	if row.retired {
		return nil
	}
	if row.nextThinkAt != nil && event.DueAt.Before(*row.nextThinkAt) {
		// A schedule already moved past this tick: the event is stale.
		return nil
	}
	profile := row.profile()
	tick := row.tick + 1
	awake := profile.Window.Awake(event.DueAt)
	from := event.DueAt
	if !awake {
		from = profile.Window.NextOpening(event.DueAt)
	}
	next := domainai.NextThink(from, profile.Interval, profile.Seed, tick)

	var due any
	if awake {
		due = timestamp(event.DueAt)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE ai_profiles SET tick = ?, next_think_at = ?, due_think_at = ?, version = version + 1
		WHERE player_id = ? AND version = ?
	`, tick, timestamp(next), due, playerID, row.version); err != nil {
		return fmt.Errorf("ai repository: move the schedule: %w", err)
	}
	if !awake {
		if err := insertDecision(ctx, tx, playerID, event.DueAt, domainai.Decision{
			Layer: domainai.Strategic, Action: "sleep", Outcome: domainai.Skipped,
			Reason: "outside the activity hours",
		}); err != nil {
			return err
		}
	}
	return scheduleThink(ctx, tx, playerID, tick, event.RulesetVersion, next, now)
}

// scheduleThink arms the next reflection. Each tick carries its own key, so a
// reflection is planned exactly once and a stale one cannot come back.
func scheduleThink(ctx context.Context, tx *sql.Tx, playerID, tick, rulesetVersion int64, at, now time.Time) error {
	var version any
	if rulesetVersion > 0 {
		version = rulesetVersion
	} else {
		active, err := activeRulesetVersion(ctx, tx)
		if err != nil {
			return err
		}
		version = active
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO scheduled_events(event_type, due_at, priority, entity_type, entity_id, ruleset_version, payload, idempotency_key, created_at)
		VALUES ('ai_think', ?, ?, 'player', ?, ?, json_object('tick', ?), ?, ?)
	`, timestamp(at), thinkEventPriority, strconv.FormatInt(playerID, 10), version, tick,
		fmt.Sprintf("ai-think:%d:%d", playerID, tick), timestamp(now)); err != nil {
		return fmt.Errorf("ai repository: schedule the reflection: %w", err)
	}
	return nil
}

// CreateAccount opens the account of an artificial player. It carries no
// credential, so no session can ever be opened on it.
func (r *AIRepository) CreateAccount(ctx context.Context, name string, now time.Time) (int64, error) {
	var accountID int64
	err := withWriteTx(ctx, r.write, "ai repository: create account", func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `
			INSERT INTO accounts(username, username_normalized, kind, created_at, updated_at)
			VALUES (?, ?, 'ai', ?, ?)
		`, name, strings.ToLower(strings.TrimSpace(name)), timestamp(now), timestamp(now))
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return appai.ErrNameTaken
			}
			return fmt.Errorf("ai repository: create account: %w", err)
		}
		accountID, err = result.LastInsertId()
		if err != nil {
			return fmt.Errorf("ai repository: account id: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO audit_log(actor_account_id, action, target_type, target_id, occurred_at, details)
			VALUES (?, 'ai_created', 'account', ?, ?, json_object('name', ?))
		`, accountID, accountID, timestamp(now), name); err != nil {
			return fmt.Errorf("ai repository: log creation: %w", err)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return accountID, nil
}

// DisableAccount closes the account of an artificial player that could not be
// brought to life.
func (r *AIRepository) DisableAccount(ctx context.Context, accountID int64, now time.Time) error {
	return withWriteTx(ctx, r.write, "ai repository: disable account", func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			"UPDATE accounts SET status = 'disabled', updated_at = ? WHERE id = ?", timestamp(now), accountID); err != nil {
			return fmt.Errorf("ai repository: disable account: %w", err)
		}
		return nil
	})
}

// CreateProfile gives an artificial player its character and its first
// reflection.
func (r *AIRepository) CreateProfile(ctx context.Context, accountID int64, profile domainai.Profile,
	first, now time.Time) (appai.Profile, error) {
	var created appai.Profile
	err := withWriteTx(ctx, r.write, "ai repository: create profile", func(tx *sql.Tx) error {
		playerID, err := playerByAccount(ctx, tx, accountID)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO ai_profiles(player_id, account_id, archetype, activity_start_hour, activity_end_hour,
				think_interval_seconds, seed, next_think_at, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, playerID, accountID, string(profile.Archetype), profile.Window.Start, profile.Window.End,
			int64(profile.Interval/time.Second), profile.Seed, timestamp(first), timestamp(now)); err != nil {
			return fmt.Errorf("ai repository: create profile: %w", err)
		}
		if err := scheduleThink(ctx, tx, playerID, 0, 0, first, now); err != nil {
			return err
		}
		created, err = loadAIProfile(ctx, tx, playerID, 0, now)
		return err
	})
	if err != nil {
		return appai.Profile{}, err
	}
	return created, nil
}

// Retire stops an artificial player and cancels the reflection it was owed.
func (r *AIRepository) Retire(ctx context.Context, playerID int64, now time.Time) error {
	return withWriteTx(ctx, r.write, "ai repository: retire", func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `
			UPDATE ai_profiles SET state = 'retired', retired_at = ?, next_think_at = NULL,
				due_think_at = NULL, version = version + 1
			WHERE player_id = ? AND state = 'active'
		`, timestamp(now), playerID)
		if err != nil {
			return fmt.Errorf("ai repository: retire: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("ai repository: retire: %w", err)
		}
		if affected != 1 {
			var exists bool
			if err := tx.QueryRowContext(ctx,
				"SELECT EXISTS(SELECT 1 FROM ai_profiles WHERE player_id = ?)", playerID).Scan(&exists); err != nil {
				return fmt.Errorf("ai repository: inspect profile: %w", err)
			}
			if !exists {
				return appai.ErrNotFound
			}
			// Already retired: retiring again changes nothing.
			return nil
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE scheduled_events SET state = 'cancelled', processed_at = ?
			WHERE entity_type = 'player' AND entity_id = ? AND event_type = 'ai_think' AND state = 'pending'
		`, timestamp(now), strconv.FormatInt(playerID, 10)); err != nil {
			return fmt.Errorf("ai repository: cancel the reflection: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE accounts SET status = 'disabled', updated_at = ?
			WHERE id = (SELECT account_id FROM ai_profiles WHERE player_id = ?)
		`, timestamp(now), playerID); err != nil {
			return fmt.Errorf("ai repository: disable account: %w", err)
		}
		return nil
	})
}

// List reports on every artificial player of the universe.
func (r *AIRepository) List(ctx context.Context, now time.Time) ([]appai.Profile, error) {
	var profiles []appai.Profile
	err := withWriteTx(ctx, r.write, "ai repository: list", func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, "SELECT player_id FROM ai_profiles ORDER BY player_id")
		if err != nil {
			return fmt.Errorf("ai repository: list: %w", err)
		}
		defer rows.Close()
		var identifiers []int64
		for rows.Next() {
			var playerID int64
			if err := rows.Scan(&playerID); err != nil {
				return fmt.Errorf("ai repository: scan profile: %w", err)
			}
			identifiers = append(identifiers, playerID)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("ai repository: iterate profiles: %w", err)
		}
		for _, playerID := range identifiers {
			profile, err := loadAIProfile(ctx, tx, playerID, 0, now)
			if err != nil {
				return err
			}
			profiles = append(profiles, profile)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return profiles, nil
}

// Inspect opens the diary and the memory of one artificial player.
func (r *AIRepository) Inspect(ctx context.Context, playerID int64, limit int, now time.Time) (appai.Profile, error) {
	var profile appai.Profile
	err := withWriteTx(ctx, r.write, "ai repository: inspect", func(tx *sql.Tx) error {
		var err error
		profile, err = loadAIProfile(ctx, tx, playerID, limit, now)
		return err
	})
	if err != nil {
		return appai.Profile{}, err
	}
	return profile, nil
}

// Due lists the artificial players that owe a reflection.
func (r *AIRepository) Due(ctx context.Context, now time.Time, limit int) ([]domainai.Profile, error) {
	rows, err := r.write.QueryContext(ctx, `
		SELECT player_id FROM ai_profiles
		WHERE state = 'active' AND due_think_at IS NOT NULL
		ORDER BY due_think_at, player_id LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("ai repository: list due: %w", err)
	}
	defer rows.Close()
	var identifiers []int64
	for rows.Next() {
		var playerID int64
		if err := rows.Scan(&playerID); err != nil {
			return nil, fmt.Errorf("ai repository: scan due: %w", err)
		}
		identifiers = append(identifiers, playerID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ai repository: iterate due: %w", err)
	}
	profiles := make([]domainai.Profile, 0, len(identifiers))
	for _, playerID := range identifiers {
		var row aiProfileRow
		err := withWriteTx(ctx, r.write, "ai repository: read due", func(tx *sql.Tx) error {
			loaded, found, err := profileRow(ctx, tx, playerID)
			if err != nil || !found {
				return err
			}
			row = loaded
			return nil
		})
		if err != nil {
			return nil, err
		}
		if row.playerID == 0 {
			continue
		}
		profiles = append(profiles, row.profile())
	}
	return profiles, nil
}

// Complete records what a reflection concluded and closes it.
func (r *AIRepository) Complete(ctx context.Context, playerID int64, decisions []domainai.Decision, now time.Time) error {
	return withWriteTx(ctx, r.write, "ai repository: complete", func(tx *sql.Tx) error {
		for _, decision := range decisions {
			if err := insertDecision(ctx, tx, playerID, now, decision); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE ai_profiles SET due_think_at = NULL, last_think_at = ?, version = version + 1
			WHERE player_id = ?
		`, timestamp(now), playerID); err != nil {
			return fmt.Errorf("ai repository: close the reflection: %w", err)
		}
		return nil
	})
}

func insertDecision(ctx context.Context, tx *sql.Tx, playerID int64, at time.Time, decision domainai.Decision) error {
	var target any
	if decision.Target != nil {
		target = decision.Target.String()
	}
	var body any
	if decision.BodyID > 0 {
		body = decision.BodyID
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO ai_decisions(player_id, decided_at, layer, action, outcome, reason, score, body_id, target)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, playerID, timestamp(at), string(decision.Layer), decision.Action, string(decision.Outcome),
		decision.Reason, decision.Score, body, target); err != nil {
		return fmt.Errorf("ai repository: record decision: %w", err)
	}
	return nil
}

// aiProfileRow is the persisted shape of an artificial player.
type aiProfileRow struct {
	playerID    int64
	accountID   int64
	name        string
	archetype   string
	window      domainai.Window
	interval    time.Duration
	seed        int64
	tick        int64
	retired     bool
	nextThinkAt *time.Time
	dueThinkAt  *time.Time
	lastThinkAt *time.Time
	createdAt   time.Time
	version     int64
}

func (row aiProfileRow) profile() domainai.Profile {
	return domainai.Profile{
		PlayerID: row.playerID, AccountID: row.accountID, Name: row.name,
		Archetype: domainai.Archetype(row.archetype), Window: row.window,
		Interval: row.interval, Seed: row.seed, Tick: row.tick, Retired: row.retired,
	}
}

func profileRow(ctx context.Context, tx *sql.Tx, playerID int64) (aiProfileRow, bool, error) {
	var row aiProfileRow
	var state, createdText string
	var seconds int64
	var nextText, dueText, lastText sql.NullString
	err := tx.QueryRowContext(ctx, `
		SELECT p.player_id, p.account_id, pl.display_name, p.archetype, p.activity_start_hour, p.activity_end_hour,
			p.think_interval_seconds, p.seed, p.tick, p.state, p.next_think_at, p.due_think_at, p.last_think_at,
			p.created_at, p.version
		FROM ai_profiles p JOIN players pl ON pl.id = p.player_id
		WHERE p.player_id = ?
	`, playerID).Scan(&row.playerID, &row.accountID, &row.name, &row.archetype, &row.window.Start, &row.window.End,
		&seconds, &row.seed, &row.tick, &state, &nextText, &dueText, &lastText, &createdText, &row.version)
	if errors.Is(err, sql.ErrNoRows) {
		return aiProfileRow{}, false, nil
	}
	if err != nil {
		return aiProfileRow{}, false, fmt.Errorf("ai repository: read profile: %w", err)
	}
	row.interval = time.Duration(seconds) * time.Second
	row.retired = state == "retired"
	row.createdAt, err = time.Parse(time.RFC3339Nano, createdText)
	if err != nil {
		return aiProfileRow{}, false, fmt.Errorf("ai repository: parse creation: %w", err)
	}
	for _, moment := range []struct {
		text  sql.NullString
		field **time.Time
	}{{nextText, &row.nextThinkAt}, {dueText, &row.dueThinkAt}, {lastText, &row.lastThinkAt}} {
		if !moment.text.Valid {
			continue
		}
		parsed, parseErr := time.Parse(time.RFC3339Nano, moment.text.String)
		if parseErr != nil {
			return aiProfileRow{}, false, fmt.Errorf("ai repository: parse schedule: %w", parseErr)
		}
		*moment.field = &parsed
	}
	return row, true, nil
}

// loadProfile builds the administration view of one artificial player.
func loadAIProfile(ctx context.Context, tx *sql.Tx, playerID int64, diary int, now time.Time) (appai.Profile, error) {
	row, found, err := profileRow(ctx, tx, playerID)
	if err != nil {
		return appai.Profile{}, err
	}
	if !found {
		return appai.Profile{}, appai.ErrNotFound
	}
	profile := appai.Profile{
		Profile: row.profile(), CreatedAt: row.createdAt,
		NextThinkAt: row.nextThinkAt, LastThinkAt: row.lastThinkAt,
		Awake: row.window.Awake(now),
	}
	if err := tx.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM planets WHERE owner_player_id = ?", playerID).Scan(&profile.Bodies); err != nil {
		return appai.Profile{}, fmt.Errorf("ai repository: count bodies: %w", err)
	}
	if diary <= 0 {
		return profile, nil
	}
	profile.Decisions, err = loadDecisions(ctx, tx, playerID, diary)
	if err != nil {
		return appai.Profile{}, err
	}
	profile.Memories, err = loadMemories(ctx, tx, playerID, diary)
	if err != nil {
		return appai.Profile{}, err
	}
	return profile, nil
}

func loadDecisions(ctx context.Context, tx *sql.Tx, playerID int64, limit int) ([]appai.Decision, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT decided_at, layer, action, outcome, reason, score, COALESCE(body_id, 0), COALESCE(target, '')
		FROM ai_decisions WHERE player_id = ? ORDER BY id DESC LIMIT ?
	`, playerID, limit)
	if err != nil {
		return nil, fmt.Errorf("ai repository: read diary: %w", err)
	}
	defer rows.Close()
	var decisions []appai.Decision
	for rows.Next() {
		var decision appai.Decision
		var decidedText, layer, outcome string
		if err := rows.Scan(&decidedText, &layer, &decision.Action, &outcome, &decision.Reason,
			&decision.Score, &decision.BodyID, &decision.Target); err != nil {
			return nil, fmt.Errorf("ai repository: scan decision: %w", err)
		}
		decision.Layer = domainai.Layer(layer)
		decision.Outcome = domainai.Outcome(outcome)
		decision.DecidedAt, err = time.Parse(time.RFC3339Nano, decidedText)
		if err != nil {
			return nil, fmt.Errorf("ai repository: parse decision date: %w", err)
		}
		decisions = append(decisions, decision)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ai repository: iterate diary: %w", err)
	}
	return decisions, nil
}

func loadMemories(ctx context.Context, tx *sql.Tx, playerID int64, limit int) ([]appai.Memory, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT kind, galaxy, system, position, observed_at, score,
			COALESCE(json_extract(payload, '$.summary'), '')
		FROM ai_memory WHERE player_id = ? ORDER BY observed_at DESC, id DESC LIMIT ?
	`, playerID, limit)
	if err != nil {
		return nil, fmt.Errorf("ai repository: read memory: %w", err)
	}
	defer rows.Close()
	var memories []appai.Memory
	for rows.Next() {
		var memory appai.Memory
		var observedText string
		var at universe.Coordinate
		if err := rows.Scan(&memory.Kind, &at.Galaxy, &at.System, &at.Position,
			&observedText, &memory.Score, &memory.Summary); err != nil {
			return nil, fmt.Errorf("ai repository: scan memory: %w", err)
		}
		memory.Coordinate = at
		var err error
		memory.ObservedAt, err = time.Parse(time.RFC3339Nano, observedText)
		if err != nil {
			return nil, fmt.Errorf("ai repository: parse observation date: %w", err)
		}
		memories = append(memories, memory)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ai repository: iterate memory: %w", err)
	}
	return memories, nil
}
