-- Rollback: remove rollup tables and restore original TTLs

DROP VIEW IF EXISTS asstats.traffic_by_link_daily_mv;
DROP VIEW IF EXISTS asstats.traffic_by_as_daily_in_mv;
DROP VIEW IF EXISTS asstats.traffic_by_as_daily_out_mv;
DROP VIEW IF EXISTS asstats.traffic_by_link_hourly_mv;

DROP TABLE IF EXISTS asstats.traffic_by_link_daily;
DROP TABLE IF EXISTS asstats.traffic_by_as_daily;
DROP TABLE IF EXISTS asstats.traffic_by_link_hourly;

ALTER TABLE asstats.flows_raw MODIFY TTL toDateTime(timestamp) + INTERVAL 7 DAY;
ALTER TABLE asstats.traffic_by_as MODIFY TTL ts + INTERVAL 365 DAY;
ALTER TABLE asstats.traffic_by_ip MODIFY TTL ts + INTERVAL 30 DAY;
ALTER TABLE asstats.traffic_by_prefix MODIFY TTL ts + INTERVAL 90 DAY;
ALTER TABLE asstats.traffic_by_link MODIFY TTL ts + INTERVAL 365 DAY;
ALTER TABLE asstats.traffic_by_ip_as MODIFY TTL ts + INTERVAL 30 DAY;
