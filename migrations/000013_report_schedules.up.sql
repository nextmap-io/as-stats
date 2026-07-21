-- ============================================================================
-- Scheduled reports (Module D) — report_schedules
-- ============================================================================
-- One row per scheduled report. A cron goroutine in the collector ticks every
-- minute (when FEATURE_REPORTS=true), finds enabled non-deleted schedules that
-- are DUE for the current occurrence, renders an HTML summary + CSV and delivers
-- them via SMTP, then stamps last_run_at.
--
-- ReplacingMergeTree(updated_at) so the latest edit always wins; ORDER BY id
-- keeps one logical row per schedule. Always read with FINAL and filter
-- deleted = 0 (soft-delete tombstones are purged by the config-purge goroutine).
--
--   frequency    : 'daily' | 'weekly' | 'monthly'
--   hour         : hour of day (0-23, UTC) the report fires
--   day_of_week  : 0-6 (0 = Sunday) — only used when frequency = 'weekly'
--   day_of_month : 1-28 — only used when frequency = 'monthly' (capped at 28 so
--                  every month has the day)
--   recipients   : comma-separated list of destination email addresses
--   sections     : comma-separated subset of
--                  overview,top_as,top_country,capacity,alerts
--   format       : 'html' | 'csv' | 'both'

CREATE TABLE IF NOT EXISTS asstats.report_schedules (
    id           String,
    name         String,
    frequency    LowCardinality(String),           -- daily | weekly | monthly
    hour         UInt8,                             -- 0-23, UTC
    day_of_week  UInt8 DEFAULT 1,                   -- 0-6 (weekly)
    day_of_month UInt8 DEFAULT 1,                   -- 1-28 (monthly)
    recipients   String,                            -- comma-separated emails
    sections     String,                            -- comma-separated section keys
    format       LowCardinality(String),           -- html | csv | both
    enabled      UInt8 DEFAULT 1,
    last_run_at  DateTime DEFAULT toDateTime(0),
    deleted      UInt8 DEFAULT 0,
    created_at   DateTime DEFAULT now(),
    updated_at   DateTime DEFAULT now()
) ENGINE = ReplacingMergeTree(updated_at)
ORDER BY id;
