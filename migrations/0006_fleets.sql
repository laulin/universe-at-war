CREATE TABLE fleets (
    id INTEGER PRIMARY KEY,
    owner_player_id INTEGER NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    origin_planet_id INTEGER NOT NULL REFERENCES planets(id) ON DELETE CASCADE,
    origin_galaxy INTEGER NOT NULL CHECK (origin_galaxy > 0),
    origin_system INTEGER NOT NULL CHECK (origin_system > 0),
    origin_position INTEGER NOT NULL CHECK (origin_position > 0),
    target_galaxy INTEGER NOT NULL CHECK (target_galaxy > 0),
    target_system INTEGER NOT NULL CHECK (target_system > 0),
    target_position INTEGER NOT NULL CHECK (target_position > 0),
    target_kind TEXT NOT NULL DEFAULT 'planet' CHECK (target_kind IN ('planet', 'moon', 'debris')),
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

CREATE INDEX fleets_owner_state_idx ON fleets (owner_player_id, state, arrives_at, id);
CREATE INDEX fleets_target_idx ON fleets (target_galaxy, target_system, target_position, state);

CREATE TABLE fleet_ships (
    fleet_id INTEGER NOT NULL REFERENCES fleets(id) ON DELETE CASCADE,
    unit_id TEXT NOT NULL,
    quantity INTEGER NOT NULL CHECK (quantity > 0),
    PRIMARY KEY (fleet_id, unit_id)
) STRICT;

CREATE TABLE fleet_cargo (
    fleet_id INTEGER PRIMARY KEY REFERENCES fleets(id) ON DELETE CASCADE,
    metal INTEGER NOT NULL DEFAULT 0 CHECK (metal >= 0),
    crystal INTEGER NOT NULL DEFAULT 0 CHECK (crystal >= 0),
    deuterium INTEGER NOT NULL DEFAULT 0 CHECK (deuterium >= 0)
) STRICT;

CREATE TABLE fleet_transitions (
    id INTEGER PRIMARY KEY,
    fleet_id INTEGER NOT NULL REFERENCES fleets(id) ON DELETE CASCADE,
    from_state TEXT NOT NULL,
    to_state TEXT NOT NULL,
    reason TEXT NOT NULL,
    occurred_at TEXT NOT NULL
) STRICT;

CREATE INDEX fleet_transitions_fleet_idx ON fleet_transitions (fleet_id, id);
