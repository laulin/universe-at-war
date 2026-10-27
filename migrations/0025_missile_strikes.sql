-- migration: rebuild referenced table
-- Missile salves are independent of fleets and survive the removal of a body.
CREATE TABLE missile_strikes (
    id INTEGER PRIMARY KEY,
    owner_player_id INTEGER NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    origin_body_id INTEGER NOT NULL,
    origin_galaxy INTEGER NOT NULL CHECK (origin_galaxy > 0),
    origin_system INTEGER NOT NULL CHECK (origin_system > 0),
    origin_position INTEGER NOT NULL CHECK (origin_position > 0),
    target_body_id INTEGER NOT NULL,
    target_created_at TEXT NOT NULL,
    target_galaxy INTEGER NOT NULL CHECK (target_galaxy > 0),
    target_system INTEGER NOT NULL CHECK (target_system > 0),
    target_position INTEGER NOT NULL CHECK (target_position > 0),
    target_kind TEXT NOT NULL CHECK (target_kind IN ('planet', 'moon')),
    quantity INTEGER NOT NULL CHECK (quantity > 0),
    primary_target TEXT NOT NULL,
    seed INTEGER NOT NULL,
    ruleset_version INTEGER NOT NULL REFERENCES ruleset_versions(version),
    departed_at TEXT NOT NULL,
    arrives_at TEXT NOT NULL,
    resolved_at TEXT,
    state TEXT NOT NULL DEFAULT 'outbound' CHECK (state IN ('outbound', 'completed', 'aborted')),
    CHECK (arrives_at > departed_at),
    CHECK ((state = 'outbound') = (resolved_at IS NULL))
) STRICT;
CREATE INDEX missile_strikes_origin_idx ON missile_strikes(origin_body_id, state, arrives_at);
CREATE INDEX missile_strikes_target_idx ON missile_strikes(target_body_id, state, arrives_at);

CREATE TABLE reports_rebuilt (
    id INTEGER PRIMARY KEY,
    recipient_player_id INTEGER NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('espionage', 'espionage_detected', 'combat_attack', 'combat_defense', 'recycling', 'expedition', 'missile_attack', 'missile_defense')),
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

INSERT INTO reports_rebuilt SELECT * FROM reports;

DROP TABLE reports;

ALTER TABLE reports_rebuilt RENAME TO reports;

CREATE INDEX reports_recipient_idx ON reports (recipient_player_id, occurred_at DESC, id DESC);
CREATE INDEX reports_unread_idx ON reports (recipient_player_id, kind) WHERE read_at IS NULL;
