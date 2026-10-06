package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	appchat "universeatwar/internal/app/chat"
)

// ChatRepository stores durable messages while checking membership against
// current player and alliance state on every read and write.
type ChatRepository struct {
	read  *sql.DB
	write *sql.DB
}

func NewChatRepository(read, write *sql.DB) *ChatRepository {
	return &ChatRepository{read: read, write: write}
}

type chatQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type conversationMeta struct {
	id                       int64
	kind                     appchat.Kind
	allianceID               sql.NullInt64
	playerOneID, playerTwoID sql.NullInt64
	playerOneName            sql.NullString
	playerTwoName            sql.NullString
	allianceName             sql.NullString
	allianceTag              sql.NullString
}

func (r *ChatRepository) ListForAccount(ctx context.Context, accountID int64) (appchat.Inbox, error) {
	playerID, _, err := chatPlayerByAccount(ctx, r.read, accountID)
	if err != nil {
		return appchat.Inbox{}, err
	}
	allianceID := int64(0)
	err = r.read.QueryRowContext(ctx,
		"SELECT alliance_id FROM alliance_members WHERE player_id = ?", playerID).Scan(&allianceID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return appchat.Inbox{}, fmt.Errorf("chat repository: read membership: %w", err)
	}
	rows, err := r.read.QueryContext(ctx, `
		SELECT c.id, c.kind, c.updated_at,
			p1.id, p1.display_name, p2.id, p2.display_name, a.name, a.tag,
			m.author_name, m.body, m.gif_url
		FROM chat_conversations c
		LEFT JOIN players p1 ON p1.id = c.player_one_id
		LEFT JOIN players p2 ON p2.id = c.player_two_id
		LEFT JOIN alliances a ON a.id = c.alliance_id
		LEFT JOIN chat_messages m ON m.id = (
			SELECT latest.id FROM chat_messages latest
			WHERE latest.conversation_id = c.id ORDER BY latest.id DESC LIMIT 1
		)
		WHERE (c.kind = 'direct' AND (c.player_one_id = ? OR c.player_two_id = ?))
			OR (c.kind = 'alliance' AND c.alliance_id = ?)
		ORDER BY c.updated_at DESC, c.id DESC
	`, playerID, playerID, allianceID)
	if err != nil {
		return appchat.Inbox{}, fmt.Errorf("chat repository: list conversations: %w", err)
	}
	defer rows.Close()
	summaries, err := scanConversationSummaries(rows, playerID, false)
	if err != nil {
		return appchat.Inbox{}, err
	}
	return appchat.Inbox{Conversations: summaries, HasAlliance: allianceID > 0}, nil
}

// UnreadCount counts incoming messages after the player's cursor in every room
// they can currently access. Alliance history from before the current
// membership is visible in the room but does not become a fresh notification.
func (r *ChatRepository) UnreadCount(ctx context.Context, accountID int64) (int, error) {
	playerID, _, err := chatPlayerByAccount(ctx, r.read, accountID)
	if err != nil {
		return 0, err
	}
	var count int
	err = r.read.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM chat_messages m
		JOIN chat_conversations c ON c.id = m.conversation_id
		LEFT JOIN chat_read_states state
			ON state.conversation_id = c.id AND state.player_id = ?
		LEFT JOIN alliance_members membership
			ON c.kind = 'alliance' AND membership.alliance_id = c.alliance_id
			AND membership.player_id = ?
		WHERE (
			(c.kind = 'direct' AND (c.player_one_id = ? OR c.player_two_id = ?))
			OR
			(c.kind = 'alliance' AND membership.player_id IS NOT NULL
				AND m.created_at >= membership.joined_at)
		)
		AND (m.author_player_id IS NULL OR m.author_player_id <> ?)
		AND m.id > COALESCE(state.last_read_message_id, 0)
	`, playerID, playerID, playerID, playerID, playerID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("chat repository: count unread: %w", err)
	}
	return count, nil
}

func (r *ChatRepository) OpenDirect(ctx context.Context, accountID, targetPlayerID int64, now time.Time, limit int) (appchat.Conversation, error) {
	var conversation appchat.Conversation
	err := withWriteTx(ctx, r.write, "chat repository: open direct", func(tx *sql.Tx) error {
		playerID, _, err := chatPlayerByAccount(ctx, tx, accountID)
		if err != nil {
			return err
		}
		if playerID == targetPlayerID {
			return appchat.ErrSelfConversation
		}
		var targetName string
		if err := tx.QueryRowContext(ctx, "SELECT display_name FROM players WHERE id = ?", targetPlayerID).Scan(&targetName); errors.Is(err, sql.ErrNoRows) {
			return appchat.ErrNoSuchPlayer
		} else if err != nil {
			return fmt.Errorf("chat repository: read target player: %w", err)
		}
		one, two := playerID, targetPlayerID
		if one > two {
			one, two = two, one
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO chat_conversations(kind, player_one_id, player_two_id, created_at, updated_at)
			VALUES ('direct', ?, ?, ?, ?)
			ON CONFLICT (player_one_id, player_two_id) WHERE kind = 'direct' DO NOTHING
		`, one, two, timestamp(now), timestamp(now)); err != nil {
			return fmt.Errorf("chat repository: create direct conversation: %w", err)
		}
		var conversationID int64
		if err := tx.QueryRowContext(ctx, `
			SELECT id FROM chat_conversations
			WHERE kind = 'direct' AND player_one_id = ? AND player_two_id = ?
		`, one, two).Scan(&conversationID); err != nil {
			return fmt.Errorf("chat repository: read direct conversation: %w", err)
		}
		conversation, err = conversationForPlayer(ctx, tx, conversationID, playerID, 0, limit)
		return err
	})
	return conversation, err
}

