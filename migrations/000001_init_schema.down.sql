-- Drop materialized views first (they depend on flows_raw)
DROP VIEW IF EXISTS asstats.traffic_by_ip_as_out_mv;
DROP VIEW IF EXISTS asstats.traffic_by_ip_as_in_mv;
DROP VIEW IF EXISTS asstats.traffic_by_as_hourly_in_mv;
DROP VIEW IF EXISTS asstats.traffic_by_as_hourly_out_mv;
DROP VIEW IF EXISTS asstats.traffic_by_link_mv;
DROP VIEW IF EXISTS asstats.traffic_by_prefix_out_mv;
DROP VIEW IF EXISTS asstats.traffic_by_prefix_in_mv;
DROP VIEW IF EXISTS asstats.traffic_by_ip_out_mv;
DROP VIEW IF EXISTS asstats.traffic_by_ip_in_mv;
DROP VIEW IF EXISTS asstats.traffic_by_as_out_mv;
DROP VIEW IF EXISTS asstats.traffic_by_as_in_mv;

-- Drop aggregation tables
DROP TABLE IF EXISTS asstats.traffic_by_ip_as;
DROP TABLE IF EXISTS asstats.traffic_by_as_hourly;
DROP TABLE IF EXISTS asstats.traffic_by_link;
DROP TABLE IF EXISTS asstats.traffic_by_prefix;
DROP TABLE IF EXISTS asstats.traffic_by_ip;
DROP TABLE IF EXISTS asstats.traffic_by_as;

-- Drop raw table
DROP TABLE IF EXISTS asstats.flows_raw;
