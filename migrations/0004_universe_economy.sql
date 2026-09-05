CREATE TABLE players (
    id INTEGER PRIMARY KEY,
    account_id INTEGER NOT NULL UNIQUE REFERENCES accounts(id) ON DELETE CASCADE,
    display_name TEXT NOT NULL UNIQUE CHECK (length(trim(display_name)) BETWEEN 3 AND 32),
    created_at TEXT NOT NULL
) STRICT;

CREATE TABLE planets (
    id INTEGER PRIMARY KEY,
    owner_player_id INTEGER NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (length(trim(name)) BETWEEN 1 AND 32),
    galaxy INTEGER NOT NULL CHECK (galaxy > 0),
    system INTEGER NOT NULL CHECK (system > 0),
    position INTEGER NOT NULL CHECK (position > 0),
    total_fields INTEGER NOT NULL CHECK (total_fields > 0),
    used_fields INTEGER NOT NULL DEFAULT 0 CHECK (used_fields >= 0 AND used_fields <= total_fields),
    minimum_temperature INTEGER NOT NULL,
    maximum_temperature INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE (galaxy, system, position),
    CHECK (minimum_temperature <= maximum_temperature)
) STRICT;

CREATE INDEX planets_owner_idx ON planets (owner_player_id, id);

CREATE TABLE planet_resources (
    planet_id INTEGER PRIMARY KEY REFERENCES planets(id) ON DELETE CASCADE,
    metal INTEGER NOT NULL DEFAULT 500 CHECK (metal >= 0),
    crystal INTEGER NOT NULL DEFAULT 500 CHECK (crystal >= 0),
    deuterium INTEGER NOT NULL DEFAULT 0 CHECK (deuterium >= 0),
    metal_remainder INTEGER NOT NULL DEFAULT 0 CHECK (metal_remainder BETWEEN 0 AND 3599),
    crystal_remainder INTEGER NOT NULL DEFAULT 0 CHECK (crystal_remainder BETWEEN 0 AND 3599),
    deuterium_remainder INTEGER NOT NULL DEFAULT 0 CHECK (deuterium_remainder BETWEEN 0 AND 3599),
    produced_at TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0)
) STRICT;

CREATE TABLE planet_buildings (
    planet_id INTEGER NOT NULL REFERENCES planets(id) ON DELETE CASCADE,
    building_id TEXT NOT NULL,
    level INTEGER NOT NULL DEFAULT 0 CHECK (level >= 0),
    PRIMARY KEY (planet_id, building_id)
) STRICT;

CREATE TABLE building_queue (
    id INTEGER PRIMARY KEY,
    planet_id INTEGER NOT NULL REFERENCES planets(id) ON DELETE CASCADE,
    building_id TEXT NOT NULL,
    target_level INTEGER NOT NULL CHECK (target_level > 0),
    metal_cost INTEGER NOT NULL CHECK (metal_cost >= 0),
    crystal_cost INTEGER NOT NULL CHECK (crystal_cost >= 0),
    deuterium_cost INTEGER NOT NULL CHECK (deuterium_cost >= 0),
    ruleset_version INTEGER NOT NULL REFERENCES ruleset_versions(version),
    started_at TEXT NOT NULL,
    completes_at TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'completed', 'cancelled')),
    completed_at TEXT,
    CHECK (completes_at > started_at),
    CHECK ((state = 'completed') = (completed_at IS NOT NULL))
) STRICT;

CREATE UNIQUE INDEX building_queue_one_active_idx
    ON building_queue (planet_id) WHERE state = 'active';
CREATE INDEX building_queue_completion_idx
    ON building_queue (state, completes_at, id);