func (r *ChatRepository) OpenAlliance(ctx context.Context, accountID int64, now time.Time, limit int) (appchat.Conversation, error) {
	var conversation appchat.Conversation
	err := withWriteTx(ctx, r.write, "chat repository: open alliance", func(tx *sql.Tx) error {
		playerID, _, err := chatPlayerByAccount(ctx, tx, accountID)
		if err != nil {
			return err
		}
		var allianceID int64
		err = tx.QueryRowContext(ctx,
			"SELECT alliance_id FROM alliance_members WHERE player_id = ?", playerID).Scan(&allianceID)
		if errors.Is(err, sql.ErrNoRows) {
			return appchat.ErrNotInAlliance
		}
		if err != nil {
			return fmt.Errorf("chat repository: read alliance membership: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO chat_conversations(kind, alliance_id, created_at, updated_at)
			VALUES ('alliance', ?, ?, ?)
			ON CONFLICT (alliance_id) WHERE kind = 'alliance' DO NOTHING
		`, allianceID, timestamp(now), timestamp(now)); err != nil {
			return fmt.Errorf("chat repository: create alliance conversation: %w", err)
		}
		var conversationID int64
		if err := tx.QueryRowContext(ctx,
			"SELECT id FROM chat_conversations WHERE kind = 'alliance' AND alliance_id = ?", allianceID).
			Scan(&conversationID); err != nil {
			return fmt.Errorf("chat repository: read alliance conversation: %w", err)
		}
		conversation, err = conversationForPlayer(ctx, tx, conversationID, playerID, 0, limit)
		return err
	})
	return conversation, err
}

func (r *ChatRepository) ConversationForAccount(ctx context.Context, accountID, conversationID, afterID int64, limit int) (appchat.Conversation, error) {
	playerID, _, err := chatPlayerByAccount(ctx, r.read, accountID)
	if err != nil {
		return appchat.Conversation{}, err
	}
	return conversationForPlayer(ctx, r.read, conversationID, playerID, afterID, limit)
}

// MarkReadThrough advances one player's cursor without ever allowing an older
// request to move it backwards. Authorization and message ownership are checked
// in the same transaction as the update.
func (r *ChatRepository) MarkReadThrough(ctx context.Context, accountID, conversationID, messageID int64, now time.Time) error {
	if messageID <= 0 {
		return nil
	}
	return withWriteTx(ctx, r.write, "chat repository: mark read", func(tx *sql.Tx) error {
		playerID, _, err := chatPlayerByAccount(ctx, tx, accountID)
		if err != nil {
			return err
		}
		meta, err := readConversationMeta(ctx, tx, conversationID)
		if err != nil {
			return err
		}
		if err := authorizeConversation(ctx, tx, meta, playerID); err != nil {
			return err
		}
		var belongs int
		if err := tx.QueryRowContext(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM chat_messages WHERE id = ? AND conversation_id = ?
			)
		`, messageID, conversationID).Scan(&belongs); err != nil {
			return fmt.Errorf("chat repository: inspect read cursor: %w", err)
		}
		if belongs == 0 {
			return appchat.ErrNotFound
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO chat_read_states(conversation_id, player_id, last_read_message_id, read_at)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(conversation_id, player_id) DO UPDATE SET
				last_read_message_id = MAX(chat_read_states.last_read_message_id, excluded.last_read_message_id),
				read_at = CASE
					WHEN excluded.last_read_message_id > chat_read_states.last_read_message_id
					THEN excluded.read_at ELSE chat_read_states.read_at END
		`, conversationID, playerID, messageID, timestamp(now)); err != nil {
			return fmt.Errorf("chat repository: advance read cursor: %w", err)
		}
		return nil
	})
}

func (r *ChatRepository) Append(ctx context.Context, accountID, conversationID int64, draft appchat.Draft, now time.Time) (appchat.Message, error) {
	var message appchat.Message
	err := withWriteTx(ctx, r.write, "chat repository: append", func(tx *sql.Tx) error {
		playerID, playerName, err := chatPlayerByAccount(ctx, tx, accountID)
		if err != nil {
			return err
		}
		meta, err := readConversationMeta(ctx, tx, conversationID)
		if err != nil {
			return err
		}
		if err := authorizeConversation(ctx, tx, meta, playerID); err != nil {
			return err
		}
		var gif any
		if draft.GIFURL != "" {
			gif = draft.GIFURL
		}
		result, err := tx.ExecContext(ctx, `
			INSERT INTO chat_messages(conversation_id, author_player_id, author_name, body, gif_url, client_key, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (conversation_id, author_player_id, client_key) DO NOTHING
		`, conversationID, playerID, playerName, draft.Body, gif, draft.ClientKey, timestamp(now))
		if err != nil {
			return fmt.Errorf("chat repository: insert message: %w", err)
		}
		inserted, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("chat repository: message result: %w", err)
		}
		if inserted > 0 {
			if _, err := tx.ExecContext(ctx,
				"UPDATE chat_conversations SET updated_at = ? WHERE id = ?", timestamp(now), conversationID); err != nil {
				return fmt.Errorf("chat repository: touch conversation: %w", err)
			}
		}
		row := tx.QueryRowContext(ctx, `
			SELECT id, conversation_id, author_player_id, author_name, body, gif_url, created_at
			FROM chat_messages
			WHERE conversation_id = ? AND author_player_id = ? AND client_key = ?
		`, conversationID, playerID, draft.ClientKey)
		message, err = scanChatMessage(row, playerID)
		return err
	})
	return message, err
}

func (r *ChatRepository) MemberForConversation(ctx context.Context, accountID, conversationID int64) (appchat.Member, error) {
	playerID, name, err := chatPlayerByAccount(ctx, r.read, accountID)
	if err != nil {
		return appchat.Member{}, err
	}
	meta, err := readConversationMeta(ctx, r.read, conversationID)
	if err != nil {
		return appchat.Member{}, err
	}
	if err := authorizeConversation(ctx, r.read, meta, playerID); err != nil {
		return appchat.Member{}, err
	}
	return appchat.Member{PlayerID: playerID, Name: name}, nil
}

func (r *ChatRepository) ListAll(ctx context.Context) ([]appchat.Summary, error) {
	rows, err := r.read.QueryContext(ctx, `
		SELECT c.id, c.kind, c.updated_at,
			p1.id, p1.display_name, p2.id, p2.display_name, a.name, a.tag,
			m.author_name, m.body, m.gif_url
		FROM chat_conversations c
		LEFT JOIN players p1 ON p1.id = c.player_one_id
		LEFT JOIN players p2 ON p2.id = c.player_two_id
		LEFT JOIN alliances a ON a.id = c.alliance_id
		LEFT JOIN chat_messages m ON m.id = (
			SELECT latest.id FROM chat_messages latest
			WHERE latest.conversation_id = c.id ORDER BY latest.id DESC LIMIT 1
		)
		ORDER BY c.updated_at DESC, c.id DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("chat repository: list all conversations: %w", err)
	}
	defer rows.Close()
	return scanConversationSummaries(rows, 0, true)
}

func (r *ChatRepository) ConversationForAdministrator(ctx context.Context, conversationID, afterID int64, limit int) (appchat.Conversation, error) {
	meta, err := readConversationMeta(ctx, r.read, conversationID)
	if err != nil {
		return appchat.Conversation{}, err
	}
	conversation := conversationFromMeta(meta, 0, true)
	conversation.Messages, err = loadChatMessages(ctx, r.read, conversationID, afterID, limit, 0)
	if err != nil {
		return appchat.Conversation{}, err
	}
	initiatorID, initiatorName, err := conversationInitiator(ctx, r.read, conversationID)
	if err != nil {
		return appchat.Conversation{}, err
	}
	for index := range conversation.Messages {
		message := &conversation.Messages[index]
		if initiatorID > 0 {
			message.FromInitiator = message.AuthorPlayerID == initiatorID
		} else {
			// A deleted author leaves their immutable display name on each
			// message, which still keeps the historical sides stable.
			message.FromInitiator = initiatorName != "" && message.AuthorName == initiatorName
		}
	}
	return conversation, nil
}

func conversationInitiator(ctx context.Context, query chatQueryer, conversationID int64) (int64, string, error) {
	var authorID sql.NullInt64
	var authorName string
	err := query.QueryRowContext(ctx, `
		SELECT author_player_id, author_name
		FROM chat_messages
		WHERE conversation_id = ?
		ORDER BY id
		LIMIT 1
	`, conversationID).Scan(&authorID, &authorName)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", nil
	}
	if err != nil {
		return 0, "", fmt.Errorf("chat repository: read conversation initiator: %w", err)
	}
	return authorID.Int64, authorName, nil
}

func chatPlayerByAccount(ctx context.Context, query chatQueryer, accountID int64) (int64, string, error) {
	var playerID int64
	var name string
	err := query.QueryRowContext(ctx,
		"SELECT id, display_name FROM players WHERE account_id = ?", accountID).Scan(&playerID, &name)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", appchat.ErrNoPlayer
	}
	if err != nil {
		return 0, "", fmt.Errorf("chat repository: read player: %w", err)
	}
	return playerID, name, nil
}

func readConversationMeta(ctx context.Context, query chatQueryer, conversationID int64) (conversationMeta, error) {
	var meta conversationMeta
	var kind string
	err := query.QueryRowContext(ctx, `
		SELECT c.id, c.kind, c.alliance_id, c.player_one_id, c.player_two_id,
			p1.display_name, p2.display_name, a.name, a.tag
		FROM chat_conversations c
		LEFT JOIN players p1 ON p1.id = c.player_one_id
		LEFT JOIN players p2 ON p2.id = c.player_two_id
		LEFT JOIN alliances a ON a.id = c.alliance_id
		WHERE c.id = ?
	`, conversationID).Scan(&meta.id, &kind, &meta.allianceID, &meta.playerOneID, &meta.playerTwoID,
		&meta.playerOneName, &meta.playerTwoName, &meta.allianceName, &meta.allianceTag)
	if errors.Is(err, sql.ErrNoRows) {
		return conversationMeta{}, appchat.ErrNotFound
	}
	if err != nil {
		return conversationMeta{}, fmt.Errorf("chat repository: read conversation: %w", err)
	}
	meta.kind = appchat.Kind(kind)
	return meta, nil
}

func authorizeConversation(ctx context.Context, query chatQueryer, meta conversationMeta, playerID int64) error {
	switch meta.kind {
	case appchat.Direct:
		if meta.playerOneID.Int64 != playerID && meta.playerTwoID.Int64 != playerID {
			return appchat.ErrNotFound
		}
	case appchat.Alliance:
		var exists int
		err := query.QueryRowContext(ctx, `
			SELECT EXISTS(SELECT 1 FROM alliance_members WHERE player_id = ? AND alliance_id = ?)
		`, playerID, meta.allianceID.Int64).Scan(&exists)
		if err != nil {
			return fmt.Errorf("chat repository: authorize alliance conversation: %w", err)
		}
		if exists == 0 {
			return appchat.ErrNotFound
		}
	default:
		return appchat.ErrNotFound
	}
	return nil
}

func conversationForPlayer(ctx context.Context, query chatQueryer, conversationID, playerID, afterID int64, limit int) (appchat.Conversation, error) {
	meta, err := readConversationMeta(ctx, query, conversationID)
	if err != nil {
		return appchat.Conversation{}, err
	}
	if err := authorizeConversation(ctx, query, meta, playerID); err != nil {
		return appchat.Conversation{}, err
	}
	conversation := conversationFromMeta(meta, playerID, false)
	conversation.Messages, err = loadChatMessages(ctx, query, conversationID, afterID, limit, playerID)
	return conversation, err
}

func conversationFromMeta(meta conversationMeta, playerID int64, administrator bool) appchat.Conversation {
	conversation := appchat.Conversation{ID: meta.id, Kind: meta.kind}
	if meta.kind == appchat.Alliance {
		conversation.AllianceID = meta.allianceID.Int64
		conversation.Title = "Alliance [" + meta.allianceTag.String + "] " + meta.allianceName.String
		return conversation
	}
	if administrator {
		conversation.Title = meta.playerOneName.String + " ↔ " + meta.playerTwoName.String
		return conversation
	}
	if meta.playerOneID.Int64 == playerID {
		conversation.TargetPlayerID = meta.playerTwoID.Int64
		conversation.Title = meta.playerTwoName.String
	} else {
		conversation.TargetPlayerID = meta.playerOneID.Int64
		conversation.Title = meta.playerOneName.String
	}
	return conversation
}

func loadChatMessages(ctx context.Context, query chatQueryer, conversationID, afterID int64, limit int, viewerPlayerID int64) ([]appchat.Message, error) {
	if limit < 1 || limit > 100 {
		limit = 100
	}
	statement := `
		SELECT id, conversation_id, author_player_id, author_name, body, gif_url, created_at
		FROM chat_messages WHERE conversation_id = ? AND id > ? ORDER BY id LIMIT ?
	`
	arguments := []any{conversationID, afterID, limit}
	if afterID == 0 {
		statement = `
			SELECT id, conversation_id, author_player_id, author_name, body, gif_url, created_at
			FROM (
				SELECT id, conversation_id, author_player_id, author_name, body, gif_url, created_at
				FROM chat_messages WHERE conversation_id = ? ORDER BY id DESC LIMIT ?
			) recent ORDER BY id
		`
		arguments = []any{conversationID, limit}
	}
	rows, err := query.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return nil, fmt.Errorf("chat repository: read messages: %w", err)
	}
	defer rows.Close()
	var messages []appchat.Message
	for rows.Next() {
		message, err := scanChatMessage(rows, viewerPlayerID)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, rows.Err()
}

type chatMessageScanner interface {
	Scan(...any) error
}

func scanChatMessage(scanner chatMessageScanner, viewerPlayerID int64) (appchat.Message, error) {
	var message appchat.Message
	var authorID sql.NullInt64
	var gifURL sql.NullString
	var createdText string
	if err := scanner.Scan(&message.ID, &message.ConversationID, &authorID, &message.AuthorName,
		&message.Body, &gifURL, &createdText); err != nil {
		return appchat.Message{}, fmt.Errorf("chat repository: scan message: %w", err)
	}
	message.AuthorPlayerID = authorID.Int64
	message.GIFURL = gifURL.String
	message.Own = authorID.Valid && viewerPlayerID > 0 && authorID.Int64 == viewerPlayerID
	createdAt, err := time.Parse(time.RFC3339Nano, createdText)
	if err != nil {
		return appchat.Message{}, fmt.Errorf("chat repository: parse message time: %w", err)
	}
	message.CreatedAt = createdAt
	return message, nil
}

func scanConversationSummaries(rows *sql.Rows, viewerPlayerID int64, administrator bool) ([]appchat.Summary, error) {
	var summaries []appchat.Summary
	for rows.Next() {
		var summary appchat.Summary
		var kind, updatedText string
		var oneID, twoID sql.NullInt64
		var oneName, twoName, allianceName, allianceTag sql.NullString
		var author, body, gifURL sql.NullString
		if err := rows.Scan(&summary.ID, &kind, &updatedText,
			&oneID, &oneName, &twoID, &twoName, &allianceName, &allianceTag,
			&author, &body, &gifURL); err != nil {
			return nil, fmt.Errorf("chat repository: scan conversation: %w", err)
		}
		summary.Kind = appchat.Kind(kind)
		if summary.Kind == appchat.Alliance {
			summary.Title = "Alliance [" + allianceTag.String + "] " + allianceName.String
		} else if administrator {
			summary.Title = oneName.String + " ↔ " + twoName.String
		} else if oneID.Int64 == viewerPlayerID {
			summary.Title = twoName.String
		} else {
			summary.Title = oneName.String
		}
		summary.LastMessage = "Aucun message"
		if author.Valid {
			preview := body.String
			if preview == "" && gifURL.Valid {
				preview = "GIF"
			}
			summary.LastMessage = author.String + " : " + preview
		}
		updatedAt, err := time.Parse(time.RFC3339Nano, updatedText)
		if err != nil {
			return nil, fmt.Errorf("chat repository: parse conversation time: %w", err)
		}
		summary.UpdatedAt = updatedAt
		summaries = append(summaries, summary)
	}
	return summaries, rows.Err()
}
