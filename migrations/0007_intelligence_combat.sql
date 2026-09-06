CREATE TABLE reports (
    id INTEGER PRIMARY KEY,
    recipient_player_id INTEGER NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('espionage', 'espionage_detected', 'combat_attack', 'combat_defense', 'recycling')),
    subject_type TEXT NOT NULL CHECK (subject_type IN ('planet', 'debris', 'fleet')),
    subject_id INTEGER NOT NULL,
    galaxy INTEGER NOT NULL CHECK (galaxy > 0),
    system INTEGER NOT NULL CHECK (system > 0),
    position INTEGER NOT NULL CHECK (position > 0),
    occurred_at TEXT NOT NULL,
    read_at TEXT,
    payload_version INTEGER NOT NULL CHECK (payload_version > 0),
    payload TEXT NOT NULL CHECK (json_valid(payload)),
    shared_alliance_id INTEGER,
    created_at TEXT NOT NULL
) STRICT;

CREATE INDEX reports_recipient_idx ON reports (recipient_player_id, occurred_at DESC, id DESC);
CREATE INDEX reports_unread_idx ON reports (recipient_player_id, kind) WHERE read_at IS NULL;

CREATE TABLE debris_fields (
    galaxy INTEGER NOT NULL CHECK (galaxy > 0),
    system INTEGER NOT NULL CHECK (system > 0),
    position INTEGER NOT NULL CHECK (position > 0),
    metal INTEGER NOT NULL DEFAULT 0 CHECK (metal >= 0),
    crystal INTEGER NOT NULL DEFAULT 0 CHECK (crystal >= 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    PRIMARY KEY (galaxy, system, position),
    CHECK (metal + crystal > 0)
) STRICT;
