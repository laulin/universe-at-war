package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	appacs "universeatwar/internal/app/acs"
	appalliance "universeatwar/internal/app/alliance"
	appfleet "universeatwar/internal/app/fleet"
	domainacs "universeatwar/internal/domain/acs"
	"universeatwar/internal/domain/catalogue"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/rules"
	"universeatwar/internal/domain/universe"
)

// ACSRepository keeps a grouped operation and its fleets consistent: one
// arrival event for the whole group, one fleet at a time joining or leaving.
type ACSRepository struct {
	write      *sql.DB
	catalogues catalogue.Set
	fleets     *FleetRepository
}

func NewACSRepository(write *sql.DB, catalogues catalogue.Set, fleets *FleetRepository) *ACSRepository {
	return &ACSRepository{write: write, catalogues: catalogues, fleets: fleets}
}

// RegisterHandlers plugs the resolution of a grouped operation into the shared
// event processor.
func (r *ACSRepository) RegisterHandlers(processor *EventProcessor) {
	processor.Register("acs_resolved", func(ctx context.Context, tx *sql.Tx, event ScheduledEvent, now time.Time) error {
		return r.resolveGroup(ctx, tx, event, now)
	})
}

// Groups lists the operations of the alliance of an account.
func (r *ACSRepository) Groups(ctx context.Context, accountID int64, now time.Time) ([]appacs.Group, error) {
	var groups []appacs.Group
	err := withWriteTx(ctx, r.write, "acs repository: groups", func(tx *sql.Tx) error {
		playerID, allianceID, _, err := allianceOf(ctx, tx, accountID)
		if err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `
			SELECT id FROM acs_groups WHERE alliance_id = ? AND state IN ('forming', 'locked')
			ORDER BY arrives_at, id
		`, allianceID)
		if err != nil {
			return fmt.Errorf("acs repository: list groups: %w", err)
		}
		defer rows.Close()
		var identifiers []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return fmt.Errorf("acs repository: scan group: %w", err)
			}
			identifiers = append(identifiers, id)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		for _, id := range identifiers {
			group, err := loadGroup(ctx, tx, id, playerID)
			if err != nil {
				return err
			}
			groups = append(groups, group)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return groups, nil
}

// Group returns one operation of the alliance of an account.
func (r *ACSRepository) Group(ctx context.Context, accountID, groupID int64, now time.Time) (appacs.Group, error) {
	var group appacs.Group
	err := withWriteTx(ctx, r.write, "acs repository: group", func(tx *sql.Tx) error {
		playerID, allianceID, _, err := allianceOf(ctx, tx, accountID)
		if err != nil {
			return err
		}
		row, err := groupRow(ctx, tx, groupID)
		if err != nil {
			return err
		}
		if row.allianceID != allianceID {
			return appacs.ErrNotFound
		}
		group, err = loadGroup(ctx, tx, groupID, playerID)
		return err
	})
	if err != nil {
		return appacs.Group{}, err
	}
	return group, nil
}

// Create opens an operation and engages its first fleet in one transaction.
func (r *ACSRepository) Create(ctx context.Context, accountID, planetID int64, request appfleet.LaunchRequest,
	seed int64, idempotencyKey string, now time.Time) (appacs.Group, error) {
	var group appacs.Group
	err := withWriteTx(ctx, r.write, "acs repository: create", func(tx *sql.Tx) error {
		playerID, allianceID, configured, err := allianceOf(ctx, tx, accountID)
		if err != nil {
			return err
		}
		if !configured.Team.ACSEnabled {
			return appacs.ErrDisabled
		}
		if request.Mission != domainfleet.MissionAttack {
			return appacs.ErrInvalidRequest
		}
		fleet, plan, rulesetVersion, err := r.fleets.launchInto(ctx, tx, accountID, planetID, request, seed, idempotencyKey, now, false)
		if err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `
			INSERT INTO acs_groups(owner_player_id, alliance_id, kind, target_galaxy, target_system, target_position,
				seed, ruleset_version, arrives_at, created_at)
			VALUES (?, ?, 'attack', ?, ?, ?, ?, ?, ?, ?)
		`, playerID, allianceID, request.Target.Galaxy, request.Target.System, request.Target.Position,
			seed, rulesetVersion, timestamp(plan.ArrivesAt), timestamp(now))
		if err != nil {
			return fmt.Errorf("acs repository: create group: %w", err)
		}
		groupID, err := result.LastInsertId()
		if err != nil {
			return fmt.Errorf("acs repository: group id: %w", err)
		}
		if err := addParticipant(ctx, tx, groupID, fleet.ID, playerID, now); err != nil {
			return err
		}
		if err := scheduleGroupArrival(ctx, tx, groupID, rulesetVersion, plan.ArrivesAt, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO game_event_log(event_type, actor_type, actor_id, entity_type, entity_id, occurred_at, payload)
			VALUES ('acs_opened', 'account', ?, 'acs_group', ?, ?, json_object('target', ?, 'arrives_at', ?))
		`, accountID, groupID, timestamp(now), request.Target.String(), timestamp(plan.ArrivesAt)); err != nil {
			return fmt.Errorf("acs repository: log opening: %w", err)
		}
		group, err = loadGroup(ctx, tx, groupID, playerID)
		return err
	})
	if err != nil {
		return appacs.Group{}, err
	}
	return group, nil
}

// Preview calculates what a fleet would do to the schedule, without writing.
func (r *ACSRepository) Preview(ctx context.Context, accountID, groupID, planetID int64,
	request appfleet.LaunchRequest, now time.Time) (appacs.Preview, error) {
	transaction, err := r.write.BeginTx(ctx, nil)
	if err != nil {
		return appacs.Preview{}, fmt.Errorf("acs repository: begin preview: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	playerID, allianceID, _, err := allianceOf(ctx, transaction, accountID)
	if err != nil {
		return appacs.Preview{}, err
	}
	row, err := groupRow(ctx, transaction, groupID)
	if err != nil {
		return appacs.Preview{}, err
	}
	if row.allianceID != allianceID {
		return appacs.Preview{}, appacs.ErrNotFound
	}
	if err := row.acceptsFleets(now); err != nil {
		return appacs.Preview{}, err
	}
	joinRequest := request
	joinRequest.Target = row.target
	joinRequest.TargetKind = domainfleet.TargetPlanet
	joinRequest.Mission = domainfleet.MissionAttack
	plan, _, _, err := r.fleets.planLaunch(ctx, transaction, accountID, planetID, joinRequest, now)
	if err != nil {
		return appacs.Preview{}, err
	}
	group, err := loadGroup(ctx, transaction, groupID, playerID)
	if err != nil {
		return appacs.Preview{}, err
	}
	arrival := domainacs.Synchronise(row.arrivesAt, plan.ArrivesAt)
	return appacs.Preview{
		Group: group, Plan: plan, GroupArrival: arrival,
		DelaysTheTeam: arrival.After(row.arrivesAt),
	}, nil
}

// Join engages one more fleet and resynchronises the whole operation.
func (r *ACSRepository) Join(ctx context.Context, accountID, groupID, planetID int64,
	request appfleet.LaunchRequest, idempotencyKey string, now time.Time) (appacs.Group, error) {
	var group appacs.Group
	err := withWriteTx(ctx, r.write, "acs repository: join", func(tx *sql.Tx) error {
		playerID, allianceID, configured, err := allianceOf(ctx, tx, accountID)
		if err != nil {
			return err
		}
		if !configured.Team.ACSEnabled {
			return appacs.ErrDisabled
		}
		row, err := groupRow(ctx, tx, groupID)
		if err != nil {
			return err
		}
		if row.allianceID != allianceID {
			return domainacs.ErrNotAMember
		}
		if err := row.acceptsFleets(now); err != nil {
			return err
		}
		participants, err := participantCount(ctx, tx, groupID)
		if err != nil {
			return err
		}
		if participants >= configured.Team.MaximumGroupSize {
			return domainacs.ErrGroupFull
		}
		joinRequest := request
		joinRequest.Target = row.target
		joinRequest.TargetKind = domainfleet.TargetPlanet
		joinRequest.Mission = domainfleet.MissionAttack
		fleet, plan, _, err := r.fleets.launchInto(ctx, tx, accountID, planetID, joinRequest, row.seed, idempotencyKey, now, false)
		if err != nil {
			return err
		}
		if err := addParticipant(ctx, tx, groupID, fleet.ID, playerID, now); err != nil {
			return err
		}
		arrival := domainacs.Synchronise(row.arrivesAt, plan.ArrivesAt)
		if err := resynchronise(ctx, tx, row, arrival, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO game_event_log(event_type, actor_type, actor_id, entity_type, entity_id, occurred_at, payload)
			VALUES ('acs_joined', 'account', ?, 'acs_group', ?, ?, json_object('fleet_id', ?, 'arrives_at', ?))
		`, accountID, groupID, timestamp(now), fleet.ID, timestamp(arrival)); err != nil {
			return fmt.Errorf("acs repository: log joining: %w", err)
		}
		group, err = loadGroup(ctx, tx, groupID, playerID)
		return err
	})
	if err != nil {
		return appacs.Group{}, err
	}
	return group, nil
}

