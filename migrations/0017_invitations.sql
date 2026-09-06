-- An invitation is a one-shot ticket into a closed universe. Only its digest is
-- kept: the code itself is shown once, to the person who created it.

CREATE TABLE invitations (
    id INTEGER PRIMARY KEY,
    code_digest TEXT NOT NULL UNIQUE,
    label TEXT NOT NULL DEFAULT '',
    created_by_account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    used_at TEXT,
    used_by_account_id INTEGER REFERENCES accounts(id) ON DELETE SET NULL,
    revoked_at TEXT,
    CHECK (expires_at > created_at),
    CHECK ((used_at IS NULL) = (used_by_account_id IS NULL))
) STRICT;

CREATE INDEX invitations_open_idx ON invitations (expires_at)
    WHERE used_at IS NULL AND revoked_at IS NULL;
