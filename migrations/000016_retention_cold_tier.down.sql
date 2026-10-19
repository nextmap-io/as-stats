-- Drop the cold tier setting. Tables keep whatever TTL ... TO VOLUME clause the
-- reconciler last applied until their policy is next reconciled.

ALTER TABLE asstats.retention_policies
    DROP COLUMN IF EXISTS cold_after_days;
