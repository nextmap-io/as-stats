-- ============================================================================
-- Alert engine: per-host baseline mode and re-armed alerts (000020)
-- ============================================================================
--
-- alert_rules.baseline_factor (volume_in / volume_out rules only):
--   0   = absolute threshold (the 3.6 behaviour, and every existing rule's
--         value after this migration)
--   > 0 = the rule fires only when the target is above the threshold AND at
--         least baseline_factor times its usual level: the per-minute p99 at
--         the same time of day over the previous 7 days (the Live Threats
--         history, floors 1 Mb/s / 1000 pps). A host without that history is
--         judged on the threshold alone, so a flood on a new host still fires.
--   The collector sets it on the seeded "High inbound volume", "High outbound
--   volume" and "Sustained outbound exfiltration" rules once, at startup, and
--   only while they are still exactly as seeded (alerts.ReconcileSeededRules).
--
-- alerts.reopen_count: how many times the engine re-opened this alert because
--   the condition came back within the re-arm window after it auto-resolved,
--   instead of creating (and notifying) a new alert.
--
-- Both are metadata-only ADD COLUMNs with a constant default: no mutation, no
-- part is rewritten.

ALTER TABLE asstats.alert_rules
    ADD COLUMN IF NOT EXISTS baseline_factor Float64 DEFAULT 0;

ALTER TABLE asstats.alerts
    ADD COLUMN IF NOT EXISTS reopen_count UInt32 DEFAULT 0;
