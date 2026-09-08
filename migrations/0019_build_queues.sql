-- A player no longer waits at the screen for one job to end before ordering the
-- next: every mechanism keeps an ordered queue. The head of a queue is the row
-- in state 'active', the only one that carries a schedule and a scheduled
-- event; the rows behind it wait in state 'queued' with no timing at all, and
-- are promoted one by one as the head completes.

CREATE TABLE building_queue_rebuilt (
    id INTEGER PRIMARY KEY,
    planet_id INTEGER NOT NULL REFERENCES planets(id) ON DELETE CASCADE,
    building_id TEXT NOT NULL,
    target_level INTEGER NOT NULL CHECK (target_level > 0),
    metal_cost INTEGER NOT NULL CHECK (metal_cost >= 0),
    crystal_cost INTEGER NOT NULL CHECK (crystal_cost >= 0),
    deuterium_cost INTEGER NOT NULL CHECK (deuterium_cost >= 0),
    ruleset_version INTEGER NOT NULL REFERENCES ruleset_versions(version),
    position INTEGER NOT NULL DEFAULT 0 CHECK (position >= 0),
    queued_at TEXT NOT NULL,
    started_at TEXT,
    completes_at TEXT,
    state TEXT NOT NULL DEFAULT 'queued' CHECK (state IN ('queued', 'active', 'completed', 'cancelled')),
    completed_at TEXT,
    CHECK ((started_at IS NULL) = (completes_at IS NULL)),
    CHECK (completes_at IS NULL OR completes_at > started_at),
    CHECK (state <> 'queued' OR started_at IS NULL),
    CHECK (state NOT IN ('active', 'completed') OR started_at IS NOT NULL),
    CHECK ((state = 'completed') = (completed_at IS NOT NULL))
) STRICT;

INSERT INTO building_queue_rebuilt (id, planet_id, building_id, target_level, metal_cost, crystal_cost,
    deuterium_cost, ruleset_version, position, queued_at, started_at, completes_at, state, completed_at)
SELECT id, planet_id, building_id, target_level, metal_cost, crystal_cost,
    deuterium_cost, ruleset_version, 0, started_at, started_at, completes_at, state, completed_at
FROM building_queue;

DROP TABLE building_queue;

ALTER TABLE building_queue_rebuilt RENAME TO building_queue;

CREATE UNIQUE INDEX building_queue_one_active_idx
    ON building_queue (planet_id) WHERE state = 'active';
CREATE UNIQUE INDEX building_queue_position_idx
    ON building_queue (planet_id, position) WHERE state IN ('active', 'queued');
CREATE INDEX building_queue_completion_idx
    ON building_queue (state, completes_at, id);

CREATE TABLE research_queue_rebuilt (
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
    position INTEGER NOT NULL DEFAULT 0 CHECK (position >= 0),
    queued_at TEXT NOT NULL,
    started_at TEXT,
    completes_at TEXT,
    state TEXT NOT NULL DEFAULT 'queued' CHECK (state IN ('queued', 'active', 'completed', 'cancelled')),
    completed_at TEXT,
    CHECK ((started_at IS NULL) = (completes_at IS NULL)),
    CHECK (completes_at IS NULL OR completes_at > started_at),
    CHECK (state <> 'queued' OR started_at IS NULL),
    CHECK (state NOT IN ('active', 'completed') OR started_at IS NOT NULL),
    CHECK ((state = 'completed') = (completed_at IS NOT NULL))
) STRICT;

INSERT INTO research_queue_rebuilt (id, player_id, planet_id, research_id, target_level, metal_cost,
    crystal_cost, deuterium_cost, energy_cost, effective_laboratory, ruleset_version, position,
    queued_at, started_at, completes_at, state, completed_at)
SELECT id, player_id, planet_id, research_id, target_level, metal_cost,
    crystal_cost, deuterium_cost, energy_cost, effective_laboratory, ruleset_version, 0,
    started_at, started_at, completes_at, state, completed_at
FROM research_queue;

DROP TABLE research_queue;

ALTER TABLE research_queue_rebuilt RENAME TO research_queue;

CREATE UNIQUE INDEX research_queue_one_active_idx
    ON research_queue (player_id) WHERE state = 'active';
CREATE UNIQUE INDEX research_queue_position_idx
    ON research_queue (player_id, position) WHERE state IN ('active', 'queued');
CREATE INDEX research_queue_completion_idx
    ON research_queue (state, completes_at, id);

-- Ships and defences used to share a single slot. They now hold one queue each,
-- so the family joins the key of both the active slot and the ordering.
CREATE TABLE production_orders_rebuilt (
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
    position INTEGER NOT NULL DEFAULT 0 CHECK (position >= 0),
    queued_at TEXT NOT NULL,
    started_at TEXT,
    completes_at TEXT,
    state TEXT NOT NULL DEFAULT 'queued' CHECK (state IN ('queued', 'active', 'completed', 'cancelled')),
    completed_at TEXT,
    CHECK ((started_at IS NULL) = (completes_at IS NULL)),
    CHECK (completes_at IS NULL OR completes_at > started_at),
    CHECK (state <> 'queued' OR started_at IS NULL),
    CHECK (state NOT IN ('active', 'completed') OR started_at IS NOT NULL),
    CHECK ((state = 'completed') = (completed_at IS NOT NULL)),
    CHECK (state <> 'completed' OR delivered = quantity)
) STRICT;

INSERT INTO production_orders_rebuilt (id, planet_id, unit_id, family, quantity, delivered,
    unit_metal_cost, unit_crystal_cost, unit_deuterium_cost, unit_seconds, ruleset_version,
    position, queued_at, started_at, completes_at, state, completed_at)
SELECT id, planet_id, unit_id, family, quantity, delivered,
    unit_metal_cost, unit_crystal_cost, unit_deuterium_cost, unit_seconds, ruleset_version,
    0, started_at, started_at, completes_at, state, completed_at
FROM production_orders;

DROP TABLE production_orders;

ALTER TABLE production_orders_rebuilt RENAME TO production_orders;

CREATE UNIQUE INDEX production_orders_one_active_idx
    ON production_orders (planet_id, family) WHERE state = 'active';
CREATE UNIQUE INDEX production_orders_position_idx
    ON production_orders (planet_id, family, position) WHERE state IN ('active', 'queued');
CREATE INDEX production_orders_completion_idx
    ON production_orders (state, completes_at, id);
