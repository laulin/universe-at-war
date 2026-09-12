-- Individual AI personalities override archetype defaults without changing
-- their empire, diary or memory. An empty object keeps following the archetype.

ALTER TABLE ai_profiles ADD COLUMN tuning TEXT NOT NULL DEFAULT '{}'
    CHECK (json_valid(tuning));

ALTER TABLE ai_profiles ADD COLUMN configuration_version INTEGER NOT NULL DEFAULT 1
    CHECK (configuration_version > 0);
