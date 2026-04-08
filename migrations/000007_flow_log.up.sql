-- ============================================================================
-- flows_log: forensic flow log with full tuple dimensions
-- 1-minute buckets, 180-day retention by default (configurable via ALTER)
-- ============================================================================
--
-- This table is populated by a MV from flows_raw and keeps all tuple
-- dimensions (IPs, ports, protocol, TCP flags) for compliance and forensic
-- search. Typical use: "all flows from IP X on date Y", "who accessed port Z".
--
-- Storage estimates (compressed with LZ4, typical 3-5x):
--   500 flows/sec  -> ~1.4 GB/day -> ~250 GB / 180 days
--   5k flows/sec   -> ~10 GB/day  -> ~1.8 TB / 180 days
--   50k flows/sec  -> ~80 GB/day  -> ~14 TB / 180 days
--
-- To change retention after deploy:
--   ALTER TABLE asstats.flows_log MODIFY TTL ts + INTERVAL N DAY;

CREATE TABLE IF NOT EXISTS asstats.flows_log (
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

    -- Skip indexes for fast forensic queries
    INDEX idx_src_ip src_ip TYPE bloom_filter(0.01) GRANULARITY 4,
    INDEX idx_dst_ip dst_ip TYPE bloom_filter(0.01) GRANULARITY 4,
    INDEX idx_src_as src_as TYPE minmax GRANULARITY 4,
    INDEX idx_dst_as dst_as TYPE minmax GRANULARITY 4
) ENGINE = SummingMergeTree((bytes, packets, flow_count))
PARTITION BY toYYYYMMDD(ts)
ORDER BY (ts, link_tag, protocol, dst_port, src_ip, dst_ip, src_as, dst_as, src_port, tcp_flags, ip_version)
TTL ts + INTERVAL 180 DAY
SETTINGS index_granularity = 8192;

-- Single MV: preserves all tuple dimensions, aggregates to 1-minute buckets
CREATE MATERIALIZED VIEW IF NOT EXISTS asstats.flows_log_mv
TO asstats.flows_log AS
SELECT
    toStartOfMinute(timestamp) AS ts,
    router_ip,
    link_tag,
    src_ip,
    dst_ip,
    src_as,
    dst_as,
    protocol,
    src_port,
    dst_port,
    tcp_flags,
    ip_version,
    sum(bytes * sampling_rate) AS bytes,
    sum(packets * sampling_rate) AS packets,
    count() AS flow_count
FROM asstats.flows_raw
GROUP BY ts, router_ip, link_tag, src_ip, dst_ip,
         src_as, dst_as, protocol, src_port, dst_port, tcp_flags, ip_version;


-- ============================================================================
-- traffic_by_port: aggregated port-level statistics
-- 5-minute buckets, 1-year retention
-- ============================================================================
-- Lightweight table for "Top Ports" and "Top Protocols" views.
-- No IP dimensions, so cardinality is tiny (~300 rows per 5-min bucket).

CREATE TABLE IF NOT EXISTS asstats.traffic_by_port (
    ts              DateTime('UTC'),
    link_tag        LowCardinality(String),
    direction       LowCardinality(String),  -- 'in' or 'out'
    protocol        UInt8,
    port            UInt16,
    ip_version      UInt8,
    bytes           UInt64,
    packets         UInt64,
    flow_count      UInt64
) ENGINE = SummingMergeTree((bytes, packets, flow_count))
PARTITION BY toYYYYMM(ts)
ORDER BY (ts, link_tag, direction, protocol, port, ip_version)
TTL ts + INTERVAL 365 DAY;

-- Inbound: dst_port = service being accessed
CREATE MATERIALIZED VIEW IF NOT EXISTS asstats.traffic_by_port_in_mv
TO asstats.traffic_by_port AS
SELECT
    toStartOfFiveMinutes(timestamp) AS ts,
    link_tag,
    'in' AS direction,
    protocol,
    dst_port AS port,
    ip_version,
    sum(bytes * sampling_rate) AS bytes,
    sum(packets * sampling_rate) AS packets,
    count() AS flow_count
FROM asstats.flows_raw
WHERE dst_port > 0 OR protocol = 1  -- keep ICMP with port=0
GROUP BY ts, link_tag, protocol, port, ip_version;

-- Outbound: src_port = service used by us
CREATE MATERIALIZED VIEW IF NOT EXISTS asstats.traffic_by_port_out_mv
TO asstats.traffic_by_port AS
SELECT
    toStartOfFiveMinutes(timestamp) AS ts,
    link_tag,
    'out' AS direction,
    protocol,
    src_port AS port,
    ip_version,
    sum(bytes * sampling_rate) AS bytes,
    sum(packets * sampling_rate) AS packets,
    count() AS flow_count
FROM asstats.flows_raw
WHERE src_port > 0 OR protocol = 1
GROUP BY ts, link_tag, protocol, port, ip_version;
