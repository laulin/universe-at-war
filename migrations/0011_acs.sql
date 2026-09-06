CREATE TABLE acs_groups (
    id INTEGER PRIMARY KEY,
    owner_player_id INTEGER NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    alliance_id INTEGER NOT NULL REFERENCES alliances(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('attack', 'defense')),
    target_galaxy INTEGER NOT NULL CHECK (target_galaxy > 0),
    target_system INTEGER NOT NULL CHECK (target_system > 0),
    target_position INTEGER NOT NULL CHECK (target_position > 0),
    seed INTEGER NOT NULL,
    ruleset_version INTEGER NOT NULL REFERENCES ruleset_versions(version),
    arrives_at TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'forming' CHECK (state IN ('forming', 'locked', 'resolved', 'cancelled')),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at TEXT NOT NULL,
    resolved_at TEXT
) STRICT;

CREATE INDEX acs_groups_alliance_idx ON acs_groups (alliance_id, state, arrives_at);

-- One fleet belongs to one operation at most, which the primary key guarantees.
CREATE TABLE acs_participants (
    fleet_id INTEGER PRIMARY KEY REFERENCES fleets(id) ON DELETE CASCADE,
    group_id INTEGER NOT NULL REFERENCES acs_groups(id) ON DELETE CASCADE,
    player_id INTEGER NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    joined_at TEXT NOT NULL
) STRICT;

CREATE INDEX acs_participants_group_idx ON acs_participants (group_id, fleet_id);
