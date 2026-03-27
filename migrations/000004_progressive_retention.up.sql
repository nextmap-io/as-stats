-- Progressive retention: reduce fine-grained TTLs, add coarser rollups
-- Strategy:
--   5-min tables: short retention (query directly for recent data)
--   1-hour tables: medium retention (weeks to months)
--   1-day tables: long retention (years)

-- ============================================================================
-- Step 1: Reduce TTLs on fine-grained tables
-- ============================================================================
ALTER TABLE asstats.flows_raw MODIFY TTL toDateTime(timestamp) + INTERVAL 3 DAY;
ALTER TABLE asstats.traffic_by_as MODIFY TTL ts + INTERVAL 90 DAY;
ALTER TABLE asstats.traffic_by_ip MODIFY TTL ts + INTERVAL 14 DAY;
ALTER TABLE asstats.traffic_by_prefix MODIFY TTL ts + INTERVAL 30 DAY;
ALTER TABLE asstats.traffic_by_link MODIFY TTL ts + INTERVAL 90 DAY;
ALTER TABLE asstats.traffic_by_ip_as MODIFY TTL ts + INTERVAL 14 DAY;

-- ============================================================================
-- Step 2: Hourly link rollup (2 years)
-- ============================================================================
CREATE TABLE IF NOT EXISTS asstats.traffic_by_link_hourly (
    ts             DateTime('UTC'),
    link_tag       LowCardinality(String),
    ip_version     UInt8 DEFAULT 4,
    bytes_in       UInt64,
    bytes_out      UInt64,
    packets_in     UInt64,
    packets_out    UInt64,
    flow_count     UInt64
) ENGINE = SummingMergeTree((bytes_in, bytes_out, packets_in, packets_out, flow_count))
PARTITION BY toYYYYMM(ts)
ORDER BY (ts, link_tag, ip_version)
TTL ts + INTERVAL 730 DAY;

CREATE MATERIALIZED VIEW IF NOT EXISTS asstats.traffic_by_link_hourly_mv
TO asstats.traffic_by_link_hourly AS
SELECT
    toStartOfHour(timestamp) AS ts,
    link_tag,
    ip_version,
    sumIf(bytes * sampling_rate, direction = 'in') AS bytes_in,
    sumIf(bytes * sampling_rate, direction = 'out') AS bytes_out,
    sumIf(packets * sampling_rate, direction = 'in') AS packets_in,
    sumIf(packets * sampling_rate, direction = 'out') AS packets_out,
    count() AS flow_count
FROM asstats.flows_raw
WHERE link_tag != ''
GROUP BY ts, link_tag, ip_version;

-- ============================================================================
-- Step 3: Daily AS rollup (5 years)
-- ============================================================================
CREATE TABLE IF NOT EXISTS asstats.traffic_by_as_daily (
    ts             DateTime('UTC'),
    link_tag       LowCardinality(String),
    direction      LowCardinality(String),
    as_number      UInt32,
    bytes          UInt64,
    packets        UInt64,
    flow_count     UInt64
) ENGINE = SummingMergeTree((bytes, packets, flow_count))
PARTITION BY toYYYYMM(ts)
ORDER BY (ts, link_tag, direction, as_number)
TTL ts + INTERVAL 1825 DAY;

CREATE MATERIALIZED VIEW IF NOT EXISTS asstats.traffic_by_as_daily_out_mv
TO asstats.traffic_by_as_daily AS
SELECT
    toStartOfDay(timestamp) AS ts,
    link_tag,
    'out' AS direction,
    src_as AS as_number,
    sum(bytes * sampling_rate) AS bytes,
    sum(packets * sampling_rate) AS packets,
    count() AS flow_count
FROM asstats.flows_raw
WHERE src_as > 0
GROUP BY ts, link_tag, as_number;

CREATE MATERIALIZED VIEW IF NOT EXISTS asstats.traffic_by_as_daily_in_mv
TO asstats.traffic_by_as_daily AS
SELECT
    toStartOfDay(timestamp) AS ts,
    link_tag,
    'in' AS direction,
    dst_as AS as_number,
    sum(bytes * sampling_rate) AS bytes,
    sum(packets * sampling_rate) AS packets,
    count() AS flow_count
FROM asstats.flows_raw
WHERE dst_as > 0
GROUP BY ts, link_tag, as_number;

-- ============================================================================
-- Step 4: Daily link rollup (5 years)
-- ============================================================================
CREATE TABLE IF NOT EXISTS asstats.traffic_by_link_daily (
    ts             DateTime('UTC'),
    link_tag       LowCardinality(String),
    ip_version     UInt8 DEFAULT 4,
    bytes_in       UInt64,
    bytes_out      UInt64,
    packets_in     UInt64,
    packets_out    UInt64,
    flow_count     UInt64
) ENGINE = SummingMergeTree((bytes_in, bytes_out, packets_in, packets_out, flow_count))
PARTITION BY toYYYYMM(ts)
ORDER BY (ts, link_tag, ip_version)
TTL ts + INTERVAL 1825 DAY;

CREATE MATERIALIZED VIEW IF NOT EXISTS asstats.traffic_by_link_daily_mv
TO asstats.traffic_by_link_daily AS
SELECT
    toStartOfDay(timestamp) AS ts,
    link_tag,
    ip_version,
    sumIf(bytes * sampling_rate, direction = 'in') AS bytes_in,
    sumIf(bytes * sampling_rate, direction = 'out') AS bytes_out,
    sumIf(packets * sampling_rate, direction = 'in') AS packets_in,
    sumIf(packets * sampling_rate, direction = 'out') AS packets_out,
    count() AS flow_count
FROM asstats.flows_raw
WHERE link_tag != ''
GROUP BY ts, link_tag, ip_version;
