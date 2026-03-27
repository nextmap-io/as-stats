-- Add ip_version to traffic_by_as for IPv4/IPv6 split per AS
-- Same approach as migration 000003 for traffic_by_link

-- Step 1: Drop MVs
DROP VIEW IF EXISTS asstats.traffic_by_as_out_mv;
DROP VIEW IF EXISTS asstats.traffic_by_as_in_mv;

-- Step 2: Create new table with ip_version
CREATE TABLE IF NOT EXISTS asstats.traffic_by_as_new (
    ts             DateTime('UTC'),
    link_tag       LowCardinality(String),
    direction      LowCardinality(String),
    as_number      UInt32,
    ip_version     UInt8 DEFAULT 4,
    bytes          UInt64,
    packets        UInt64,
    flow_count     UInt64
) ENGINE = SummingMergeTree((bytes, packets, flow_count))
PARTITION BY toYYYYMM(ts)
ORDER BY (ts, link_tag, direction, as_number, ip_version)
TTL ts + INTERVAL 90 DAY;

-- Step 3: Migrate existing data
INSERT INTO asstats.traffic_by_as_new
SELECT ts, link_tag, direction, as_number, 0, bytes, packets, flow_count
FROM asstats.traffic_by_as;

-- Step 4: Swap tables
DROP TABLE asstats.traffic_by_as;
RENAME TABLE asstats.traffic_by_as_new TO asstats.traffic_by_as;

-- Step 5: Recreate MVs with ip_version
CREATE MATERIALIZED VIEW IF NOT EXISTS asstats.traffic_by_as_out_mv
TO asstats.traffic_by_as AS
SELECT
    toStartOfFiveMinutes(timestamp) AS ts,
    link_tag,
    'out' AS direction,
    src_as AS as_number,
    ip_version,
    sum(bytes * sampling_rate) AS bytes,
    sum(packets * sampling_rate) AS packets,
    count() AS flow_count
FROM asstats.flows_raw
WHERE src_as > 0
GROUP BY ts, link_tag, as_number, ip_version;

CREATE MATERIALIZED VIEW IF NOT EXISTS asstats.traffic_by_as_in_mv
TO asstats.traffic_by_as AS
SELECT
    toStartOfFiveMinutes(timestamp) AS ts,
    link_tag,
    'in' AS direction,
    dst_as AS as_number,
    ip_version,
    sum(bytes * sampling_rate) AS bytes,
    sum(packets * sampling_rate) AS packets,
    count() AS flow_count
FROM asstats.flows_raw
WHERE dst_as > 0
GROUP BY ts, link_tag, as_number, ip_version;

-- Also update hourly and daily AS rollups to include ip_version

-- Hourly
DROP VIEW IF EXISTS asstats.traffic_by_as_hourly_out_mv;
DROP VIEW IF EXISTS asstats.traffic_by_as_hourly_in_mv;

CREATE TABLE IF NOT EXISTS asstats.traffic_by_as_hourly_new (
    ts             DateTime('UTC'),
    link_tag       LowCardinality(String),
    direction      LowCardinality(String),
    as_number      UInt32,
    ip_version     UInt8 DEFAULT 4,
    bytes          UInt64,
    packets        UInt64,
    flow_count     UInt64
) ENGINE = SummingMergeTree((bytes, packets, flow_count))
PARTITION BY toYYYYMM(ts)
ORDER BY (ts, link_tag, direction, as_number, ip_version)
TTL ts + INTERVAL 730 DAY;

INSERT INTO asstats.traffic_by_as_hourly_new
SELECT ts, link_tag, direction, as_number, 0, bytes, packets, flow_count
FROM asstats.traffic_by_as_hourly;

DROP TABLE asstats.traffic_by_as_hourly;
RENAME TABLE asstats.traffic_by_as_hourly_new TO asstats.traffic_by_as_hourly;

CREATE MATERIALIZED VIEW IF NOT EXISTS asstats.traffic_by_as_hourly_out_mv
TO asstats.traffic_by_as_hourly AS
SELECT
    toStartOfHour(timestamp) AS ts,
    link_tag, 'out' AS direction, src_as AS as_number, ip_version,
    sum(bytes * sampling_rate) AS bytes,
    sum(packets * sampling_rate) AS packets,
    count() AS flow_count
FROM asstats.flows_raw
WHERE src_as > 0
GROUP BY ts, link_tag, as_number, ip_version;

CREATE MATERIALIZED VIEW IF NOT EXISTS asstats.traffic_by_as_hourly_in_mv
TO asstats.traffic_by_as_hourly AS
SELECT
    toStartOfHour(timestamp) AS ts,
    link_tag, 'in' AS direction, dst_as AS as_number, ip_version,
    sum(bytes * sampling_rate) AS bytes,
    sum(packets * sampling_rate) AS packets,
    count() AS flow_count
FROM asstats.flows_raw
WHERE dst_as > 0
GROUP BY ts, link_tag, as_number, ip_version;

-- Daily
DROP VIEW IF EXISTS asstats.traffic_by_as_daily_out_mv;
DROP VIEW IF EXISTS asstats.traffic_by_as_daily_in_mv;

CREATE TABLE IF NOT EXISTS asstats.traffic_by_as_daily_new (
    ts             DateTime('UTC'),
    link_tag       LowCardinality(String),
    direction      LowCardinality(String),
    as_number      UInt32,
    ip_version     UInt8 DEFAULT 4,
    bytes          UInt64,
    packets        UInt64,
    flow_count     UInt64
) ENGINE = SummingMergeTree((bytes, packets, flow_count))
PARTITION BY toYYYYMM(ts)
ORDER BY (ts, link_tag, direction, as_number, ip_version)
TTL ts + INTERVAL 1825 DAY;

INSERT INTO asstats.traffic_by_as_daily_new
SELECT ts, link_tag, direction, as_number, 0, bytes, packets, flow_count
FROM asstats.traffic_by_as_daily;

DROP TABLE asstats.traffic_by_as_daily;
RENAME TABLE asstats.traffic_by_as_daily_new TO asstats.traffic_by_as_daily;

CREATE MATERIALIZED VIEW IF NOT EXISTS asstats.traffic_by_as_daily_out_mv
TO asstats.traffic_by_as_daily AS
SELECT
    toStartOfDay(timestamp) AS ts,
    link_tag, 'out' AS direction, src_as AS as_number, ip_version,
    sum(bytes * sampling_rate) AS bytes,
    sum(packets * sampling_rate) AS packets,
    count() AS flow_count
FROM asstats.flows_raw
WHERE src_as > 0
GROUP BY ts, link_tag, as_number, ip_version;

CREATE MATERIALIZED VIEW IF NOT EXISTS asstats.traffic_by_as_daily_in_mv
TO asstats.traffic_by_as_daily AS
SELECT
    toStartOfDay(timestamp) AS ts,
    link_tag, 'in' AS direction, dst_as AS as_number, ip_version,
    sum(bytes * sampling_rate) AS bytes,
    sum(packets * sampling_rate) AS packets,
    count() AS flow_count
FROM asstats.flows_raw
WHERE dst_as > 0
GROUP BY ts, link_tag, as_number, ip_version;
