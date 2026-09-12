package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	appadmin "universeatwar/internal/app/administration"
	"universeatwar/internal/domain/building"
	"universeatwar/internal/domain/server"
)

// DashboardRepository reads the state of a running universe for its
// administrator. It only ever reads what the database already knows.
type DashboardRepository struct {
	write     *sql.DB
	path      string
	startedAt time.Time
}

func NewDashboardRepository(write *sql.DB, path string, startedAt time.Time) *DashboardRepository {
	return &DashboardRepository{write: write, path: path, startedAt: startedAt}
}

// Health gathers what an administrator needs at a glance.
func (r *DashboardRepository) Health(ctx context.Context, now time.Time) (appadmin.Health, error) {
	health := appadmin.Health{StartedAt: r.startedAt, Uptime: now.Sub(r.startedAt)}
	if health.Uptime < 0 {
		health.Uptime = 0
	}
	var state string
	if err := r.write.QueryRowContext(ctx, "SELECT state FROM server_state WHERE id = 1").Scan(&state); err != nil &&
		!errors.Is(err, sql.ErrNoRows) {
		return appadmin.Health{}, fmt.Errorf("dashboard repository: read state: %w", err)
	}
	health.State = server.State(state)
	if err := r.write.QueryRowContext(ctx,
		"SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&health.SchemaVersion); err != nil {
		return appadmin.Health{}, fmt.Errorf("dashboard repository: read schema: %w", err)
	}
	if err := r.write.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&health.JournalMode); err != nil {
		return appadmin.Health{}, fmt.Errorf("dashboard repository: read journal: %w", err)
	}
	var pageSize, pageCount int64
	if err := r.write.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize); err != nil {
		return appadmin.Health{}, fmt.Errorf("dashboard repository: read page size: %w", err)
	}
	if err := r.write.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pageCount); err != nil {
		return appadmin.Health{}, fmt.Errorf("dashboard repository: read page count: %w", err)
	}
	health.DatabaseBytes = pageSize * pageCount

	var effective sql.NullString
	if err := r.write.QueryRowContext(ctx,
		"SELECT version, effective_at FROM ruleset_versions WHERE status = 'active'").
		Scan(&health.RulesetVersion, &effective); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return appadmin.Health{}, fmt.Errorf("dashboard repository: read ruleset: %w", err)
	}
	if effective.Valid {
		parsed, err := time.Parse(time.RFC3339Nano, effective.String)
		if err != nil {
			return appadmin.Health{}, fmt.Errorf("dashboard repository: parse ruleset date: %w", err)
		}
		health.RulesetSince = parsed
	}

	var nextDue sql.NullString
	if err := r.write.QueryRowContext(ctx, `
		SELECT due_at, event_type FROM scheduled_events
		WHERE state = 'pending' AND attempts < 5 ORDER BY due_at, priority, id LIMIT 1
	`).Scan(&nextDue, &health.NextEventType); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return appadmin.Health{}, fmt.Errorf("dashboard repository: read next event: %w", err)
	}
	if nextDue.Valid {
		parsed, err := time.Parse(time.RFC3339Nano, nextDue.String)
		if err != nil {
			return appadmin.Health{}, fmt.Errorf("dashboard repository: parse next event: %w", err)
		}
		health.NextEventAt = &parsed
	}
	for _, counter := range []struct {
		query string
		into  *int
		args  []any
	}{
		{"SELECT COUNT(*) FROM scheduled_events WHERE state = 'pending' AND due_at <= ?", &health.PendingEvents,
			[]any{timestamp(now)}},
		{"SELECT COUNT(*) FROM scheduled_events WHERE state = 'pending' AND attempts >= 5", &health.FailedEvents, nil},
		{"SELECT COUNT(*) FROM scheduled_events WHERE state = 'completed' AND processed_at > ?", &health.RecentEvents,
			[]any{timestamp(now.Add(-time.Hour))}},
		{"SELECT COUNT(*) FROM accounts", &health.Accounts, nil},
		{"SELECT COUNT(*) FROM players", &health.Players, nil},
		{"SELECT COUNT(*) FROM ai_profiles WHERE state = 'active'", &health.Artificials, nil},
		{`SELECT COUNT(*) FROM bans WHERE status = 'active' AND starts_at <= ? AND (ends_at IS NULL OR ends_at > ?)`,
			&health.ActiveBans, []any{timestamp(now), timestamp(now)}},
	} {
		if err := r.write.QueryRowContext(ctx, counter.query, counter.args...).Scan(counter.into); err != nil {
			return appadmin.Health{}, fmt.Errorf("dashboard repository: count: %w", err)
		}
	}
	var backupName sql.NullString
	var backupTaken sql.NullString
	if err := r.write.QueryRowContext(ctx,
		"SELECT name, taken_at FROM backups ORDER BY taken_at DESC, id DESC LIMIT 1").
		Scan(&backupName, &backupTaken); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return appadmin.Health{}, fmt.Errorf("dashboard repository: read backup: %w", err)
	}
	if backupTaken.Valid {
		taken, err := time.Parse(time.RFC3339Nano, backupTaken.String)
		if err != nil {
			return appadmin.Health{}, fmt.Errorf("dashboard repository: parse backup date: %w", err)
		}
		health.LastBackupAt = &taken
		health.LastBackupName = backupName.String
	}
	return health, nil
}