// Withdraw recalls one's own fleet out of an operation, cancelling the whole
// operation when the last fleet leaves.
func (r *ACSRepository) Withdraw(ctx context.Context, accountID, fleetID int64, idempotencyKey string, now time.Time) error {
	return withWriteTx(ctx, r.write, "acs repository: withdraw", func(tx *sql.Tx) error {
		playerID, _, _, err := allianceOf(ctx, tx, accountID)
		if err != nil {
			return err
		}
		var groupID, ownerID int64
		err = tx.QueryRowContext(ctx,
			"SELECT group_id, player_id FROM acs_participants WHERE fleet_id = ?", fleetID).Scan(&groupID, &ownerID)
		if errors.Is(err, sql.ErrNoRows) {
			return appacs.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("acs repository: read participant: %w", err)
		}
		if ownerID != playerID {
			return appacs.ErrForbidden
		}
		row, err := groupRow(ctx, tx, groupID)
		if err != nil {
			return err
		}
		if err := row.acceptsFleets(now); err != nil {
			return err
		}
		if _, err := r.fleets.recallInto(ctx, tx, accountID, fleetID, idempotencyKey, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM acs_participants WHERE fleet_id = ?", fleetID); err != nil {
			return fmt.Errorf("acs repository: remove participant: %w", err)
		}
		remaining, err := participantCount(ctx, tx, groupID)
		if err != nil {
			return err
		}
		if remaining == 0 {
			if err := transitionGroup(ctx, tx, row, domainacs.Cancelled, now); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx,
				"UPDATE scheduled_events SET state = 'cancelled', processed_at = ? WHERE idempotency_key = ? AND state = 'pending'",
				timestamp(now), groupArrivalKey(groupID)); err != nil {
				return fmt.Errorf("acs repository: cancel arrival: %w", err)
			}
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO game_event_log(event_type, actor_type, actor_id, entity_type, entity_id, occurred_at, payload)
			VALUES ('acs_withdrawn', 'account', ?, 'acs_group', ?, ?, json_object('fleet_id', ?, 'remaining', ?))
		`, accountID, groupID, timestamp(now), fleetID, remaining); err != nil {
			return fmt.Errorf("acs repository: log withdrawal: %w", err)
		}
		return nil
	})
}

// allianceOf resolves the player, their alliance and the active rules, which
// every grouped operation needs before deciding anything.
func allianceOf(ctx context.Context, tx *sql.Tx, accountID int64) (int64, int64, rules.Ruleset, error) {
	playerID, err := playerByAccount(ctx, tx, accountID)
	if err != nil {
		return 0, 0, rules.Ruleset{}, err
	}
	allianceID, _, err := membership(ctx, tx, playerID)
	if err != nil {
		if errors.Is(err, appalliance.ErrNotAMember) {
			return 0, 0, rules.Ruleset{}, domainacs.ErrNotAMember
		}
		return 0, 0, rules.Ruleset{}, err
	}
	configured, _, err := activeRuleset(ctx, tx)
	if err != nil {
		return 0, 0, rules.Ruleset{}, err
	}
	return playerID, allianceID, configured, nil
}

func addParticipant(ctx context.Context, tx *sql.Tx, groupID, fleetID, playerID int64, now time.Time) error {
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO acs_participants(fleet_id, group_id, player_id, joined_at) VALUES (?, ?, ?, ?)",
		fleetID, groupID, playerID, timestamp(now)); err != nil {
		return fmt.Errorf("acs repository: add participant: %w", err)
	}
	return nil
}

func participantCount(ctx context.Context, tx *sql.Tx, groupID int64) (int, error) {
	var count int
	if err := tx.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM acs_participants WHERE group_id = ?", groupID).Scan(&count); err != nil {
		return 0, fmt.Errorf("acs repository: count participants: %w", err)
	}
	return count, nil
}

func groupArrivalKey(groupID int64) string {
	return fmt.Sprintf("acs-arrive:%d", groupID)
}

func scheduleGroupArrival(ctx context.Context, tx *sql.Tx, groupID, rulesetVersion int64, arrivesAt, now time.Time) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO scheduled_events(event_type, due_at, priority, entity_type, entity_id, ruleset_version, payload, idempotency_key, created_at)
		VALUES ('acs_resolved', ?, ?, 'acs_group', ?, ?, json_object(), ?, ?)
	`, timestamp(arrivesAt), combatEventPriority, strconv.FormatInt(groupID, 10), rulesetVersion,
		groupArrivalKey(groupID), timestamp(now)); err != nil {
		return fmt.Errorf("acs repository: schedule arrival: %w", err)
	}
	return nil
}

// resynchronise moves the whole operation, and every fleet of it, to a new
// arrival: a late fleet delays the team.
func resynchronise(ctx context.Context, tx *sql.Tx, row acsGroupRow, arrival time.Time, now time.Time) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE acs_groups SET arrives_at = ?, version = version + 1 WHERE id = ? AND state = 'forming' AND version = ?
	`, timestamp(arrival), row.id, row.version); err != nil {
		return fmt.Errorf("acs repository: move operation: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE scheduled_events SET due_at = ? WHERE idempotency_key = ? AND state = 'pending'",
		timestamp(arrival), groupArrivalKey(row.id)); err != nil {
		return fmt.Errorf("acs repository: move arrival: %w", err)
	}
	// Every fleet lands with the operation and then flies home for its own
	// travel time, which the gap between its arrival and its return preserves.
	if _, err := tx.ExecContext(ctx, `
		UPDATE fleets
		SET returns_at = strftime('%Y-%m-%dT%H:%M:%SZ', ?, '+' ||
			CAST(strftime('%s', returns_at) - strftime('%s', arrives_at) AS TEXT) || ' seconds'),
			arrives_at = ?,
			version = version + 1
		WHERE id IN (SELECT fleet_id FROM acs_participants WHERE group_id = ?)
			AND state = 'outbound' AND returns_at IS NOT NULL
	`, timestamp(arrival), timestamp(arrival), row.id); err != nil {
		return fmt.Errorf("acs repository: move fleets: %w", err)
	}
	return nil
}

func transitionGroup(ctx context.Context, tx *sql.Tx, row acsGroupRow, to domainacs.State, now time.Time) error {
	if !domainacs.CanTransition(row.state, to) {
		return fmt.Errorf("acs repository: forbidden transition %s to %s", row.state, to)
	}
	var resolved any
	if to == domainacs.Resolved || to == domainacs.Cancelled {
		resolved = timestamp(now)
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE acs_groups SET state = ?, resolved_at = ?, version = version + 1
		WHERE id = ? AND state = ? AND version = ?
	`, string(to), resolved, row.id, string(row.state), row.version)
	if err != nil {
		return fmt.Errorf("acs repository: move operation state: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("acs repository: move operation state: %w", err)
	}
	if affected != 1 {
		return errors.New("acs repository: the operation changed during the command")
	}
	return nil
}

