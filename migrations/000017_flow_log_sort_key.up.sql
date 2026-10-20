-- ============================================================================
-- flows_log: sort by (15-minute bucket, dst_ip) so IP lookups use the key
-- ============================================================================
--
-- Why. The 000007 layout sorts by (ts, link_tag, protocol, dst_port, src_ip,
-- dst_ip, …): an IP filter can only use the bloom_filter skip index, and a busy
-- host appears in nearly every 32k-row block, so the index skips ~7 % of the
-- granules. Measured on production (one day = 99 M rows, 620 k distinct dst
-- IPs, cold caches, SELECT * … ORDER BY bytes DESC LIMIT 100):
--
--                       000007 layout          this layout
--   busy dst IP, 24 h   2.3 s / 5.2 GiB        0.32 s / 73 MiB
--   busy dst IP,  1 h   —                      0.11 s / 3.5 MiB
--   busy src IP, 24 h   2.6 s / 6.2 GiB        1.1 s / 1.5 GiB
--   15-min window       0.16 s                 0.16 s
--   size                1.01 GiB/day           892 MiB/day
--
-- The key keeps exactly the same set of columns as before (router_ip stays
-- out of it, as before), so SummingMergeTree collapses exactly the same rows.
-- dst_ip is in the key, so it has no bloom filter any more; src_ip keeps one,
-- now at GRANULARITY 1.
--
-- READERS MUST ALSO FILTER ON THE 15-MINUTE BUCKET. ClickHouse 24.3 only
-- partially derives a toStartOfFifteenMinutes(ts) key range from
-- `ts >= a AND ts < b` (a 15-minute window read 80 % of the day). Every query
-- adds `toStartOfFifteenMinutes(ts) BETWEEN toStartOfFifteenMinutes(a) AND
-- toStartOfFifteenMinutes(b_inclusive)` — store.flowLogTimeFilter.
--
-- No data is copied. The existing table (tens of GiB, possibly partly on a
-- slow cold volume) is kept as flows_log_legacy and ages out under the
-- flows_log retention policy (the reconciler applies flows_log's ttl_days and
-- cold_after_days to it). Readers query flows_log_all, a Merge table over
-- flows_log and flows_log_legacy; writes still go to flows_log only.
--
-- How the materialised view is re-pointed — verified on ClickHouse 24.3
-- (Atomic database), with a loop INSERTing into flows_raw throughout:
--
--   * A `TO` materialised view resolves its target BY NAME at insert time; it
--     does not follow the target's UUID.
--   * So a plain `RENAME TABLE flows_log TO flows_log_legacy` followed by a
--     CREATE leaves a window in which every INSERT INTO flows_raw FAILS
--     ("Target table 'asstats.flows_log' of view 'asstats.flows_log_mv'
--     doesn't exists") — and the collector never retries a failed batch.
--   * EXCHANGE TABLES swaps the two names atomically: every insert block lands
--     either in the old table (before) or the new one (after), never both and
--     never neither. flows_log_mv is left untouched and simply writes to the
--     new flows_log from the swap on.
--
--   Hence: create the new table under a temporary name, EXCHANGE it with
--   flows_log, then rename the temporary name (now holding the old data) to
--   flows_log_legacy. Integration test: TestIntegrationFlowLogMigration
--   (internal/store) — sum(flow_count) over flows_log_all equals count() of
--   flows_raw after migrating under continuous inserts.
--
-- Every statement is metadata-only (no part is read or rewritten), so the
-- migration takes milliseconds whatever the size of flows_log. The new table
-- is created on the default storage policy (a storage policy is operator
-- configuration); with a hot_cold policy configured and a cold tier set on
-- flows_log, the reconciler switches it to that policy by itself.
--
-- Re-running / recovery. ClickHouse has no transactional DDL and cmd/migrate
-- never replays a failed migration. The guards below make a re-run refuse to
-- start instead of swapping the tables back. If the migration failed:
--   * before the EXCHANGE: nothing changed (an empty flows_log_new may exist,
--     which a re-run reuses);
--   * after the EXCHANGE, before the RENAME: flows_log already has the new
--     layout and flows_log_new holds the old data — run
--       RENAME TABLE asstats.flows_log_new TO asstats.flows_log_legacy;
--     and the last statement by hand;
--   then record the migration as applied in schema_migrations.

-- Guard: not already applied.
SELECT throwIf(count() > 0, 'flows_log_legacy already exists: migration 000017 was already (partly) applied, see the recovery notes in 000017_flow_log_sort_key.up.sql')
FROM system.tables
WHERE database = currentDatabase() AND name = 'flows_log_legacy';

-- Guard: flows_log exists and still has the 000007 layout (not yet swapped).
SELECT throwIf(count() != 1, 'flows_log is missing or already has the 15-minute/dst_ip sort key: see the recovery notes in 000017_flow_log_sort_key.up.sql')
FROM system.tables
WHERE database = currentDatabase() AND name = 'flows_log'
  AND sorting_key NOT LIKE 'toStartOfFifteenMinutes(ts)%';

-- Guard: a leftover flows_log_new (from a run that failed before the swap) must
-- be empty; anything else needs a human.
SELECT throwIf(count() > 0, 'flows_log_new exists and holds data: see the recovery notes in 000017_flow_log_sort_key.up.sql')
FROM system.parts
WHERE database = currentDatabase() AND table = 'flows_log_new' AND active;

CREATE TABLE IF NOT EXISTS asstats.flows_log_new (
    ts              DateTime('UTC'),
    router_ip       IPv6,
    link_tag        LowCardinality(String),
    src_ip          IPv6,
    dst_ip          IPv6,
    src_as          UInt32,
    dst_as          UInt32,
    protocol        UInt8,
    src_port        UInt16,
    dst_port        UInt16,
    tcp_flags       UInt8,
    ip_version      UInt8,
    bytes           UInt64,
    packets         UInt64,
    flow_count      UInt64,

    INDEX idx_src_ip src_ip TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_src_as src_as TYPE minmax GRANULARITY 4,
    INDEX idx_dst_as dst_as TYPE minmax GRANULARITY 4
) ENGINE = SummingMergeTree((bytes, packets, flow_count))
PARTITION BY toYYYYMMDD(ts)
ORDER BY (toStartOfFifteenMinutes(ts), dst_ip, ts, link_tag, protocol, dst_port, src_ip, src_as, dst_as, src_port, tcp_flags, ip_version)
TTL ts + INTERVAL 180 DAY
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;

-- Atomic swap: flows_log_mv (TO asstats.flows_log, resolved by name) writes to
-- the new table from here on.
EXCHANGE TABLES asstats.flows_log AND asstats.flows_log_new;

RENAME TABLE asstats.flows_log_new TO asstats.flows_log_legacy;

-- Read side. The regex is anchored, so it matches exactly flows_log and
-- flows_log_legacy — not flows_log_mv, flows_log_new or flows_log_all itself.
-- currentDatabase() is resolved once, at CREATE time.
CREATE TABLE IF NOT EXISTS asstats.flows_log_all AS asstats.flows_log
ENGINE = Merge(currentDatabase(), '^flows_log(_legacy)?$');
