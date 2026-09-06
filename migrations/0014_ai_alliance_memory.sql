-- The common memory of an alliance of artificial players. Every belief carries
-- its author, its date, its confidence and its expiry; a belief drawn from a
-- report dies with the sharing of that report.

CREATE TABLE ai_alliance_memory (
    id INTEGER PRIMARY KEY,
    alliance_id INTEGER NOT NULL REFERENCES alliances(id) ON DELETE CASCADE,
    author_player_id INTEGER NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('target', 'threat', 'debris', 'capability')),
    galaxy INTEGER NOT NULL CHECK (galaxy > 0),
    system INTEGER NOT NULL CHECK (system > 0),
    position INTEGER NOT NULL CHECK (position > 0),
    source_report_id INTEGER REFERENCES reports(id) ON DELETE CASCADE,
    observed_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    confidence REAL NOT NULL CHECK (confidence >= 0 AND confidence <= 1),
    payload_version INTEGER NOT NULL CHECK (payload_version > 0),
    payload TEXT NOT NULL CHECK (json_valid(payload)),
    CHECK (expires_at > observed_at),
    UNIQUE (alliance_id, kind, author_player_id, galaxy, system, position)
) STRICT;

CREATE INDEX ai_alliance_memory_read_idx ON ai_alliance_memory (alliance_id, kind, expires_at);
