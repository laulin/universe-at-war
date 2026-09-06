-- migration: rebuild referenced table
--
-- A colonisation aims at a position that holds nothing yet, which the target
-- kind of a fleet must be able to say.

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
    target_kind TEXT NOT NULL DEFAULT 'planet' CHECK (target_kind IN ('planet', 'moon', 'debris', 'empty')),
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
    returns_at TEXT,
    recalled_at TEXT,
    state TEXT NOT NULL DEFAULT 'outbound' CHECK (state IN ('outbound', 'returning', 'recalled', 'completed', 'destroyed')),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at TEXT NOT NULL,
    CHECK (arrives_at > departed_at)
) STRICT;

INSERT INTO fleets_rebuilt SELECT * FROM fleets;

DROP TABLE fleets;

ALTER TABLE fleets_rebuilt RENAME TO fleets;

CREATE INDEX fleets_owner_state_idx ON fleets (owner_player_id, state, arrives_at, id);
CREATE INDEX fleets_target_idx ON fleets (target_galaxy, target_system, target_position, state);
