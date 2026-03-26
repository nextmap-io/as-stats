-- Known links: maps (router_ip, snmp_index) to a named link
CREATE TABLE IF NOT EXISTS asstats.links (
    tag           String,
    router_ip     IPv6,
    snmp_index    UInt32 DEFAULT 0,
    description   String DEFAULT '',
    group_name    String DEFAULT '',
    color         String DEFAULT '',
    capacity_mbps UInt32 DEFAULT 0,
    updated_at    DateTime DEFAULT now()
) ENGINE = ReplacingMergeTree(updated_at)
ORDER BY (tag);

-- AS names: maps AS number to name/org/country
CREATE TABLE IF NOT EXISTS asstats.as_names (
    as_number    UInt32,
    as_name      String,
    as_org       String DEFAULT '',
    country      LowCardinality(String) DEFAULT '',
    updated_at   DateTime DEFAULT now()
) ENGINE = ReplacingMergeTree(updated_at)
ORDER BY as_number;
