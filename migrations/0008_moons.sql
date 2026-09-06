-- migration: rebuild referenced table
--
-- A moon shares the position of its planet, so the uniqueness of a coordinate
-- must include the kind of body. The constraint belongs to the table itself and
-- SQLite cannot drop it in place, hence the rewrite.

CREATE TABLE planets_rebuilt (
    id INTEGER PRIMARY KEY,
    owner_player_id INTEGER NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    kind TEXT NOT NULL DEFAULT 'planet' CHECK (kind IN ('planet', 'moon')),
    parent_planet_id INTEGER REFERENCES planets(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (length(trim(name)) BETWEEN 1 AND 32),
    galaxy INTEGER NOT NULL CHECK (galaxy > 0),
    system INTEGER NOT NULL CHECK (system > 0),
    position INTEGER NOT NULL CHECK (position > 0),
    total_fields INTEGER NOT NULL CHECK (total_fields > 0),
    used_fields INTEGER NOT NULL DEFAULT 0 CHECK (used_fields >= 0 AND used_fields <= total_fields),
    minimum_temperature INTEGER NOT NULL,
    maximum_temperature INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE (galaxy, system, position, kind),
    CHECK (minimum_temperature <= maximum_temperature),
    CHECK ((kind = 'moon') = (parent_planet_id IS NOT NULL))
) STRICT;

INSERT INTO planets_rebuilt(
    id, owner_player_id, kind, parent_planet_id, name, galaxy, system, position,
    total_fields, used_fields, minimum_temperature, maximum_temperature, created_at
)
SELECT id, owner_player_id, 'planet', NULL, name, galaxy, system, position,
       total_fields, used_fields, minimum_temperature, maximum_temperature, created_at
FROM planets;

DROP TABLE planets;

ALTER TABLE planets_rebuilt RENAME TO planets;

CREATE INDEX planets_owner_idx ON planets (owner_player_id, id);
CREATE INDEX planets_parent_idx ON planets (parent_planet_id);

CREATE TABLE jump_gates (
    moon_id INTEGER PRIMARY KEY REFERENCES planets(id) ON DELETE CASCADE,
    ready_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
) STRICT;
