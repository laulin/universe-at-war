-- The roles an alliance of artificial players hands out, and the single plan it
-- pursues at a time.

CREATE TABLE ai_alliance_roles (
    alliance_id INTEGER NOT NULL REFERENCES alliances(id) ON DELETE CASCADE,
    player_id INTEGER NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('scout', 'fleeter', 'recycler', 'defender', 'logistician', 'miner')),
    assigned_at TEXT NOT NULL,
    PRIMARY KEY (alliance_id, player_id)
) STRICT;

CREATE TABLE ai_alliance_objectives (
    id INTEGER PRIMARY KEY,
    alliance_id INTEGER NOT NULL REFERENCES alliances(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('raid', 'defence')),
    galaxy INTEGER NOT NULL CHECK (galaxy > 0),
    system INTEGER NOT NULL CHECK (system > 0),
    position INTEGER NOT NULL CHECK (position > 0),
    state TEXT NOT NULL DEFAULT 'scouting' CHECK (state IN ('scouting', 'assembling', 'resolved', 'abandoned')),
    group_id INTEGER REFERENCES acs_groups(id) ON DELETE SET NULL,
    quorum INTEGER NOT NULL CHECK (quorum > 0),
    opened_at TEXT NOT NULL,
    deadline_at TEXT NOT NULL,
    closed_at TEXT,
    reason TEXT NOT NULL DEFAULT '',
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    CHECK (deadline_at > opened_at),
    CHECK ((state IN ('resolved', 'abandoned')) = (closed_at IS NOT NULL))
) STRICT;

-- An alliance pursues one plan at a time: the others are history.
CREATE UNIQUE INDEX ai_alliance_objectives_open_idx ON ai_alliance_objectives (alliance_id)
    WHERE state IN ('scouting', 'assembling');

CREATE INDEX ai_alliance_objectives_history_idx ON ai_alliance_objectives (alliance_id, id DESC);