// acsGroupRow is the persisted shape of a grouped operation.
type acsGroupRow struct {
	id             int64
	ownerPlayerID  int64
	allianceID     int64
	kind           domainacs.Kind
	target         universe.Coordinate
	seed           int64
	rulesetVersion int64
	arrivesAt      time.Time
	state          domainacs.State
	version        int64
}

// acceptsFleets reports whether the operation still takes fleets in or out.
func (row acsGroupRow) acceptsFleets(now time.Time) error {
	if !row.state.Open() {
		return domainacs.ErrNotForming
	}
	if !row.arrivesAt.After(now) {
		return domainacs.ErrTooLate
	}
	return nil
}

func groupRow(ctx context.Context, tx *sql.Tx, groupID int64) (acsGroupRow, error) {
	var row acsGroupRow
	var kind, state, arrivesText string
	err := tx.QueryRowContext(ctx, `
		SELECT id, owner_player_id, alliance_id, kind, target_galaxy, target_system, target_position,
			seed, ruleset_version, arrives_at, state, version
		FROM acs_groups WHERE id = ?
	`, groupID).Scan(&row.id, &row.ownerPlayerID, &row.allianceID, &kind,
		&row.target.Galaxy, &row.target.System, &row.target.Position,
		&row.seed, &row.rulesetVersion, &arrivesText, &state, &row.version)
	if errors.Is(err, sql.ErrNoRows) {
		return acsGroupRow{}, appacs.ErrNotFound
	}
	if err != nil {
		return acsGroupRow{}, fmt.Errorf("acs repository: read operation: %w", err)
	}
	row.kind = domainacs.Kind(kind)
	row.state = domainacs.State(state)
	row.arrivesAt, err = time.Parse(time.RFC3339Nano, arrivesText)
	if err != nil {
		return acsGroupRow{}, fmt.Errorf("acs repository: parse arrival: %w", err)
	}
	return row, nil
}

