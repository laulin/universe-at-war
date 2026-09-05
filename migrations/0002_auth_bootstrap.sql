CREATE TABLE accounts (
    id INTEGER PRIMARY KEY,
    username TEXT NOT NULL,
    username_normalized TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
) STRICT;

CREATE TABLE account_roles (
    account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('ADMIN', 'MODERATOR', 'PLAYER')),
    granted_at TEXT NOT NULL,
    granted_by_account_id INTEGER REFERENCES accounts(id),
    PRIMARY KEY (account_id, role)
) STRICT;

CREATE TABLE password_credentials (
    account_id INTEGER PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    encoded_hash TEXT NOT NULL,
    must_change_password INTEGER NOT NULL DEFAULT 0 CHECK (must_change_password IN (0, 1)),
    updated_at TEXT NOT NULL
) STRICT;

CREATE TABLE sessions (
    id INTEGER PRIMARY KEY,
    account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    token_digest BLOB NOT NULL UNIQUE,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    revoked_at TEXT,
    CHECK (expires_at > created_at)
) STRICT;

CREATE INDEX sessions_account_active_idx
    ON sessions (account_id, expires_at) WHERE revoked_at IS NULL;

CREATE TABLE bans (
    id INTEGER PRIMARY KEY,
    account_id INTEGER NOT NULL REFERENCES accounts(id),
    author_account_id INTEGER NOT NULL REFERENCES accounts(id),
    starts_at TEXT NOT NULL,
    ends_at TEXT,
    justification TEXT NOT NULL CHECK (length(trim(justification)) > 0),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'lifted', 'expired')),
    lifted_at TEXT,
    lifted_by_account_id INTEGER REFERENCES accounts(id),
    created_at TEXT NOT NULL,
    CHECK (ends_at IS NULL OR ends_at > starts_at),
    CHECK ((status = 'lifted') = (lifted_at IS NOT NULL))
) STRICT;

CREATE INDEX bans_account_status_idx ON bans (account_id, status, starts_at);

CREATE TABLE audit_log (
    id INTEGER PRIMARY KEY,
    actor_account_id INTEGER REFERENCES accounts(id),
    action TEXT NOT NULL,
    target_type TEXT,
    target_id TEXT,
    occurred_at TEXT NOT NULL,
    request_id TEXT,
    details TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(details))
) STRICT;

CREATE INDEX audit_log_occurred_idx ON audit_log (occurred_at, id);
CREATE INDEX audit_log_actor_idx ON audit_log (actor_account_id, occurred_at, id);

CREATE TABLE ruleset_versions (
    id INTEGER PRIMARY KEY,
    version INTEGER NOT NULL UNIQUE CHECK (version > 0),
    status TEXT NOT NULL CHECK (status IN ('draft', 'active', 'superseded')),
    document TEXT NOT NULL CHECK (json_valid(document)),
    checksum TEXT NOT NULL,
    author_account_id INTEGER NOT NULL REFERENCES accounts(id),
    justification TEXT,
    effective_at TEXT,
    created_at TEXT NOT NULL
) STRICT;

