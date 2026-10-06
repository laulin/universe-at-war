-- Persistent player conversations. Typing presence deliberately stays out of
-- the database: it is transient UI state and expires after a few seconds.
CREATE TABLE chat_conversations (
    id INTEGER PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('direct', 'alliance')),
    alliance_id INTEGER REFERENCES alliances(id) ON DELETE CASCADE,
    player_one_id INTEGER REFERENCES players(id) ON DELETE CASCADE,
    player_two_id INTEGER REFERENCES players(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CHECK (
        (kind = 'direct' AND alliance_id IS NULL AND player_one_id IS NOT NULL
            AND player_two_id IS NOT NULL AND player_one_id < player_two_id)
        OR
        (kind = 'alliance' AND alliance_id IS NOT NULL
            AND player_one_id IS NULL AND player_two_id IS NULL)
    )
) STRICT;

CREATE UNIQUE INDEX chat_conversations_direct_idx
    ON chat_conversations (player_one_id, player_two_id) WHERE kind = 'direct';
CREATE UNIQUE INDEX chat_conversations_alliance_idx
    ON chat_conversations (alliance_id) WHERE kind = 'alliance';
CREATE INDEX chat_conversations_updated_idx
    ON chat_conversations (updated_at DESC, id DESC);

CREATE TABLE chat_messages (
    id INTEGER PRIMARY KEY,
    conversation_id INTEGER NOT NULL REFERENCES chat_conversations(id) ON DELETE CASCADE,
    author_player_id INTEGER REFERENCES players(id) ON DELETE SET NULL,
    author_name TEXT NOT NULL CHECK (length(trim(author_name)) BETWEEN 3 AND 32),
    body TEXT NOT NULL DEFAULT '' CHECK (length(body) <= 2000),
    gif_url TEXT CHECK (gif_url IS NULL OR (length(gif_url) <= 2048 AND gif_url LIKE 'https://%')),
    client_key TEXT NOT NULL CHECK (length(client_key) BETWEEN 8 AND 128),
    created_at TEXT NOT NULL,
    CHECK (length(trim(body)) > 0 OR gif_url IS NOT NULL),
    UNIQUE (conversation_id, author_player_id, client_key)
) STRICT;

CREATE INDEX chat_messages_conversation_idx
    ON chat_messages (conversation_id, id DESC);
