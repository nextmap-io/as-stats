-- ============================================================================
-- Retention policies: DB-backed, runtime-controllable TTLs per table
-- ============================================================================
-- Each row records the desired retention (in days) for one TTL-bearing table.
-- The collector seeds this table on first startup (idempotent — only if empty)
-- with the TTLs encoded in the migrations, then a reconciler goroutine applies
-- any divergence via ALTER TABLE ... MODIFY TTL on a fixed interval.
--
-- ReplacingMergeTree(updated_at) so the latest edit always wins; ORDER BY
-- table_name keeps one logical row per table. Always read with FINAL.

CREATE TABLE IF NOT EXISTS asstats.retention_policies (
    table_name   String,
    ttl_column   String,                  -- TTL timestamp column for this table
    ttl_days     UInt32,                  -- desired retention in days
    enabled      UInt8 DEFAULT 1,         -- 0 = reconciler skips this table
    updated_at   DateTime DEFAULT now()
) ENGINE = ReplacingMergeTree(updated_at)
ORDER BY table_name;
