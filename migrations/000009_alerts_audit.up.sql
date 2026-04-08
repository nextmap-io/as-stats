-- ============================================================================
-- Alert rules: configurable DDoS detection thresholds
-- ============================================================================
-- Admin-managed rules evaluated by the alert engine (collector goroutine).
-- Default rules are seeded on first startup.

CREATE TABLE IF NOT EXISTS asstats.alert_rules (
    id                UUID,
    name              String,
    description       String DEFAULT '',
    rule_type         LowCardinality(String),  -- 'volume_in', 'volume_out', 'syn_flood', 'amplification', 'port_scan', 'custom'
    enabled           UInt8 DEFAULT 1,
    threshold_bps     UInt64 DEFAULT 0,         -- bits per second (0 = unused)
    threshold_pps     UInt64 DEFAULT 0,         -- packets per second
    threshold_count   UInt64 DEFAULT 0,         -- for uniq() rules
    window_seconds    UInt32 DEFAULT 60,        -- evaluation window
    cooldown_seconds  UInt32 DEFAULT 300,       -- min time between re-alerts on same target
    severity          LowCardinality(String) DEFAULT 'warning',  -- 'info', 'warning', 'critical'
    target_filter     String DEFAULT '',        -- CIDR (only match IPs in this range) — enforced with LOCAL_AS
    custom_sql        String DEFAULT '',        -- advanced custom WHERE clause (for 'custom' type)
    action            LowCardinality(String) DEFAULT 'notify',  -- 'notify', 'ack_required', 'auto_block'
    webhook_ids       Array(UUID) DEFAULT [],   -- webhook configs to notify
    created_at        DateTime DEFAULT now(),
    updated_at        DateTime DEFAULT now(),
    deleted           UInt8 DEFAULT 0
) ENGINE = ReplacingMergeTree(updated_at)
ORDER BY id;


-- ============================================================================
-- Alerts: active + historical alerts triggered by rules
-- ============================================================================

CREATE TABLE IF NOT EXISTS asstats.alerts (
    id                UUID,
    rule_id           UUID,
    rule_name         String,
    severity          LowCardinality(String),
    triggered_at      DateTime('UTC'),
    last_seen_at      DateTime('UTC'),          -- last time the condition was still true
    resolved_at       DateTime('UTC') DEFAULT toDateTime(0),  -- 0 = still active
    target_ip         IPv6,
    target_as         UInt32 DEFAULT 0,
    protocol          UInt8 DEFAULT 0,
    metric_value      Float64,                  -- actual value (bps, pps, count)
    threshold         Float64,                  -- threshold that was exceeded
    metric_type       LowCardinality(String),   -- 'bps', 'pps', 'count'
    details           String DEFAULT '',        -- JSON with extra context (top sources, etc)
    status            LowCardinality(String) DEFAULT 'active',  -- 'active', 'acknowledged', 'resolved', 'muted'
    acknowledged_by   String DEFAULT '',        -- user email if auth enabled
    acknowledged_at   DateTime('UTC') DEFAULT toDateTime(0),
    action_taken      LowCardinality(String) DEFAULT 'none',    -- 'none', 'bgp_blackhole', 'manual_block'
    action_by         String DEFAULT '',
    action_at         DateTime('UTC') DEFAULT toDateTime(0)
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(triggered_at)
ORDER BY (triggered_at, severity, id)
TTL triggered_at + INTERVAL 365 DAY;


-- ============================================================================
-- Audit log: compliance trail of all sensitive actions
-- ============================================================================

CREATE TABLE IF NOT EXISTS asstats.audit_log (
    ts              DateTime('UTC'),
    user_sub        String,                    -- OIDC subject (empty if unauthenticated)
    user_email      String,
    user_role       LowCardinality(String),    -- 'admin', 'viewer', ''
    action          LowCardinality(String),    -- 'flow_search', 'flow_export', 'alert_ack', 'alert_block', 'rule_create', 'rule_update', 'rule_delete', 'link_create', 'link_delete', 'webhook_create', etc.
    resource        String,                    -- e.g. 'alerts/abc-123', 'rules/def-456'
    params          String DEFAULT '',         -- JSON of request parameters
    client_ip       IPv6,                      -- client source IP
    user_agent      String DEFAULT '',
    result          LowCardinality(String),    -- 'success', 'denied', 'error'
    error_message   String DEFAULT ''
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(ts)
ORDER BY (ts, user_sub, action)
TTL ts + INTERVAL 365 DAY;


-- ============================================================================
-- Webhook configurations
-- ============================================================================
-- Extensible: 'slack' for now, 'teams', 'discord', 'generic' later

CREATE TABLE IF NOT EXISTS asstats.webhook_configs (
    id           UUID,
    name         String,
    webhook_type LowCardinality(String),       -- 'slack', 'teams', 'discord', 'generic'
    url          String,                        -- incoming webhook URL
    enabled      UInt8 DEFAULT 1,
    min_severity LowCardinality(String) DEFAULT 'warning',  -- minimum severity to trigger
    headers      String DEFAULT '',             -- JSON of extra HTTP headers
    template     String DEFAULT '',             -- optional custom message template
    created_at   DateTime DEFAULT now(),
    updated_at   DateTime DEFAULT now(),
    deleted      UInt8 DEFAULT 0
) ENGINE = ReplacingMergeTree(updated_at)
ORDER BY id;
