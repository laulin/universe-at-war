-- One monotonic read cursor per player and conversation keeps unread tracking
-- compact even for alliance rooms. The repository verifies that the cursor
-- names a message from the same conversation before it is advanced.
CREATE TABLE chat_read_states (
    conversation_id INTEGER NOT NULL REFERENCES chat_conversations(id) ON DELETE CASCADE,
    player_id INTEGER NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    last_read_message_id INTEGER NOT NULL CHECK (last_read_message_id > 0),
    read_at TEXT NOT NULL,
    PRIMARY KEY (conversation_id, player_id)
) STRICT;

CREATE INDEX chat_read_states_player_idx
    ON chat_read_states (player_id, conversation_id);

-- A database upgraded from the version that had no read tracking cannot know
-- which historical messages were actually seen. Start every existing member
-- at the latest message so deploying the feature does not announce the whole
-- archive as new; messages inserted after this migration follow normal rules.
INSERT INTO chat_read_states(conversation_id, player_id, last_read_message_id, read_at)
SELECT c.id, c.player_one_id, MAX(m.id), c.updated_at
FROM chat_conversations c
JOIN chat_messages m ON m.conversation_id = c.id
WHERE c.kind = 'direct'
GROUP BY c.id, c.player_one_id
UNION ALL
SELECT c.id, c.player_two_id, MAX(m.id), c.updated_at
FROM chat_conversations c
JOIN chat_messages m ON m.conversation_id = c.id
WHERE c.kind = 'direct'
GROUP BY c.id, c.player_two_id
UNION ALL
SELECT c.id, membership.player_id, MAX(m.id), c.updated_at
FROM chat_conversations c
JOIN chat_messages m ON m.conversation_id = c.id
JOIN alliance_members membership ON membership.alliance_id = c.alliance_id
WHERE c.kind = 'alliance'
GROUP BY c.id, membership.player_id;