// loadGroup builds the view of one operation for one member.
func loadGroup(ctx context.Context, tx *sql.Tx, groupID, viewerID int64) (appacs.Group, error) {
	row, err := groupRow(ctx, tx, groupID)
	if err != nil {
		return appacs.Group{}, err
	}
	owner, err := playerName(ctx, tx, row.ownerPlayerID)
	if err != nil {
		return appacs.Group{}, err
	}
	participants, err := loadParticipants(ctx, tx, groupID, viewerID)
	if err != nil {
		return appacs.Group{}, err
	}
	configured, _, err := activeRuleset(ctx, tx)
	if err != nil {
		return appacs.Group{}, err
	}
	return appacs.Group{
		ID: row.id, OwnerName: owner, Kind: row.kind, Target: row.target, State: row.state,
		ArrivesAt: row.arrivesAt, Participants: participants, MaximumSize: configured.Team.MaximumGroupSize,
		Own: row.ownerPlayerID == viewerID,
	}, nil
}

func loadParticipants(ctx context.Context, tx *sql.Tx, groupID, viewerID int64) ([]appacs.Participant, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT a.fleet_id, a.player_id, p.display_name, f.origin_galaxy, f.origin_system, f.origin_position, a.joined_at
		FROM acs_participants a
		JOIN players p ON p.id = a.player_id
		JOIN fleets f ON f.id = a.fleet_id
		WHERE a.group_id = ?
		ORDER BY a.fleet_id
	`, groupID)
	if err != nil {
		return nil, fmt.Errorf("acs repository: read participants: %w", err)
	}
	defer rows.Close()
	var participants []appacs.Participant
	var identifiers []int64
	for rows.Next() {
		var participant appacs.Participant
		var playerID int64
		var joinedText string
		if err := rows.Scan(&participant.FleetID, &playerID, &participant.PlayerName,
			&participant.Origin.Galaxy, &participant.Origin.System, &participant.Origin.Position, &joinedText); err != nil {
			return nil, fmt.Errorf("acs repository: scan participant: %w", err)
		}
		participant.Own = playerID == viewerID
		participant.JoinedAt, err = time.Parse(time.RFC3339Nano, joinedText)
		if err != nil {
			return nil, fmt.Errorf("acs repository: parse join date: %w", err)
		}
		participants = append(participants, participant)
		identifiers = append(identifiers, participant.FleetID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// A member sees how many ships an ally brings, never which ones: an ally is
	// told what they could count from their own bridge.
	for index, fleetID := range identifiers {
		composition, err := loadComposition(ctx, tx, fleetID)
		if err != nil {
			return nil, err
		}
		participants[index].Ships = composition.Count()
		if participants[index].Own {
			participants[index].Composition = composition
		}
	}
	return participants, nil
}

// resolveGroup lands a whole operation at once: the group is locked, then every
// fleet still engaged applies its mission at the shared arrival time.
func (r *ACSRepository) resolveGroup(ctx context.Context, tx *sql.Tx, event ScheduledEvent, now time.Time) error {
	groupID, err := strconv.ParseInt(event.EntityID, 10, 64)
	if err != nil {
		return errors.New("acs repository: invalid operation reference")
	}
	row, err := groupRow(ctx, tx, groupID)
	if err != nil {
		return err
	}
	if row.state == domainacs.Resolved || row.state == domainacs.Cancelled {
		// The operation already ended: replaying its arrival changes nothing.
		return nil
	}
	if row.state == domainacs.Forming {
		if err := transitionGroup(ctx, tx, row, domainacs.Locked, now); err != nil {
			return err
		}
		if row, err = groupRow(ctx, tx, groupID); err != nil {
			return err
		}
	}
	fleets, err := participantFleets(ctx, tx, groupID)
	if err != nil {
		return err
	}
	engaged := 0
	for _, fleetID := range fleets {
		fleet, err := loadFleetRow(ctx, tx, fleetID)
		if err != nil {
			return err
		}
		if fleet.state != domainfleet.Outbound {
			// The fleet was withdrawn or already resolved.
			continue
		}
		if err := r.fleets.resolveFleetArrival(ctx, tx, fleet, row.arrivesAt, now); err != nil {
			return err
		}
		engaged++
	}
	if err := transitionGroup(ctx, tx, row, domainacs.Resolved, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO game_event_log(event_type, entity_type, entity_id, occurred_at, payload)
		VALUES ('acs_resolved', 'acs_group', ?, ?, json_object('target', ?, 'fleets', ?))
	`, groupID, timestamp(now), row.target.String(), engaged); err != nil {
		return fmt.Errorf("acs repository: log resolution: %w", err)
	}
	return nil
}

// participantFleets lists the fleets of an operation in a stable order, so two
// runs of the same operation resolve the same way.
func participantFleets(ctx context.Context, tx *sql.Tx, groupID int64) ([]int64, error) {
	rows, err := tx.QueryContext(ctx,
		"SELECT fleet_id FROM acs_participants WHERE group_id = ? ORDER BY fleet_id", groupID)
	if err != nil {
		return nil, fmt.Errorf("acs repository: list participants: %w", err)
	}
	defer rows.Close()
	var identifiers []int64
	for rows.Next() {
		var fleetID int64
		if err := rows.Scan(&fleetID); err != nil {
			return nil, fmt.Errorf("acs repository: scan participant: %w", err)
		}
		identifiers = append(identifiers, fleetID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("acs repository: iterate participants: %w", err)
	}
	return identifiers, nil
}
