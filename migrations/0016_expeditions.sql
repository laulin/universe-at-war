-- migration: rebuild referenced table
--
-- An expedition flies past the last planet of a system, to a position that
-- belongs to nobody, and comes back with a report of its own kind.

CREATE TABLE fleets_rebuilt (
    id INTEGER PRIMARY KEY,
    owner_player_id INTEGER NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    origin_planet_id INTEGER NOT NULL REFERENCES planets(id) ON DELETE CASCADE,
    origin_galaxy INTEGER NOT NULL CHECK (origin_galaxy > 0),
    origin_system INTEGER NOT NULL CHECK (origin_system > 0),
    origin_position INTEGER NOT NULL CHECK (origin_position > 0),
    target_galaxy INTEGER NOT NULL CHECK (target_galaxy > 0),
    target_system INTEGER NOT NULL CHECK (target_system > 0),
    target_position INTEGER NOT NULL CHECK (target_position > 0),
    target_kind TEXT NOT NULL DEFAULT 'planet' CHECK (target_kind IN ('planet', 'moon', 'debris', 'empty', 'space')),
    target_planet_id INTEGER REFERENCES planets(id),
    mission TEXT NOT NULL,
    speed_percent INTEGER NOT NULL CHECK (speed_percent BETWEEN 10 AND 100),
    fleet_speed INTEGER NOT NULL CHECK (fleet_speed > 0),
    distance INTEGER NOT NULL CHECK (distance > 0),
    fuel INTEGER NOT NULL CHECK (fuel >= 0),
    seed INTEGER NOT NULL,
    ruleset_version INTEGER NOT NULL REFERENCES ruleset_versions(version),
    departed_at TEXT NOT NULL,
    arrives_at TEXT NOT NULL,
    holds_until TEXT,
    returns_at TEXT,
    recalled_at TEXT,
    state TEXT NOT NULL DEFAULT 'outbound' CHECK (state IN ('outbound', 'holding', 'returning', 'recalled', 'completed', 'destroyed')),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at TEXT NOT NULL,
    CHECK (arrives_at > departed_at),
    CHECK (holds_until IS NULL OR holds_until > arrives_at)
) STRICT;

INSERT INTO fleets_rebuilt SELECT * FROM fleets;

DROP TABLE fleets;

ALTER TABLE fleets_rebuilt RENAME TO fleets;

CREATE INDEX fleets_owner_state_idx ON fleets (owner_player_id, state, arrives_at, id);
CREATE INDEX fleets_target_idx ON fleets (target_galaxy, target_system, target_position, state);
CREATE INDEX fleets_defenders_idx ON fleets (target_galaxy, target_system, target_position, state, mission);

CREATE TABLE reports_rebuilt (
    id INTEGER PRIMARY KEY,
    recipient_player_id INTEGER NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('espionage', 'espionage_detected', 'combat_attack', 'combat_defense', 'recycling', 'expedition')),
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
