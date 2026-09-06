CREATE TABLE alliances (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL CHECK (length(trim(name)) BETWEEN 3 AND 32),
    name_normalized TEXT NOT NULL UNIQUE,
    tag TEXT NOT NULL UNIQUE CHECK (length(tag) BETWEEN 2 AND 8),
    description TEXT NOT NULL DEFAULT '',
    founder_player_id INTEGER NOT NULL REFERENCES players(id),
    created_at TEXT NOT NULL
) STRICT;

-- A player belongs to at most one alliance, which the primary key guarantees.
CREATE TABLE alliance_members (
    player_id INTEGER PRIMARY KEY REFERENCES players(id) ON DELETE CASCADE,
    alliance_id INTEGER NOT NULL REFERENCES alliances(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('founder', 'officer', 'member')),
    joined_at TEXT NOT NULL
) STRICT;

CREATE INDEX alliance_members_alliance_idx ON alliance_members (alliance_id, role, player_id);

CREATE TABLE alliance_invitations (
    id INTEGER PRIMARY KEY,
    alliance_id INTEGER NOT NULL REFERENCES alliances(id) ON DELETE CASCADE,
    player_id INTEGER NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    invited_by_player_id INTEGER NOT NULL REFERENCES players(id),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'accepted', 'declined', 'revoked')),
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    resolved_at TEXT,
    CHECK ((status = 'pending') = (resolved_at IS NULL))
) STRICT;

CREATE UNIQUE INDEX alliance_invitations_pending_idx
    ON alliance_invitations (alliance_id, player_id) WHERE status = 'pending';
CREATE INDEX alliance_invitations_player_idx ON alliance_invitations (player_id, status, expires_at);

CREATE TABLE alliance_relations (
    alliance_id INTEGER NOT NULL REFERENCES alliances(id) ON DELETE CASCADE,
    other_alliance_id INTEGER NOT NULL REFERENCES alliances(id) ON DELETE CASCADE,
    relation TEXT NOT NULL CHECK (relation IN ('pact', 'war')),
    declared_by_player_id INTEGER NOT NULL REFERENCES players(id),
    declared_at TEXT NOT NULL,
    PRIMARY KEY (alliance_id, other_alliance_id),
    CHECK (alliance_id <> other_alliance_id)
) STRICT;

CREATE TABLE alliance_history (
    id INTEGER PRIMARY KEY,
    alliance_id INTEGER NOT NULL REFERENCES alliances(id) ON DELETE CASCADE,
    actor_player_id INTEGER REFERENCES players(id),
    action TEXT NOT NULL,
    target_player_id INTEGER REFERENCES players(id),
    occurred_at TEXT NOT NULL,
    details TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(details))
) STRICT;

CREATE INDEX alliance_history_idx ON alliance_history (alliance_id, occurred_at DESC, id DESC);
