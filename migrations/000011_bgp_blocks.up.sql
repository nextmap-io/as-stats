-- ============================================================================
-- BGP blackhole block records
-- ============================================================================
-- Tracks every block/unblock action for audit and state recovery.
-- On API server startup, the GoBGP client loads all 'active' rows and
-- re-announces them so protection survives restarts.

CREATE TABLE IF NOT EXISTS asstats.bgp_blocks (
    id                UUID,
    ip                IPv6,
    prefix_len        UInt8 DEFAULT 32,
    community         String DEFAULT '65535:666',
    next_hop          String DEFAULT '',
    reason            LowCardinality(String) DEFAULT '',     -- 'auto_block', 'manual'
    description       String DEFAULT '',
    status            LowCardinality(String) DEFAULT 'active', -- 'active', 'withdrawn'

    blocked_by        String DEFAULT '',                     -- user email or 'engine'
    blocked_at        DateTime('UTC') DEFAULT now(),

    unblocked_by      String DEFAULT '',
    unblocked_at      DateTime('UTC') DEFAULT toDateTime(0),
    unblock_reason    String DEFAULT '',

    alert_id          UUID DEFAULT toUUID('00000000-0000-0000-0000-000000000000'),
    rule_name         String DEFAULT '',
    metric_value      Float64 DEFAULT 0,
    metric_type       LowCardinality(String) DEFAULT '',
    threshold         Float64 DEFAULT 0,
    top_sources       Array(String) DEFAULT [],

    duration_seconds  UInt32 DEFAULT 0,
    expires_at        DateTime('UTC') DEFAULT toDateTime(0)
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(blocked_at)
ORDER BY (blocked_at, ip, id)
TTL blocked_at + INTERVAL 365 DAY;
