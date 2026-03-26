-- Add ip_version to traffic_by_link for IPv4/IPv6 split graphs
-- Requires table recreation because ORDER BY key changes

-- Step 1: Drop the materialized view (must be dropped before table changes)
DROP VIEW IF EXISTS asstats.traffic_by_link_mv;

-- Step 2: Create new table with ip_version in ORDER BY
CREATE TABLE IF NOT EXISTS asstats.traffic_by_link_new (
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
TTL ts + INTERVAL 365 DAY;

-- Step 3: Migrate existing data (ip_version=0 for legacy rows)
INSERT INTO asstats.traffic_by_link_new
SELECT ts, link_tag, 0, bytes_in, bytes_out, packets_in, packets_out, flow_count
FROM asstats.traffic_by_link;

-- Step 4: Swap tables
DROP TABLE asstats.traffic_by_link;
RENAME TABLE asstats.traffic_by_link_new TO asstats.traffic_by_link;

-- Step 5: Recreate MV with ip_version
CREATE MATERIALIZED VIEW IF NOT EXISTS asstats.traffic_by_link_mv
TO asstats.traffic_by_link AS
SELECT
    toStartOfFiveMinutes(timestamp) AS ts,
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