// Accounts lists every account with its roles, its status and whether it plays.
func (r *DashboardRepository) Accounts(ctx context.Context, now time.Time) ([]appadmin.Account, error) {
	rows, err := r.write.QueryContext(ctx, `
		SELECT a.id, a.username, a.kind, a.status, a.created_at,
			EXISTS(SELECT 1 FROM players p WHERE p.account_id = a.id),
			EXISTS(SELECT 1 FROM bans b WHERE b.account_id = a.id AND b.status = 'active'
				AND b.starts_at <= ? AND (b.ends_at IS NULL OR b.ends_at > ?)),
			COALESCE((SELECT GROUP_CONCAT(role) FROM account_roles r WHERE r.account_id = a.id), '')
		FROM accounts a ORDER BY a.id
	`, timestamp(now), timestamp(now))
	if err != nil {
		return nil, fmt.Errorf("dashboard repository: list accounts: %w", err)
	}
	defer rows.Close()
	var accounts []appadmin.Account
	for rows.Next() {
		var account appadmin.Account
		var createdText, roles string
		if err := rows.Scan(&account.ID, &account.Username, &account.Kind, &account.Status, &createdText,
			&account.HasEmpire, &account.Banned, &roles); err != nil {
			return nil, fmt.Errorf("dashboard repository: scan account: %w", err)
		}
		account.CreatedAt, err = time.Parse(time.RFC3339Nano, createdText)
		if err != nil {
			return nil, fmt.Errorf("dashboard repository: parse creation: %w", err)
		}
		account.Roles = splitRoles(roles)
		accounts = append(accounts, account)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dashboard repository: iterate accounts: %w", err)
	}
	return accounts, nil
}

// ActiveGameSettings reads the immutable document currently in force.
func (r *DashboardRepository) ActiveGameSettings(ctx context.Context) (appadmin.StoredGameSettings, error) {
	var stored appadmin.StoredGameSettings
	var document, effective string
	if err := r.write.QueryRowContext(ctx, `
		SELECT document, version, effective_at FROM ruleset_versions
		WHERE status = 'active' ORDER BY version DESC LIMIT 1
	`).Scan(&document, &stored.Version, &effective); err != nil {
		return appadmin.StoredGameSettings{}, fmt.Errorf("dashboard repository: read game settings: %w", err)
	}
	parsed, err := time.Parse(time.RFC3339Nano, effective)
	if err != nil {
		return appadmin.StoredGameSettings{}, fmt.Errorf("dashboard repository: parse game settings date: %w", err)
	}
	stored.Document = []byte(document)
	stored.EffectiveAt = parsed
	return stored, nil
}

// ReplaceGameSettings settles every planet at the version boundary, supersedes
// the current document and publishes a new immutable active version in one
// transaction.
func (r *DashboardRepository) ReplaceGameSettings(ctx context.Context, actorID, expectedVersion int64,
	document []byte, checksum, justification string, now time.Time) (appadmin.StoredGameSettings, error) {
	var stored appadmin.StoredGameSettings
	err := withWriteTx(ctx, r.write, "dashboard repository: replace game settings", func(tx *sql.Tx) error {
		var activeVersion int64
		if err := tx.QueryRowContext(ctx, `
			SELECT version FROM ruleset_versions WHERE status = 'active'
			ORDER BY version DESC LIMIT 1
		`).Scan(&activeVersion); err != nil {
			return fmt.Errorf("dashboard repository: read active ruleset: %w", err)
		}
		if activeVersion != expectedVersion {
			return appadmin.ErrRulesConflict
		}

		// Lazy production must be materialised under the old rules before the
		// active document changes, otherwise the new speed would apply to the
		// whole interval since each planet was last visited.
		rows, err := tx.QueryContext(ctx, "SELECT id FROM planets ORDER BY id")
		if err != nil {
			return fmt.Errorf("dashboard repository: list planets: %w", err)
		}
		var planetIDs []int64
		for rows.Next() {
			var planetID int64
			if err := rows.Scan(&planetID); err != nil {
				_ = rows.Close()
				return fmt.Errorf("dashboard repository: scan planet: %w", err)
			}
			planetIDs = append(planetIDs, planetID)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("dashboard repository: iterate planets: %w", err)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("dashboard repository: close planets: %w", err)
		}
		for _, planetID := range planetIDs {
			planet, _, production, err := loadPlanetByID(ctx, tx, planetID, now, building.DefaultCatalogue())
			if err != nil {
				return fmt.Errorf("dashboard repository: settle planet %d: %w", planetID, err)
			}
			if err := persistProduction(ctx, tx, planet.ID, production); err != nil {
				return err
			}
		}

		result, err := tx.ExecContext(ctx,
			"UPDATE ruleset_versions SET status = 'superseded' WHERE status = 'active' AND version = ?",
			expectedVersion)
		if err != nil {
			return fmt.Errorf("dashboard repository: supersede ruleset: %w", err)
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return appadmin.ErrRulesConflict
		}
		if err := tx.QueryRowContext(ctx,
			"SELECT COALESCE(MAX(version), 0) + 1 FROM ruleset_versions").Scan(&stored.Version); err != nil {
			return fmt.Errorf("dashboard repository: select ruleset version: %w", err)
		}
		effectiveAt := now.UTC()
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO ruleset_versions(
				version, status, document, checksum, author_account_id, justification, effective_at, created_at
			) VALUES (?, 'active', ?, ?, ?, ?, ?, ?)
		`, stored.Version, string(document), checksum, actorID, justification,
			timestamp(effectiveAt), timestamp(effectiveAt)); err != nil {
			return fmt.Errorf("dashboard repository: publish ruleset: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO audit_log(actor_account_id, action, target_type, target_id, occurred_at, details)
			VALUES (?, 'ruleset_updated', 'ruleset', ?, ?,
				json_object('previous_version', ?, 'version', ?, 'justification', ?))
		`, actorID, stored.Version, timestamp(effectiveAt), expectedVersion, stored.Version, justification); err != nil {
			return fmt.Errorf("dashboard repository: audit ruleset: %w", err)
		}
		stored.Document = append([]byte(nil), document...)
		stored.EffectiveAt = effectiveAt
		return nil
	})
	if err != nil {
		return appadmin.StoredGameSettings{}, err
	}
	return stored, nil
}

