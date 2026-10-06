package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	appai "universeatwar/internal/app/ai"
	"universeatwar/internal/domain/server"
)

// Census reports what the population reconciler compares its standing order
// against: whether the universe runs, the ruleset in force, how many players
// are active and how many identities have already been provisioned.
//
// A universe with no active ruleset is not broken, it is not started: it
// reports itself as not running rather than as an error.
func (r *AIRepository) Census(ctx context.Context) (appai.Census, error) {
	var state server.State
	if err := r.write.QueryRowContext(ctx, "SELECT state FROM server_state WHERE id = 1").Scan(&state); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return appai.Census{}, nil
		}
		return appai.Census{}, fmt.Errorf("ai repository: read universe state: %w", err)
	}
	if state != server.Running {
		return appai.Census{}, nil
	}
	configured, err := activeRulesetFrom(ctx, r.write)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return appai.Census{}, nil
		}
		return appai.Census{}, err
	}
	// A slot is spent as soon as anything of the player survives: its profile,
	// which a retirement keeps, or its empire, which nothing removes. Only an
	// account left behind by a birth that never reached either is free to be
	// tried again.
	var active, provisioned int
	if err := r.write.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM ai_profiles WHERE state = 'active'").Scan(&active); err != nil {
		return appai.Census{}, fmt.Errorf("ai repository: count active artificial population: %w", err)
	}
	if err := r.write.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM accounts a
		WHERE a.kind = 'ai' AND (
			a.status = 'active'
			OR EXISTS (SELECT 1 FROM ai_profiles p WHERE p.account_id = a.id)
			OR EXISTS (SELECT 1 FROM players pl WHERE pl.account_id = a.id))
	`).Scan(&provisioned); err != nil {
		return appai.Census{}, fmt.Errorf("ai repository: count artificial population: %w", err)
	}
	return appai.Census{Running: true, Rules: configured, Active: active, Provisioned: provisioned}, nil
}

// Departures finds objective exits: an active profile which owns no world any
// more. It also exposes stale memberships of already retired players so an
// upgraded universe repairs them through the same idempotent retirement path.
func (r *AIRepository) Departures(ctx context.Context, limit int) ([]appai.Departure, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := r.write.QueryContext(ctx, `
		SELECT p.player_id, p.state = 'active'
		FROM ai_profiles p
		WHERE (p.state = 'active' AND NOT EXISTS (
			SELECT 1 FROM planets b WHERE b.owner_player_id = p.player_id
		)) OR (p.state = 'retired' AND EXISTS (
			SELECT 1 FROM alliance_members m WHERE m.player_id = p.player_id
		))
		ORDER BY p.player_id LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("ai repository: read artificial departures: %w", err)
	}
	defer rows.Close()
	var departures []appai.Departure
	for rows.Next() {
		var departure appai.Departure
		if err := rows.Scan(&departure.PlayerID, &departure.Active); err != nil {
			return nil, fmt.Errorf("ai repository: scan artificial departure: %w", err)
		}
		departures = append(departures, departure)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ai repository: iterate artificial departures: %w", err)
	}
	return departures, nil
}

// Teams reports the strength of the named alliances, one entry per tag and in
// the order asked for, along with the active artificial players that belong to
// no alliance at all.
func (r *AIRepository) Teams(ctx context.Context, tags []string, spares int) ([]appai.TeamCensus, []int64, error) {
	strengths := make([]appai.TeamCensus, 0, len(tags))
	if len(tags) == 0 {
		return strengths, nil, nil
	}
	placeholders := make([]string, len(tags))
	arguments := make([]any, len(tags))
	for index, tag := range tags {
		placeholders[index], arguments[index] = "?", strings.ToUpper(strings.TrimSpace(tag))
	}
	rows, err := r.write.QueryContext(ctx, `
		SELECT a.tag, (SELECT COUNT(*) FROM alliance_members m WHERE m.alliance_id = a.id)
		FROM alliances a WHERE a.tag IN (`+strings.Join(placeholders, ", ")+`)
	`, arguments...)
	if err != nil {
		return nil, nil, fmt.Errorf("ai repository: read artificial teams: %w", err)
	}
	members := make(map[string]int, len(tags))
	for rows.Next() {
		var tag string
		var count int
		if err := rows.Scan(&tag, &count); err != nil {
			_ = rows.Close()
			return nil, nil, fmt.Errorf("ai repository: read artificial team: %w", err)
		}
		members[tag] = count
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, nil, fmt.Errorf("ai repository: read artificial teams: %w", err)
	}
	// A tag no alliance wears yet is a team of nobody, which the reconciler
	// founds the first time it needs a member there.
	for index, tag := range tags {
		strengths = append(strengths, appai.TeamCensus{Tag: tag, Members: members[arguments[index].(string)]})
	}
	if spares <= 0 {
		return strengths, nil, nil
	}
	loners, err := r.write.QueryContext(ctx, `
		SELECT p.player_id FROM ai_profiles p
		WHERE p.state = 'active'
			AND NOT EXISTS (SELECT 1 FROM alliance_members m WHERE m.player_id = p.player_id)
		ORDER BY p.player_id LIMIT ?
	`, spares)
	if err != nil {
		return nil, nil, fmt.Errorf("ai repository: read unallied artificial players: %w", err)
	}
	var unallied []int64
	for loners.Next() {
		var playerID int64
		if err := loners.Scan(&playerID); err != nil {
			_ = loners.Close()
			return nil, nil, fmt.Errorf("ai repository: read unallied artificial player: %w", err)
		}
		unallied = append(unallied, playerID)
	}
	if err := errors.Join(loners.Err(), loners.Close()); err != nil {
		return nil, nil, fmt.Errorf("ai repository: read unallied artificial players: %w", err)
	}
	return strengths, unallied, nil
}

// Unfinished lists births that stopped between the empire and the character:
// an artificial account whose player owns its world and carries no profile, so
// nothing has ever thought for it. Only accounts still in use are offered, since
// one that was disabled was put aside deliberately.
func (r *AIRepository) Unfinished(ctx context.Context, limit int) ([]appai.Unfinished, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := r.write.QueryContext(ctx, `
		SELECT a.id, pl.display_name
		FROM accounts a JOIN players pl ON pl.account_id = a.id
		WHERE a.kind = 'ai' AND a.status = 'active'
			AND NOT EXISTS (SELECT 1 FROM ai_profiles p WHERE p.account_id = a.id)
		ORDER BY a.id LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("ai repository: read unfinished births: %w", err)
	}
	var births []appai.Unfinished
	for rows.Next() {
		var birth appai.Unfinished
		if err := rows.Scan(&birth.AccountID, &birth.Name); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("ai repository: read unfinished birth: %w", err)
		}
		births = append(births, birth)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, fmt.Errorf("ai repository: read unfinished births: %w", err)
	}
	return births, nil
}
