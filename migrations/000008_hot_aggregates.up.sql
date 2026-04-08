-- ============================================================================
-- Hot pre-aggregation tables for DDoS detection and alert rules
-- 1-minute buckets, 7-day retention
-- ============================================================================
--
-- These tables exist so the alert engine can run fast queries without
-- scanning flows_raw or flows_log. All alerting queries hit these tables.
--
-- traffic_by_dst_1min: aggregates by destination IP (targets of potential attacks)
-- traffic_by_src_1min: aggregates by source IP (potential bots / infected hosts)
--
-- Both use AggregatingMergeTree for HyperLogLog-based unique counting.

CREATE TABLE IF NOT EXISTS asstats.traffic_by_dst_1min (
    ts              DateTime('UTC'),
    dst_ip          IPv6,
    protocol        UInt8,
    bytes           UInt64,
    packets         UInt64,
    flow_count      UInt64,
    syn_count       UInt64,    -- TCP SYN-only (flag & 2 != 0 && flag & 16 == 0)
    unique_src_ips  AggregateFunction(uniq, IPv6),
    unique_src_ports AggregateFunction(uniq, UInt16)
) ENGINE = AggregatingMergeTree()
PARTITION BY toYYYYMMDD(ts)
ORDER BY (ts, dst_ip, protocol)
TTL ts + INTERVAL 7 DAY;

CREATE MATERIALIZED VIEW IF NOT EXISTS asstats.traffic_by_dst_1min_mv
TO asstats.traffic_by_dst_1min AS
SELECT
    toStartOfMinute(timestamp) AS ts,
    dst_ip,
    protocol,
    sum(bytes * sampling_rate) AS bytes,
    sum(packets * sampling_rate) AS packets,
    count() AS flow_count,
    sumIf(packets * sampling_rate, (tcp_flags & 2) != 0 AND (tcp_flags & 16) = 0) AS syn_count,
    uniqState(src_ip) AS unique_src_ips,
    uniqState(src_port) AS unique_src_ports
FROM asstats.flows_raw
GROUP BY ts, dst_ip, protocol;


CREATE TABLE IF NOT EXISTS asstats.traffic_by_src_1min (
    ts               DateTime('UTC'),
    src_ip           IPv6,
    protocol         UInt8,
    bytes            UInt64,
    packets          UInt64,
    flow_count       UInt64,
    unique_dst_ips   AggregateFunction(uniq, IPv6),
    unique_dst_ports AggregateFunction(uniq, UInt16)
) ENGINE = AggregatingMergeTree()
PARTITION BY toYYYYMMDD(ts)
ORDER BY (ts, src_ip, protocol)
TTL ts + INTERVAL 7 DAY;

CREATE MATERIALIZED VIEW IF NOT EXISTS asstats.traffic_by_src_1min_mv
TO asstats.traffic_by_src_1min AS
SELECT
    toStartOfMinute(timestamp) AS ts,
    src_ip,
    protocol,
    sum(bytes * sampling_rate) AS bytes,
    sum(packets * sampling_rate) AS packets,
    count() AS flow_count,
    uniqState(dst_ip) AS unique_dst_ips,
    uniqState(dst_port) AS unique_dst_ports
FROM asstats.flows_raw
GROUP BY ts, src_ip, protocol;
