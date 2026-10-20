-- ============================================================================
-- traffic_by_port: one row per flow, real direction, service port
-- ============================================================================
--
-- Why. Migration 000007 fed traffic_by_port from two materialised views over
-- the SAME flows_raw rows:
--   traffic_by_port_in_mv   direction 'in',  port = dst_port
--   traffic_by_port_out_mv  direction 'out', port = src_port
-- so every flow was stored twice, and "direction" only said which port column
-- had been copied. Aggregated per protocol (Top Protocols), in == out by
-- construction; every total was doubled; Top Ports "in" listed the ephemeral
-- client ports of every download next to the services.
--
-- New semantics (traffic_by_port_mv, a single view):
--   * one row per flows_raw row — never two;
--   * direction = flows_raw.direction (set by the enricher from the link the
--     flow was credited to: 'in' = entered our network on that link, 'out' =
--     left it). Flows not credited to a configured link (link_tag = '') are
--     stored with direction '' even when the exporter sent a flowDirection,
--     which is router-relative, not network-relative. They are counted once,
--     never shown under Inbound / Outbound (same population as the link
--     totals, which ignore them too), and still visible to direction-agnostic
--     readers (Changes › movers by port);
--   * port = the SERVICE port: the lower of src_port / dst_port when both are
--     set (client ports are ephemeral, i.e. high), the non-zero one when one
--     is 0, 0 when both are; ICMP / ICMPv6 (protocol 1 / 58) always 0 (their
--     "ports" are type/code); store.ServicePort is the Go mirror;
--   * every flow is kept, including portless protocols (GRE, ESP, …) with
--     port 0, so per-protocol sums equal flows_raw exactly.
-- Columns, 5-minute bucketing, ORDER BY, partitioning and the
-- × sampling_rate scaling are unchanged.
--
-- No data is copied or converted: the old rows cannot be split back into
-- flows (the two copies of a flow are not linked), so port history restarts at
-- this upgrade. The existing table is kept, renamed traffic_by_port_legacy,
-- read by nothing, and ages out under the traffic_by_port retention policy
-- (the reconciler applies its ttl_days / cold_after_days to it, like
-- flows_log_legacy); a later release can drop it.
--
-- How (same mechanism as 000017, see the notes there and bug #19 in
-- CLAUDE.md): a `TO` materialised view resolves its target BY NAME at insert
-- time, and a plain RENAME of a target makes every INSERT INTO flows_raw fail
-- until the name reappears. So:
--   1. create the new, empty table under a temporary name;
--   2. drop the two old views — from here no view writes to traffic_by_port;
--   3. EXCHANGE it with traffic_by_port (atomic): the name now holds the new,
--      empty table, the temporary name the old data;
--   4. rename the temporary name to traffic_by_port_legacy;
--   5. create traffic_by_port_mv TO traffic_by_port.
-- An INSERT INTO flows_raw sees one set of views for the whole insert, so each
-- block lands in the new table exactly once (if it started after step 5) or
-- not at all (if it started before), never twice, never half; the old views
-- never write into the new table because they are gone before the EXCHANGE;
-- no step leaves a view whose target is missing, so no INSERT fails. Blocks
-- inserted during the milliseconds between steps 2 and 5 are in flows_raw but
-- in neither port table: they fall in the first, partial, 5-minute bucket of
-- the new history. Integration test: TestIntegrationPortStatsMigration
-- (internal/store) — 4 concurrent inserters across the migration, every block
-- in the new table exactly once from a point on, no INSERT error.
--
-- Every statement is metadata-only; the migration takes milliseconds whatever
-- the size of traffic_by_port. The new table is created on the default storage
-- policy; with a hot_cold policy and a cold tier set on traffic_by_port, the
-- reconciler switches it to that policy by itself (adoptColdPolicy).
--
-- Re-running / recovery. ClickHouse has no transactional DDL and cmd/migrate
-- never replays a failed migration. The guards make a re-run refuse to start.
-- If the migration failed:
--   * before the DROPs: nothing changed (an empty traffic_by_port_new may
--     exist, which a re-run reuses);
--   * after the DROPs, before the EXCHANGE: port stats are not recorded until
--     you finish by hand: run the EXCHANGE, RENAME and CREATE MATERIALIZED VIEW
--     below (the old views must NOT be recreated);
--   * after the EXCHANGE, before the RENAME: run
--       RENAME TABLE asstats.traffic_by_port_new TO asstats.traffic_by_port_legacy;
--     and the last statement;
--   then record the migration as applied in schema_migrations.

