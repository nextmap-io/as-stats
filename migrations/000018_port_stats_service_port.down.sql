-- Reverse of 000018 (manual use only; cmd/migrate never runs down files).
-- Swaps the 000007-semantics table back under the traffic_by_port name and
-- restores the two double-counting views. Rows written since the upgrade stay
-- in the new-semantics table, renamed traffic_by_port_v18 (their meaning
-- differs: do not copy them back), drop it when done.
DROP VIEW IF EXISTS asstats.traffic_by_port_mv;
EXCHANGE TABLES asstats.traffic_by_port AND asstats.traffic_by_port_legacy;
RENAME TABLE asstats.traffic_by_port_legacy TO asstats.traffic_by_port_v18;

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
WHERE dst_port > 0 OR protocol = 1
GROUP BY ts, link_tag, protocol, port, ip_version;

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
