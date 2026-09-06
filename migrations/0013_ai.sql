-- An artificial player is an ordinary account without any credential: it owns
-- an empire, it pays for everything, and no session can ever be opened on it.

ALTER TABLE accounts ADD COLUMN kind TEXT NOT NULL DEFAULT 'human' CHECK (kind IN ('human', 'ai'));

CREATE TABLE ai_profiles (
    player_id INTEGER PRIMARY KEY REFERENCES players(id) ON DELETE CASCADE,
    account_id INTEGER NOT NULL UNIQUE REFERENCES accounts(id) ON DELETE CASCADE,
    archetype TEXT NOT NULL,
    activity_start_hour INTEGER NOT NULL CHECK (activity_start_hour BETWEEN 0 AND 23),
    activity_end_hour INTEGER NOT NULL CHECK (activity_end_hour BETWEEN 0 AND 23),
    think_interval_seconds INTEGER NOT NULL CHECK (think_interval_seconds > 0),
    seed INTEGER NOT NULL,
    tick INTEGER NOT NULL DEFAULT 0 CHECK (tick >= 0),
    state TEXT NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'retired')),
    next_think_at TEXT,
    due_think_at TEXT,
    last_think_at TEXT,
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at TEXT NOT NULL,
    retired_at TEXT,
    CHECK ((state = 'retired') = (retired_at IS NOT NULL))
) STRICT;

CREATE INDEX ai_profiles_due_idx ON ai_profiles (state, next_think_at, player_id);

-- The memory of an artificial player holds only what it could legitimately
-- observe: its own reports and the public map.
CREATE TABLE ai_memory (
    id INTEGER PRIMARY KEY,
    player_id INTEGER NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('target', 'threat', 'debris')),
    galaxy INTEGER NOT NULL CHECK (galaxy > 0),
    system INTEGER NOT NULL CHECK (system > 0),
    position INTEGER NOT NULL CHECK (position > 0),
    observed_at TEXT NOT NULL,
    score REAL NOT NULL DEFAULT 0,
    payload_version INTEGER NOT NULL CHECK (payload_version > 0),
    payload TEXT NOT NULL CHECK (json_valid(payload)),
    UNIQUE (player_id, kind, galaxy, system, position)
) STRICT;

CREATE INDEX ai_memory_player_idx ON ai_memory (player_id, kind, observed_at DESC);

-- Every reflection leaves a trace, so a human can tell what the machine tried
-- and why it did not always get it.
CREATE TABLE ai_decisions (
    id INTEGER PRIMARY KEY,
    player_id INTEGER NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    decided_at TEXT NOT NULL,
    layer TEXT NOT NULL CHECK (layer IN ('strategic', 'operational', 'tactical')),
    action TEXT NOT NULL,
    outcome TEXT NOT NULL CHECK (outcome IN ('done', 'skipped', 'failed')),
    reason TEXT NOT NULL,
    score REAL NOT NULL DEFAULT 0,
    body_id INTEGER,
    target TEXT
) STRICT;

CREATE INDEX ai_decisions_player_idx ON ai_decisions (player_id, id DESC);
