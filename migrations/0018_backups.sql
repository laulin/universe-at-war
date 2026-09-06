-- A record of every snapshot taken, so an administrator can tell at a glance
-- when the universe was last put somewhere safe.

CREATE TABLE backups (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    path TEXT NOT NULL,
    taken_at TEXT NOT NULL,
    bytes INTEGER NOT NULL CHECK (bytes > 0),
    schema_version INTEGER NOT NULL CHECK (schema_version > 0),
    verified INTEGER NOT NULL DEFAULT 0 CHECK (verified IN (0, 1)),
    author_account_id INTEGER REFERENCES accounts(id) ON DELETE SET NULL
) STRICT;

CREATE INDEX backups_recent_idx ON backups (taken_at DESC, id DESC);
