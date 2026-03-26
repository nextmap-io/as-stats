-- AS-Stats ClickHouse Schema
-- Raw flows, aggregation tables, and materialized views

CREATE DATABASE IF NOT EXISTS asstats;

-- ============================================================================
-- Raw flows table: high-volume ingest, short retention (7 days)
-- ============================================================================
CREATE TABLE IF NOT EXISTS asstats.flows_raw (
    timestamp      DateTime64(3, 'UTC'),
    router_ip      IPv6,
    link_tag       LowCardinality(String) DEFAULT '',

    -- IP layer
    src_ip         IPv6,
    dst_ip         IPv6,
    ip_version     UInt8 DEFAULT 4,

    -- AS info
    src_as         UInt32 DEFAULT 0,
    dst_as         UInt32 DEFAULT 0,

    -- Prefix (CIDR notation)
    src_prefix     String DEFAULT '',
    dst_prefix     String DEFAULT '',

    -- Transport
    protocol       UInt8 DEFAULT 0,
    src_port       UInt16 DEFAULT 0,
    dst_port       UInt16 DEFAULT 0,
    tcp_flags      UInt8 DEFAULT 0,

    -- Counters
    bytes          UInt64,
    packets        UInt64,

    -- Metadata
    sampling_rate  UInt32 DEFAULT 1,
    direction      LowCardinality(String) DEFAULT '',  -- 'in' or 'out'
    flow_type      LowCardinality(String) DEFAULT 'netflow'
) ENGINE = MergeTree()
PARTITION BY toYYYYMMDD(timestamp)
ORDER BY (timestamp, router_ip, src_as, dst_as)
TTL toDateTime(timestamp) + INTERVAL 7 DAY
SETTINGS index_granularity = 8192;


