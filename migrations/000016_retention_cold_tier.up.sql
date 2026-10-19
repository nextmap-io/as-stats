-- ============================================================================
-- Hot / cold storage tier for the retention subsystem
-- ============================================================================
--
-- cold_after_days: age (in days) after which the reconciler moves a table's
-- data to the `cold` volume of its storage policy, via
--   TTL <expr> + INTERVAL cold_after_days DAY TO VOLUME 'cold', <expr> + INTERVAL ttl_days DAY
-- plus ALTER TABLE ... MOVE PARTITION ... TO VOLUME 'cold' for partitions that
-- were written before the move clause existed (materialize_ttl_after_modify = 0
-- leaves them without move TTL info).
--
-- 0 = no cold tier (the default, and the only effective value for tables whose
-- storage policy has no volume named `cold`). When set, the API enforces
-- 1 <= cold_after_days < ttl_days.
--
-- ADD COLUMN with a DEFAULT is metadata-only on MergeTree: existing parts are
-- not rewritten, the default is computed on read.

ALTER TABLE asstats.retention_policies
    ADD COLUMN IF NOT EXISTS cold_after_days UInt32 DEFAULT 0;
