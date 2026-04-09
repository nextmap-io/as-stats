-- ============================================================================
-- Hostgroups: named collections of CIDRs with per-group alert thresholds
-- ============================================================================
-- A hostgroup lets operators scope alert rules to specific network segments
-- (e.g. "CDN servers" = ['10.0.1.0/24', '10.0.2.0/24']) instead of the
-- global LOCAL_AS prefixes. Each rule can optionally reference one hostgroup
-- via its `hostgroup_id` field.

CREATE TABLE IF NOT EXISTS asstats.hostgroups (
    id           UUID,
    name         String,
    description  String DEFAULT '',
    cidrs        Array(String),          -- e.g. ['10.0.1.0/24', '10.0.2.0/24']
    created_at   DateTime DEFAULT now(),
    updated_at   DateTime DEFAULT now(),
    deleted      UInt8 DEFAULT 0
) ENGINE = ReplacingMergeTree(updated_at)
ORDER BY id;


-- ============================================================================
-- alert_rules: two new columns for hostgroup scoping + carpet bombing
-- ============================================================================

-- hostgroup_id: when set (non-zero UUID), the engine resolves CIDRs from this
-- hostgroup instead of the global LOCAL_AS prefixes. Zero UUID = global.
ALTER TABLE asstats.alert_rules
    ADD COLUMN IF NOT EXISTS hostgroup_id UUID
    DEFAULT toUUID('00000000-0000-0000-0000-000000000000');

-- subnet_prefix_len: only used by the 'subnet_flood' rule type. Specifies
-- the IPv4 prefix length for carpet-bombing aggregation (e.g. 24 = /24).
-- The query adds 96 to convert to the IPv6-mapped prefix used internally.
ALTER TABLE asstats.alert_rules
    ADD COLUMN IF NOT EXISTS subnet_prefix_len UInt8 DEFAULT 0;
