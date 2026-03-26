-- Rollback: remove ip_version from traffic_by_link

DROP VIEW IF EXISTS asstats.traffic_by_link_mv;

CREATE TABLE IF NOT EXISTS asstats.traffic_by_link_old (
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

INSERT INTO asstats.traffic_by_link_old
SELECT ts, link_tag, sum(bytes_in), sum(bytes_out), sum(packets_in), sum(packets_out), sum(flow_count)
FROM asstats.traffic_by_link
GROUP BY ts, link_tag;

DROP TABLE asstats.traffic_by_link;
RENAME TABLE asstats.traffic_by_link_old TO asstats.traffic_by_link;

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
