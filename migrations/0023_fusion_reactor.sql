ALTER TABLE planet_resources ADD COLUMN deuterium_net_remainder INTEGER NOT NULL DEFAULT 0
    CHECK (deuterium_net_remainder BETWEEN -3599 AND 3599);

-- Existing universes already carry a positive fractional remainder. Preserve
-- it when moving to the signed balance used by fusion-reactor fuel consumption.
UPDATE planet_resources
SET deuterium_net_remainder = deuterium_remainder;
