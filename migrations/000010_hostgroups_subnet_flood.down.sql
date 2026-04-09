DROP TABLE IF EXISTS asstats.hostgroups;
ALTER TABLE asstats.alert_rules DROP COLUMN IF EXISTS hostgroup_id;
ALTER TABLE asstats.alert_rules DROP COLUMN IF EXISTS subnet_prefix_len;
