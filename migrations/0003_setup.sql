CREATE TABLE setup_drafts (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    document TEXT NOT NULL CHECK (json_valid(document)),
    current_step INTEGER NOT NULL DEFAULT 1 CHECK (current_step BETWEEN 1 AND 10),
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'completed')),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by_account_id INTEGER NOT NULL REFERENCES accounts(id),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
) STRICT;