-- Guard: not already applied.
SELECT throwIf(count() > 0, 'traffic_by_port_legacy or traffic_by_port_mv already exists: migration 000018 was already (partly) applied, see the recovery notes in 000018_port_stats_service_port.up.sql')
FROM system.tables
WHERE database = currentDatabase() AND name IN ('traffic_by_port_legacy', 'traffic_by_port_mv');

-- Guard: traffic_by_port exists (created by 000007).
SELECT throwIf(count() != 1, 'traffic_by_port is missing: see the recovery notes in 000018_port_stats_service_port.up.sql')
FROM system.tables
WHERE database = currentDatabase() AND name = 'traffic_by_port';

-- Guard: a leftover traffic_by_port_new (from a run that failed before the
-- swap) must be empty; anything else needs a human.
SELECT throwIf(count() > 0, 'traffic_by_port_new exists and holds data: see the recovery notes in 000018_port_stats_service_port.up.sql')
FROM system.parts
WHERE database = currentDatabase() AND table = 'traffic_by_port_new' AND active;

CREATE TABLE IF NOT EXISTS asstats.traffic_by_port_new (
    ts              DateTime('UTC'),
    link_tag        LowCardinality(String),
    direction       LowCardinality(String),  -- flows_raw.direction: 'in', 'out', or '' (no configured link)
    protocol        UInt8,
    port            UInt16,                  -- service port (lower of the two), 0 for ICMP / portless
    ip_version      UInt8,
    bytes           UInt64,
    packets         UInt64,
    flow_count      UInt64
) ENGINE = SummingMergeTree((bytes, packets, flow_count))
PARTITION BY toYYYYMM(ts)
ORDER BY (ts, link_tag, direction, protocol, port, ip_version)
TTL ts + INTERVAL 365 DAY;

-- From here until the CREATE MATERIALIZED VIEW below, no view writes port
-- statistics (see the header).
DROP VIEW IF EXISTS asstats.traffic_by_port_in_mv;
DROP VIEW IF EXISTS asstats.traffic_by_port_out_mv;

EXCHANGE TABLES asstats.traffic_by_port AND asstats.traffic_by_port_new;

RENAME TABLE asstats.traffic_by_port_new TO asstats.traffic_by_port_legacy;

-- Column references are qualified (f.) so the `direction` / `port` aliases
-- never shadow the source columns (bug #2 in CLAUDE.md).
CREATE MATERIALIZED VIEW IF NOT EXISTS asstats.traffic_by_port_mv
TO asstats.traffic_by_port AS
SELECT
    toStartOfFiveMinutes(f.timestamp) AS ts,
    f.link_tag AS link_tag,
    if(f.link_tag = '', '', f.direction) AS direction,
    f.protocol AS protocol,
    multiIf(f.protocol IN (1, 58), 0,
            f.src_port = 0, f.dst_port,
            f.dst_port = 0, f.src_port,
            least(f.src_port, f.dst_port)) AS port,
    f.ip_version AS ip_version,
    sum(f.bytes * f.sampling_rate) AS bytes,
    sum(f.packets * f.sampling_rate) AS packets,
    count() AS flow_count
FROM asstats.flows_raw f
GROUP BY ts, link_tag, direction, protocol, port, ip_version;
