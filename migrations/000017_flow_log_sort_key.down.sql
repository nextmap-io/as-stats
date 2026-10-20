-- Reverse of 000017 (manual use only; cmd/migrate never runs down files).
-- Swaps the 000007-layout table back under the flows_log name. Rows written
-- since the upgrade stay in the new-layout table, renamed flows_log_v17: copy
-- them back with INSERT INTO asstats.flows_log SELECT * FROM asstats.flows_log_v17
-- if they matter, then drop it. A 3.3 reader then queries flows_log directly.
DROP TABLE IF EXISTS asstats.flows_log_all;
EXCHANGE TABLES asstats.flows_log AND asstats.flows_log_legacy;
RENAME TABLE asstats.flows_log_legacy TO asstats.flows_log_v17;