-- ============================================================================
-- Aggregation: traffic by AS (5-minute buckets, 1 year retention)
-- ============================================================================
CREATE TABLE IF NOT EXISTS asstats.traffic_by_as (
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
TTL ts + INTERVAL 365 DAY;

-- MV: source AS -> outbound traffic
CREATE MATERIALIZED VIEW IF NOT EXISTS asstats.traffic_by_as_out_mv
TO asstats.traffic_by_as AS
SELECT
    toStartOfFiveMinutes(timestamp) AS ts,
    link_tag,
    'out' AS direction,
    src_as AS as_number,
    sum(bytes * sampling_rate) AS bytes,
    sum(packets * sampling_rate) AS packets,
    count() AS flow_count
FROM asstats.flows_raw
WHERE src_as > 0
GROUP BY ts, link_tag, as_number;

-- MV: destination AS -> inbound traffic
CREATE MATERIALIZED VIEW IF NOT EXISTS asstats.traffic_by_as_in_mv
TO asstats.traffic_by_as AS
SELECT
    toStartOfFiveMinutes(timestamp) AS ts,
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
-- Aggregation: traffic by IP (5-minute buckets, 30 day retention)
-- ============================================================================
CREATE TABLE IF NOT EXISTS asstats.traffic_by_ip (
    ts             DateTime('UTC'),
    link_tag       LowCardinality(String),
    direction      LowCardinality(String),
    ip_address     IPv6,
    as_number      UInt32,
    bytes          UInt64,
    packets        UInt64,
    flow_count     UInt64
) ENGINE = SummingMergeTree((bytes, packets, flow_count))
PARTITION BY toYYYYMMDD(ts)
ORDER BY (ts, link_tag, direction, ip_address)
TTL ts + INTERVAL 30 DAY;

-- MV: destination IP -> inbound
CREATE MATERIALIZED VIEW IF NOT EXISTS asstats.traffic_by_ip_in_mv
TO asstats.traffic_by_ip AS
SELECT
    toStartOfFiveMinutes(timestamp) AS ts,
    link_tag,
    'in' AS direction,
    dst_ip AS ip_address,
    dst_as AS as_number,
    sum(bytes * sampling_rate) AS bytes,
    sum(packets * sampling_rate) AS packets,
    count() AS flow_count
FROM asstats.flows_raw
GROUP BY ts, link_tag, ip_address, as_number;

-- MV: source IP -> outbound
CREATE MATERIALIZED VIEW IF NOT EXISTS asstats.traffic_by_ip_out_mv
TO asstats.traffic_by_ip AS
SELECT
    toStartOfFiveMinutes(timestamp) AS ts,
    link_tag,
    'out' AS direction,
    src_ip AS ip_address,
    src_as AS as_number,
    sum(bytes * sampling_rate) AS bytes,
    sum(packets * sampling_rate) AS packets,
    count() AS flow_count
FROM asstats.flows_raw
GROUP BY ts, link_tag, ip_address, as_number;


-- ============================================================================
-- Aggregation: traffic by prefix (5-minute buckets, 90 day retention)
-- ============================================================================
CREATE TABLE IF NOT EXISTS asstats.traffic_by_prefix (
    ts             DateTime('UTC'),
    link_tag       LowCardinality(String),
    direction      LowCardinality(String),
    prefix         String,
    as_number      UInt32,
    bytes          UInt64,
    packets        UInt64,
    flow_count     UInt64
) ENGINE = SummingMergeTree((bytes, packets, flow_count))
PARTITION BY toYYYYMM(ts)
ORDER BY (ts, link_tag, direction, prefix, as_number)
TTL ts + INTERVAL 90 DAY;

-- MV: destination prefix -> inbound
CREATE MATERIALIZED VIEW IF NOT EXISTS asstats.traffic_by_prefix_in_mv
TO asstats.traffic_by_prefix AS
SELECT
    toStartOfFiveMinutes(timestamp) AS ts,
    link_tag,
    'in' AS direction,
    dst_prefix AS prefix,
    dst_as AS as_number,
    sum(bytes * sampling_rate) AS bytes,
    sum(packets * sampling_rate) AS packets,
    count() AS flow_count
FROM asstats.flows_raw
WHERE dst_prefix != ''
GROUP BY ts, link_tag, prefix, as_number;

-- MV: source prefix -> outbound
CREATE MATERIALIZED VIEW IF NOT EXISTS asstats.traffic_by_prefix_out_mv
TO asstats.traffic_by_prefix AS
SELECT
    toStartOfFiveMinutes(timestamp) AS ts,
    link_tag,
    'out' AS direction,
    src_prefix AS prefix,
    src_as AS as_number,
    sum(bytes * sampling_rate) AS bytes,
    sum(packets * sampling_rate) AS packets,
    count() AS flow_count
FROM asstats.flows_raw
WHERE src_prefix != ''
GROUP BY ts, link_tag, prefix, as_number;


-- ============================================================================
-- Aggregation: traffic by link (5-minute buckets, 1 year retention)
-- ============================================================================
CREATE TABLE IF NOT EXISTS asstats.traffic_by_link (
    ts             DateTime('UTC'),
    link_tag       LowCardinality(String),
    bytes_in       UInt64,
    bytes_out      UInt64,
    packets_in     UInt64,
    packets_out    UInt64,
    flow_count     UInt64
) ENGINE = SummingMergeTree((bytes_in, bytes_out, packets_in, packets_out, flow_count))
PARTITION BY toYYYYMM(ts)
ORDER BY (ts, link_tag)
TTL ts + INTERVAL 365 DAY;

CREATE MATERIALIZED VIEW IF NOT EXISTS asstats.traffic_by_link_mv
TO asstats.traffic_by_link AS
SELECT
    toStartOfFiveMinutes(timestamp) AS ts,
    link_tag,
    sumIf(bytes * sampling_rate, direction = 'in') AS bytes_in,
    sumIf(bytes * sampling_rate, direction = 'out') AS bytes_out,
    sumIf(packets * sampling_rate, direction = 'in') AS packets_in,
    sumIf(packets * sampling_rate, direction = 'out') AS packets_out,
    count() AS flow_count
FROM asstats.flows_raw
WHERE link_tag != ''
GROUP BY ts, link_tag;


-- ============================================================================
-- Hourly aggregation for longer-range queries (traffic by AS)
-- ============================================================================
CREATE TABLE IF NOT EXISTS asstats.traffic_by_as_hourly (
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
TTL ts + INTERVAL 730 DAY;

CREATE MATERIALIZED VIEW IF NOT EXISTS asstats.traffic_by_as_hourly_out_mv
TO asstats.traffic_by_as_hourly AS
SELECT
    toStartOfHour(timestamp) AS ts,
    link_tag,
    'out' AS direction,
    src_as AS as_number,
    sum(bytes * sampling_rate) AS bytes,
    sum(packets * sampling_rate) AS packets,
    count() AS flow_count
FROM asstats.flows_raw
WHERE src_as > 0
GROUP BY ts, link_tag, as_number;

CREATE MATERIALIZED VIEW IF NOT EXISTS asstats.traffic_by_as_hourly_in_mv
TO asstats.traffic_by_as_hourly AS
SELECT
    toStartOfHour(timestamp) AS ts,
    link_tag,
    'in' AS direction,
    dst_as AS as_number,
    sum(bytes * sampling_rate) AS bytes,
    sum(packets * sampling_rate) AS packets,
    count() AS flow_count
FROM asstats.flows_raw
WHERE dst_as > 0
GROUP BY ts, link_tag, as_number;
