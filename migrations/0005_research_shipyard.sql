CREATE TABLE player_research (
    player_id INTEGER NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    research_id TEXT NOT NULL,
    level INTEGER NOT NULL DEFAULT 0 CHECK (level >= 0),
    PRIMARY KEY (player_id, research_id)
) STRICT;

CREATE TABLE research_queue (
    id INTEGER PRIMARY KEY,
    player_id INTEGER NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    planet_id INTEGER NOT NULL REFERENCES planets(id) ON DELETE CASCADE,
    research_id TEXT NOT NULL,
    target_level INTEGER NOT NULL CHECK (target_level > 0),
    metal_cost INTEGER NOT NULL CHECK (metal_cost >= 0),
    crystal_cost INTEGER NOT NULL CHECK (crystal_cost >= 0),
    deuterium_cost INTEGER NOT NULL CHECK (deuterium_cost >= 0),
    energy_cost INTEGER NOT NULL DEFAULT 0 CHECK (energy_cost >= 0),
    effective_laboratory INTEGER NOT NULL DEFAULT 0 CHECK (effective_laboratory >= 0),
    ruleset_version INTEGER NOT NULL REFERENCES ruleset_versions(version),
    started_at TEXT NOT NULL,
    completes_at TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'completed', 'cancelled')),
    completed_at TEXT,
    CHECK (completes_at > started_at),
    CHECK ((state = 'completed') = (completed_at IS NOT NULL))
) STRICT;

CREATE UNIQUE INDEX research_queue_one_active_idx
    ON research_queue (player_id) WHERE state = 'active';
CREATE INDEX research_queue_completion_idx
    ON research_queue (state, completes_at, id);

CREATE TABLE planet_units (
    planet_id INTEGER NOT NULL REFERENCES planets(id) ON DELETE CASCADE,
    unit_id TEXT NOT NULL,
    quantity INTEGER NOT NULL DEFAULT 0 CHECK (quantity >= 0),
    PRIMARY KEY (planet_id, unit_id)
) STRICT;

CREATE TABLE production_orders (
    id INTEGER PRIMARY KEY,
    planet_id INTEGER NOT NULL REFERENCES planets(id) ON DELETE CASCADE,
    unit_id TEXT NOT NULL,
    family TEXT NOT NULL CHECK (family IN ('ship', 'defense')),
    quantity INTEGER NOT NULL CHECK (quantity > 0),
    delivered INTEGER NOT NULL DEFAULT 0 CHECK (delivered >= 0 AND delivered <= quantity),
    unit_metal_cost INTEGER NOT NULL CHECK (unit_metal_cost >= 0),
    unit_crystal_cost INTEGER NOT NULL CHECK (unit_crystal_cost >= 0),
    unit_deuterium_cost INTEGER NOT NULL CHECK (unit_deuterium_cost >= 0),
    unit_seconds INTEGER NOT NULL CHECK (unit_seconds > 0),
    ruleset_version INTEGER NOT NULL REFERENCES ruleset_versions(version),
    started_at TEXT NOT NULL,
    completes_at TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'completed', 'cancelled')),
    completed_at TEXT,
    CHECK (completes_at > started_at),
    CHECK ((state = 'completed') = (completed_at IS NOT NULL)),
    CHECK (state <> 'completed' OR delivered = quantity)
) STRICT;

CREATE UNIQUE INDEX production_orders_one_active_idx
    ON production_orders (planet_id) WHERE state = 'active';
CREATE INDEX production_orders_completion_idx
    ON production_orders (state, completes_at, id);
