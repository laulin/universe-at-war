CREATE TABLE app_metadata (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    created_by_version TEXT NOT NULL,
    last_opened_by_version TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
) STRICT;

CREATE TABLE server_state (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    state TEXT NOT NULL CHECK (state IN (
        'BOOTSTRAP_PENDING',
        'SETUP_IN_PROGRESS',
        'RUNNING',
        'PAUSED',
        'MAINTENANCE'
    )),
    previous_state TEXT CHECK (previous_state IS NULL OR previous_state IN ('RUNNING', 'PAUSED')),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_at TEXT NOT NULL
) STRICT;

CREATE TABLE scheduled_events (
    id INTEGER PRIMARY KEY,
    event_type TEXT NOT NULL,
    due_at TEXT NOT NULL,
    priority INTEGER NOT NULL DEFAULT 100,
    entity_type TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    ruleset_version INTEGER,
    payload_version INTEGER NOT NULL DEFAULT 1 CHECK (payload_version > 0),
    payload TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(payload)),
    idempotency_key TEXT NOT NULL UNIQUE,
    state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'processing', 'completed', 'cancelled')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error TEXT,
    created_at TEXT NOT NULL,
    processed_at TEXT
) STRICT;

CREATE INDEX scheduled_events_due_idx
    ON scheduled_events (state, due_at, priority, id);

CREATE TABLE game_event_log (
    id INTEGER PRIMARY KEY,
    event_type TEXT NOT NULL,
    actor_type TEXT,
    actor_id TEXT,
    entity_type TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    occurred_at TEXT NOT NULL,
    payload TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(payload))
) STRICT;

CREATE INDEX game_event_log_entity_idx
    ON game_event_log (entity_type, entity_id, occurred_at, id);

CREATE TABLE idempotency_keys (
    id INTEGER PRIMARY KEY,
    actor_id TEXT NOT NULL,
    operation TEXT NOT NULL,
    key TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    result_type TEXT,
    result_id TEXT,
    created_at TEXT NOT NULL,
    expires_at TEXT,
    UNIQUE (actor_id, operation, key)
) STRICT;