// SetRole grants or withdraws a role.
func (r *DashboardRepository) SetRole(ctx context.Context, authorID, accountID int64, role string,
	granted bool, now time.Time) error {
	return withWriteTx(ctx, r.write, "dashboard repository: set role", func(tx *sql.Tx) error {
		var exists bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM accounts WHERE id = ?)", accountID).
			Scan(&exists); err != nil {
			return fmt.Errorf("dashboard repository: read account: %w", err)
		}
		if !exists {
			return appadmin.ErrAccountNotFound
		}
		action := "role_revoked"
		if granted {
			action = "role_granted"
			if _, err := tx.ExecContext(ctx, `
				INSERT OR IGNORE INTO account_roles(account_id, role, granted_at, granted_by_account_id)
				VALUES (?, ?, ?, ?)
			`, accountID, role, timestamp(now), authorID); err != nil {
				return fmt.Errorf("dashboard repository: grant role: %w", err)
			}
		} else if _, err := tx.ExecContext(ctx,
			"DELETE FROM account_roles WHERE account_id = ? AND role = ?", accountID, role); err != nil {
			return fmt.Errorf("dashboard repository: revoke role: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO audit_log(actor_account_id, action, target_type, target_id, occurred_at, details)
			VALUES (?, ?, 'account', ?, ?, json_object('role', ?))
		`, authorID, action, accountID, timestamp(now), role); err != nil {
			return fmt.Errorf("dashboard repository: audit role: %w", err)
		}
		return nil
	})
}

// SetStatus enables or disables an account and closes its sessions when it is
// disabled. The empire behind it keeps producing.
func (r *DashboardRepository) SetStatus(ctx context.Context, authorID, accountID int64, status string,
	now time.Time) error {
	return withWriteTx(ctx, r.write, "dashboard repository: set status", func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx,
			"UPDATE accounts SET status = ?, version = version + 1, updated_at = ? WHERE id = ?",
			status, timestamp(now), accountID)
		if err != nil {
			return fmt.Errorf("dashboard repository: set status: %w", err)
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return appadmin.ErrAccountNotFound
		}
		if status == "disabled" {
			if _, err := tx.ExecContext(ctx,
				"UPDATE sessions SET revoked_at = ? WHERE account_id = ? AND revoked_at IS NULL",
				timestamp(now), accountID); err != nil {
				return fmt.Errorf("dashboard repository: close sessions: %w", err)
			}
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO audit_log(actor_account_id, action, target_type, target_id, occurred_at, details)
			VALUES (?, 'account_status_changed', 'account', ?, ?, json_object('status', ?))
		`, authorID, accountID, timestamp(now), status); err != nil {
			return fmt.Errorf("dashboard repository: audit status: %w", err)
		}
		return nil
	})
}

// splitRoles turns the grouped roles of one account into a list.
func splitRoles(joined string) []string {
	if joined == "" {
		return nil
	}
	var roles []string
	current := ""
	for _, character := range joined {
		if character == ',' {
			roles = append(roles, current)
			current = ""
			continue
		}
		current += string(character)
	}
	return append(roles, current)
}
