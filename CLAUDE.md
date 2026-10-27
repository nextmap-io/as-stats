# AS-Stats — internal architecture notes

This file is consumed by automated coding agents. It is **not** the user
documentation — see [`README.md`](README.md) for that. The goal here is to
give an agent enough context to make non-trivial changes without re-reading
the entire codebase from scratch every time.

## What this is

A Go + ClickHouse + React stack that ingests NetFlow / sFlow / IPFIX from
routers and serves per-AS / per-IP / per-prefix traffic analytics, plus
optional forensic flow log, port stats, and a DDoS detection engine.

```
Routers ── UDP(2055/6343) ─► Collector(Go) ── batch INSERT ─► ClickHouse
                                                                   │
                                          API(Go) ── REST JSON ─► Frontend(React)
                                                                   │
                                  optional: alert engine reads hot 1-min tables
```

Single Go module, two binaries (`cmd/collector`, `cmd/api`), React frontend
in `frontend/`.

## Key decisions

- **ClickHouse only** — no other store. `SummingMergeTree` for aggregates,
  materialised views fire on INSERT to `flows_raw`. **Always `sum()` in
  reads** to handle not-yet-merged rows.
- **Separate MVs per direction** (`_in_mv` / `_out_mv`) writing to the same
  target table — no `UNION ALL` in MVs (ClickHouse does not support it for
  this pattern). That is right only when the two views write *different
  entities* of one flow (src AS / dst AS, src IP / dst IP); two views writing
  the same entity count every flow twice (bug #21, `traffic_by_port`, now one
  view keyed on `flows_raw.direction`).
- **IPv6 columns everywhere** — IPv4 is stored as IPv4-mapped IPv6
  (`::ffff:1.2.3.4`). This is the source of two distinct bug families that
  have already cost us:
  1. `isIPAddressInRange("::ffff:x.x.x.x", "x.x.x.0/24")` returns `0`. Use
     `buildCIDRFilter()` (parameterised) or `CIDRRangeSQL()` (startup-built
     literal), which map IPv4 CIDRs onto their IPv6-mapped range
     (`x.x.x.0/24` → `::ffff:x.x.x.0 … ::ffff:x.x.x.255`) and emit typed
     `col BETWEEN toIPv6(lo) AND toIPv6(hi)` predicates — never
     `isIPAddressInRange(toString(col), …)`, which stringifies every row
     (532 ms vs 107 ms for 12 prefixes). Single IPs: `col = toIPv6(@ip)`
     (`toIPv6` maps an IPv4 string itself). See `internal/store/alert_queries.go`.
  2. ClickHouse alias scoping — `min(ts) AS ts` shadows the source column
     in the same query's `WHERE`. **Qualify with a table alias** (`f.ts`)
     in any query that aggregates a column it also filters on. We've hit
     this twice now (`/top/port`, `/flows/search`).
- **Channel-based collector pipeline** with backpressure via buffered
  channels. UDP drops at the socket are acceptable for sampled flow data.
- **`LOCAL_AS` enrichment** — at startup, fetches your announced prefixes
  from the RIPEstat API and remaps private ASNs in flows whose src/dst sit
  in those prefixes to the public AS. Refreshed at collector startup only.
- **In-process API cache**, 30s TTL on heavy GETs. No Redis dependency.
- **`chi` router**, stdlib `http.Handler`. Not echo / gin.
- **No ORM** — raw parameterised SQL via `clickhouse.Named()`. Never string
  concatenation for user input.
- **TanStack Query** on the frontend for every API call. Never raw
  `useEffect` for data fetching.
- **"Ledger" UI** — Archivo (variable width, `tabular-nums`), rules instead of
  boxes, colour only for abnormal states (amber = watch, red = incident). A
  narrow cut (`.font-ip`) for IPs/prefixes; monospace only for code/tokens.

## Collector pipeline

```
UDP socket ─► [Reader, 1 goroutine] ─► packets channel (workers×64 buffer)
                                              │
              ┌──────────┬──────────┬─────────┴──────────┬──────────┐
              ▼          ▼          ▼                    ▼          ▼
       [Decoder 1] [Decoder 2]  ...             [Decoder N]
       NetFlow / sFlow parse, no shared state except the template cache
              │          │          │                    │          │
              └──────────┴──────────┴────────┬───────────┴──────────┘
                                             ▼
                              flows channel (batchSize buffer)  
                                             │
                                  [Enricher, 1 goroutine]
                                  - (router_ip, snmp_ifindex) → link tag + direction
                                  - private AS → LOCAL_AS prefix remap
                                             │
                              enriched channel (batchSize buffer)  
                                             │
                                  [BatchWriter, 1 goroutine]
                                  - accumulates up to COLLECTOR_BATCH_SIZE
                                  - flushes on size OR COLLECTOR_FLUSH_INTERVAL
                                  - single batch INSERT to ClickHouse
```

- **Reader**: 1 goroutine per UDP socket. Reads packets, copies router IP,
  pushes to the decoder channel.
- **Decoders**: `COLLECTOR_WORKERS` goroutines (default 4). Parse NetFlow
  v5 (fixed format), v9/IPFIX (template-based) or sFlow v5. Independent
  except for the global template cache (mutex-protected).
- **Enricher**: 1 goroutine. Maps `(router_ip, snmp_index)` → link tag +
  direction. Remaps private AS to `LOCAL_AS` for matching prefixes. Uses
  `sync.RWMutex` — link config reloaded every 60s without blocking
  decoders.
- **BatchWriter**: 1 goroutine. Buffers and writes in batches. Tracks
  metrics (flows received / written, errors, batch timing). One INSERT may be
  in flight while the next batch fills (at most two batches held). Each INSERT
  is bounded by `COLLECTOR_FLUSH_TIMEOUT` (30s) and is **never retried** — a
  failed INSERT can be partially committed across the MVs, so a retry would
  double count; the batch is dropped and counted instead.
- **Flush cadence**: defaults are 15s / 50,000 flows (time-driven at our
  rates). Every INSERT fans out to ~20 MVs and creates ~21 parts, so flushing
  every 5s kept ClickHouse busy merging; 15–30s measured −83% parts.
- **Shutdown** cascades: listeners close → decoders drain → `flows` closes →
  enricher drains and closes `enriched` → writer flushes. `Run` waits for the
  writer, bounded by `COLLECTOR_SHUTDOWN_TIMEOUT` (40s); compose sets
  `stop_grace_period: 60s` so Docker does not SIGKILL mid-flush.
- **Link matching** honours NetFlow/IPFIX field 61 (flowDirection): a record
  is credited to the link on the side it was observed (egress → output
  interface), falling back to the other side. Without it, input+output
  monitors on the same uplink double count every flow.
- **Sampling**: per-record rate field first, then the rate announced in
  v9/IPFIX options data for that sampler / exporter, then
  `COLLECTOR_SAMPLING_OVERRIDE`, then 1. Options records are never emitted as
  flows.
- **Backpressure**: all channels are buffered. If ClickHouse is slow,
  channels fill up and UDP packets are dropped at the socket level —
  acceptable for sampled data.
- **Template cache**: global, keyed by `(router_ip, source_id, template_id)`.
  Templates do not expire by age — a re-send replaces the entry (an age
  expiry could drop a template just before its refresh); a size cap bounds
  the cache. Data sets with no known template are counted per router in
  `asstats_flowsets_dropped_no_template_total`. Cisco IOS-XE with source port 0
  requires a `NOTRACK` iptables rule to bypass conntrack.

### Scaling

- Increase `COLLECTOR_WORKERS` if CPU-bound on decoding.
- Enricher is single-threaded but cheap (map lookup + prefix match).
- BatchWriter is I/O-bound — increase `COLLECTOR_BATCH_SIZE` for higher
  throughput at the cost of latency.
- For 500k+ flows/sec, run multiple collector instances on different ports.

## Directory map

| Path | Purpose |
|---|---|
| `cmd/collector/` | Flow collector entrypoint. Loads config, wires the pipeline, starts the alert engine if `FEATURE_ALERTS=true`. |
| `cmd/api/` | API server entrypoint. |
| `cmd/migrate/` | One-shot schema migrator (embedded `migrations/*.up.sql`, checksum-verified). |
| `internal/collector/netflow/` | NetFlow v5 (fixed), v9/IPFIX (template-based) parsers. |
| `internal/collector/sflow/` | sFlow v5 parser (raw packet header decoding). |
| `internal/collector/enricher/` | `(router_ip, snmp_index)` → link tag + direction. |
| `internal/collector/writer/` | Batch writer to ClickHouse. |
| `internal/api/handler/` | One file per resource: `as.go`, `ip.go`, `links.go`, `top.go`, `search.go`, `auth.go`, `dns.go`, `status.go`, `flows.go`, `alerts.go`, `threats.go`, `features.go`. |
| `internal/api/middleware/` | Auth (OIDC sessions), CSRF (double-submit cookie), rate limiting, in-memory cache, audit. |
| `internal/api/router.go` | `chi` router wiring, middleware stack, security headers, feature-gated route registration. |
| `internal/ripestat/` | RIPEstat API client: fetch AS prefixes (SQL filters are built by `store.CIDRRangeSQL`). |
| `internal/store/store.go` | Interfaces: `FlowWriter`, `FlowReader`, `LinkStore`, `ASNameStore`. |
| `internal/store/clickhouse.go` | Write implementation (batch INSERT). |
| `internal/store/reader.go` | Read implementations — every aggregation query lives here. |
| `internal/store/step.go` | Source-table + bucket-width planner (`linkSeriesPlan`, `asSeriesPlan`, `ipSeriesPlan`, `capacityPlan`) and the exported `*StepSeconds` helpers handlers put in `meta.step_seconds`. |
| `internal/store/names.go` | `asNamesLatest` / `linksLatest`: deduplicated views every name join must use (see bug #15). |
| `internal/store/status.go` | `LiveStatus` for `GET /status/live` (10-min flows_raw scan + configured routers + disk). |
| `internal/store/port_stats.go` | `TopProtocols`, `TopPorts` (read `traffic_by_port` only, Inbound/Outbound = `direction IN ('in','out')`, percent per direction), `ServicePort` (Go mirror of the 000018 view), `PortHistorySince`. See "Port statistics". |
| `internal/store/as_group.go` | `groupedAS` / `groupedASName`: the private-use ASN fold every per-AS list must group on (`TestPerASListsGroupPrivateASNs`). |
| `internal/store/flow_log.go` | `SearchFlowLog` (`port` = either side), `FlowLogTimeSeries`. Every flow-log read goes through `flowLogReadTable` (`flows_log_all`) + `flowLogTimeFilter` — see "Flow log layout" and bug #18. (`SetFlowLogRetention` removed — retention is now in `retention.go`.) |
| `internal/store/threats.go` | Live Threats queries: `ThreatCandidates`, `ThreatBaselines`, `ThreatPeerBaselines`, `ThreatPorts`, `ThreatSeries`. Classification lives in `api/handler/threat_classify.go` (pure, table-tested). See "Live Threats". |
| `internal/store/alerts.go` | Alert rules / alerts / webhooks / audit log CRUD. `AlertDetails` JSON marshaling for `details` column. |
| `internal/store/alert_queries.go` | `Eval*` query helpers used by the alert engine (incl. `EvalDiskUsage`, `EvalLinkCapacity`, `EvalAnomaly`). **`buildCIDRFilter` lives here** — read its docstring before touching any IP filtering code. |
| `internal/store/retention.go` | v2: table→TTL whitelist, `EnsureRetentionPolicies`, `ReconcileRetention` (+ hot/cold tier: `parseTableTTL`, `sweepColdPartitions`), `StorageStats`, `PurgeSoftDeleted`. Runtime retention management (replaces `SetFlowLogRetention`). |
| `internal/store/capacity.go` | v2: `LinksCapacity` (busier-direction util%, complete buckets, forecast on daily p95), `LinkLoadCurve`, `EvalLinkCapacity`. See "Capacity semantics". |
| `internal/store/geo.go` | v2: `TopCountry` (AS-level geo via `as_names.country`). |
| `internal/store/changes.go` | v2: `Movers`, `Talkers`, `LinkPercentiles` (p50/p95/p99). |
| `internal/store/conversations.go` | v2: bidirectional top-talkers (`Conversations`). |
| `internal/store/anomaly.go` | v2: `EvalAnomaly` baseline (median+MAD) + `AnomalyExplain`. |
| `internal/store/heatmap.go` | v2: `TrafficHeatmap` (7×24 dow×hour). |
| `internal/store/tokens.go` | v2: read-only API token CRUD + `LookupAPIToken`. |
| `internal/store/reports.go` | v2: `report_schedules` CRUD. |
| `internal/reports/` | v2: scheduled-report engine — HTML/CSV generation, `net/smtp` sender, scheduler loop (`isDue`). |
| `internal/alerts/engine.go` | Alert engine goroutine — rule loop, alert lifecycle (heartbeat / re-arm / new), top-source enrichment, auto-resolution, default-rule seeding. |
| `internal/alerts/baseline.go` | Baseline mode of the volume rules (`baseline_factor`, p99 of the Live Threats history, `UsualLevel`). |
| `internal/alerts/reconcile.go` | `ReconcileSeededRules`: one-time upgrade of the 3.6 seeded rules still exactly as seeded. |
| `internal/alerts/alert_replay_integration_test.go` | Replay harness: 3.6 vs current engine minute by minute over a synthetic day (benign busy hosts + attacks). |
| `internal/alerts/webhook.go` | Slack / Teams / Discord / generic webhook notifier. |
| `internal/bgp/` | RTBH: `Service` (API side — rails, `bgp_blocks`, expiry / reconcile loop, audit), `Policy` (allow-list, protected, prefix length), `CommandDaemon` (templates → argv, no shell) / `DryRunDaemon`, `Settings` (BGP_* validation), `Client` (collector → `/internal/bgp/blocks`). See "BGP integration". |
| `internal/store/bgp_blocks.go` | `bgp_blocks` CRUD: `InsertBlock`, `SetBlockStatus` (by id, sync mutation), `ListActiveBlocks`, `ListBlocksSince`, `ListBlockHistory`. |
| `scripts/bgp/` | `bird-rtbh.sh`, `frr-rtbh.sh`: daemon helpers for `BGP_*_CMD` (tested in `internal/bgp/scripts_test.go` against stub CLIs). |
| `internal/services/wellknown.go` | IANA protocol + well-known port name resolution (used by Flow Search and Top Ports). |
| `internal/model/` | Shared types: `FlowRecord`, `ASTraffic`, `IPTraffic`, `LiveThreats` / `ThreatRow`, `AlertRule`, etc. |
| `internal/config/` | Env-var config loading. `LoadCollector` and `LoadAPI`. |
| `migrations/` | ClickHouse DDL, numbered 000001–000020 (000007–000013 include feature-gated tables; 000012 `retention_policies`, 000013 `report_schedules`, 000014 `api_tokens`, 000015 hot-table `SimpleAggregateFunction` fix, 000016 `retention_policies.cold_after_days`, 000017 `flows_log` sort key + `flows_log_legacy` + `flows_log_all`, 000018 `traffic_by_port` one row per flow + `traffic_by_port_legacy` + `traffic_by_port_mv`, 000019 `bgp_blocks.dry_run`, 000020 `alert_rules.baseline_factor` + `alerts.reopen_count`). Every released schema level has a `DetectBaseline` probe in `internal/migrate/clickhouse.go` — add one with each migration. Embedded in and applied by `cmd/migrate` (the `migrate` service runs before collector/api), which records each file's SHA-256 — **never edit a released migration**, add a new one. |
| `frontend/src/pages/` | One React page per route in `App.tsx`. |
| `frontend/src/hooks/` | TanStack Query hooks (`useApi.ts`), URL-synced filters (`useFilters.ts`), unit toggle (`useUnit.ts`), chart theme (`useChartColors.ts`), DNS (`useDns.ts`), feature flags (`useFeatures.ts`). |
| `frontend/src/components/charts/` | `TrafficChart` (single in/out), `LinkTrafficChart` (stacked by link), `ASTrafficChart` (stacked by AS) — all Recharts `AreaChart` with `stepAfter`. |
| `frontend/src/components/` | `ExpandableChart`, `ChartModal`, `PTR` (`IPWithPTR`), `ErrorBoundary`. |
| `frontend/src/lib/api.ts` | Typed `fetch` wrapper with CSRF token injection. **Every API call goes through here.** |
| `frontend/src/lib/types.ts` | TypeScript mirror of `internal/model/`. |

## Database schema

All tables in the `asstats` database.

### Core tables (always created)

| Table | Engine | Granularity | TTL |
|---|---|---|---|
| `flows_raw` | `MergeTree` | per flow | 3 days (migration 000004; runtime-managed via `retention_policies`) |
| `traffic_by_as` | `SummingMergeTree` | 5 min | 90 days |
| `traffic_by_as_hourly` | `SummingMergeTree` | 1 hour | 2 years |
| `traffic_by_as_daily` | `SummingMergeTree` | 1 day | 5 years |
| `traffic_by_link` | `SummingMergeTree` | 5 min | 90 days |
| `traffic_by_link_hourly` | `SummingMergeTree` | 1 hour | 2 years |
| `traffic_by_link_daily` | `SummingMergeTree` | 1 day | 5 years |
| `traffic_by_ip` | `SummingMergeTree` | 5 min | 14 days |
| `traffic_by_ip_as` | `SummingMergeTree` | 5 min | 14 days |
| `traffic_by_prefix` | `SummingMergeTree` | 5 min | 30 days |
| `links` | `ReplacingMergeTree` | — | — |
| `as_names` | `ReplacingMergeTree` | — | — |
| `retention_policies` | `ReplacingMergeTree` | — | — |
| `api_tokens` | `ReplacingMergeTree` | — | — |

`retention_policies` (migration 000012) drives runtime TTL management — see
the Retention subsystem note below. `api_tokens` (000014) backs read-only
Bearer API tokens. Both are always created.

Each aggregation table has **two MVs** (`_in_mv`, `_out_mv`) firing on
INSERT to `flows_raw`. The query layer picks the best table based on the
time range: 5-min for ≤90d, hourly for ≤2y, daily beyond.

### Optional tables (feature-gated)

Created by migrations 000007–000013. Used **only** when the corresponding
feature flag is enabled.

| Table | Feature | Purpose |
|---|---|---|
| `flows_log` | `FEATURE_FLOW_SEARCH` | Full per-tuple log, 1-min buckets, 180d default TTL. Sorted by `(toStartOfFifteenMinutes(ts), dst_ip, ts, …)` since 000017; bloom filter on `src_ip` only. **Write-only from Go** — read `flows_log_all`. See "Flow log layout". |
| `flows_log_legacy` | `FEATURE_FLOW_SEARCH` | The pre-000017 `flows_log` (old sort key), kept instead of copied; follows the `flows_log` retention and empties itself. Empty on fresh installs. |
| `flows_log_all` | `FEATURE_FLOW_SEARCH` | `Merge(currentDatabase(), '^flows_log(_legacy)?$')` — the read side of the flow log. |
| `report_schedules` | `FEATURE_REPORTS` | Scheduled HTML+CSV email reports (`ReplacingMergeTree`, soft-delete). Cron goroutine in the collector sends via SMTP. |
| `traffic_by_port` | `FEATURE_PORT_STATS` | Per `(link, direction, protocol, service port, ip_version)`, 5-min, 1y TTL. **One row per flow** since 000018 (`traffic_by_port_mv`); see "Port statistics". |
| `traffic_by_port_legacy` | `FEATURE_PORT_STATS` | The pre-000018 table (every flow counted twice). **Never read**; follows the `traffic_by_port` retention and empties itself. Empty on fresh installs. |
| `traffic_by_dst_1min` | `FEATURE_ALERTS` | `AggregatingMergeTree` keyed by `(ts, dst_ip, protocol)`. Stores `bytes`, `packets`, `flow_count`, `syn_count`, plus HyperLogLog sketches `unique_src_ips` / `unique_src_ports`. 7d TTL. **Used exclusively by the alert engine and Live Threats** — no other on-demand queries. |
| `traffic_by_src_1min` | `FEATURE_ALERTS` | Symmetric for outbound traffic. Used by `volume_out` / `port_scan`. |
| `alert_rules` | `FEATURE_ALERTS` | Configurable detection rules (`ReplacingMergeTree`). 10 default rules seeded on first startup (idempotent — only if the table is empty). |
| `alerts` | `FEATURE_ALERTS` | Triggered instances with full lifecycle (active → ack → resolved). 1y TTL. |
| `webhook_configs` | `FEATURE_ALERTS` | Slack / Teams / Discord / generic destinations (`ReplacingMergeTree`). |
| `audit_log` | `FEATURE_ALERTS` | Compliance trail for all sensitive actions. 1y TTL. |

### Flow log layout (migration 000017)

`flows_log` is `ORDER BY (toStartOfFifteenMinutes(ts), dst_ip, ts, link_tag,
protocol, dst_port, src_ip, src_as, dst_as, src_port, tcp_flags, ip_version)` —
the same column *set* as the 000007 key (router_ip still out of it), so
SummingMergeTree collapses the same rows. Indexes: `src_ip` bloom_filter(0.01)
GRANULARITY 1, minmax on `src_as` / `dst_as`; no `dst_ip` bloom (it is in the
key). Why: under the old `(ts, link_tag, protocol, dst_port, …)` key an IP
filter had only the bloom filter, which skipped ~7 % of granules for a busy
host. Prod, one day (99 M rows), cold caches: busy dst 24 h 2.3 s / 5.2 GiB →
0.32 s / 73 MiB; busy src 24 h 2.6 s / 6.2 GiB → 1.1 s / 1.5 GiB; 15-min
window 0.16 s both; 1.01 GiB → 892 MiB per day.

- **Reads** go to `flows_log_all` (`flowLogReadTable`), a `Merge` table over
  `flows_log` + `flows_log_legacy`, with `flowLogTimeFilter(alias, from, to)`
  (bug #18). `TestFlowLogQueriesCarryBucketPredicate` runs every reader against
  a recording conn; `TestNoDirectFlowLogReads` fails on any `FROM flows_log…`
  in Go sources. **Writes** only go to `flows_log` via `flows_log_mv`.
- **The migration copies nothing.** It creates `flows_log_new`, `EXCHANGE
  TABLES flows_log AND flows_log_new`, renames `flows_log_new` (now the old
  data) to `flows_log_legacy`, creates `flows_log_all`. Verified on 24.3: a
  `TO` MV resolves its target **by name** at insert time, so after the swap
  `flows_log_mv` writes to the new table untouched; EXCHANGE is atomic, so no
  insert block is lost or doubled (`TestIntegrationFlowLogMigration`: 4
  inserters across the migration, `count(flows_raw) = sum(flow_count)` over
  `flows_log_all`; ad hoc, 300 EXCHANGEs under 6000 concurrent INSERTs, exact
  totals). A plain RENAME instead fails every INSERT INTO `flows_raw` until the
  target reappears (bug #19). Guards (`SELECT throwIf(…)`) refuse a re-run;
  recovery notes are in the migration file.
- A column added to `flows_log` must also exist on `flows_log_legacy` (or the
  Merge read breaks) until the legacy table is dropped by a later release.

### Port statistics (migration 000018)

`traffic_by_port` is fed by **one** view, `traffic_by_port_mv`: one row per
`flows_raw` row, never two (bug #21).

- `direction` = `flows_raw.direction` when the flow was credited to a
  configured link (`'in'` entered our network on it, `'out'` left it), and
  `''` when `link_tag = ''` — even if the exporter sent a flowDirection,
  which is router-relative. `''` rows are stored (so `sum(flow_count)` over the
  table equals `count()` of `flows_raw`) but Top Protocols / Top Ports list
  only `IN ('in','out')` (`portDirectionFilter`), the population of the link
  totals; Movers by port is direction-agnostic and includes them.
- `port` = the **service port**: `least(src_port, dst_port)` when both are set
  (client ports are ephemeral), the non-zero one otherwise, `0` for ICMP /
  ICMPv6 (1 / 58) and portless protocols (GRE, ESP…, kept so per-protocol sums
  are exact). Go mirror: `store.ServicePort`; `TestIntegrationPortStats` checks
  the SQL against it on a 405-tuple grid. Top Ports hides port 0 except ICMP.
- Percentages in Top Protocols / Top Ports are of the row's **direction**.
  Drill-down to Flow Search uses `port` (either side), not `dst_port`.
- **Migration**: create `traffic_by_port_new`, `DROP VIEW` the two 000007
  views, `EXCHANGE TABLES`, `RENAME … TO traffic_by_port_legacy`, `CREATE
  MATERIALIZED VIEW traffic_by_port_mv`. The old views are dropped *before*
  the swap, or they would write old-semantics rows into the new table; no
  view ever targets a missing name (bug #19), so no INSERT fails. Each insert
  block lands in the new table exactly once (inserted after the CREATE) or not
  at all; blocks inserted in the milliseconds between DROP and CREATE are in
  neither table (start of the new history). `TestIntegrationPortStatsMigration`:
  4 inserters across the migration, per-inserter suffix property, 0 errors;
  ad hoc, 300 cycles of the DDL sequence under 2,418 concurrent INSERTs, 0
  failures. Guards refuse a re-run; recovery notes in the file.
- **History restarts** at the upgrade (the old rows cannot be split back into
  flows). `PortHistorySince` (system.parts only) returns the start of the new
  history while `traffic_by_port_legacy` still has parts; the handlers put it
  in `meta.port_history_since` when the window (Movers: the prior window)
  reaches back before it, and the pages say so.
- `traffic_by_port_legacy` is a retention follower of `traffic_by_port` and is
  **never read** (`TestNoLegacyPortReads`, `TestPortReadersReadOnlyTheNewTable`).

### Retention subsystem (v2, Module A)

TTLs are no longer hardcoded-only in migrations. `retention_policies` holds one
row per TTL-bearing table (`table_name`, `ttl_column`, `ttl_days`, `enabled`),
seeded once from the migration defaults (idempotent — never clobbers operator
edits). A **reconciler goroutine** in the collector (`RETENTION_RECONCILE_INTERVAL`,
default 15m) applies `ALTER TABLE … MODIFY TTL` when a policy diverges, skipping
absent feature-gated tables. Because that ALTER pins
`materialize_ttl_after_modify = 0`, already-merged parts keep the expiry they
were written with; the reconciler therefore also **drops whole partitions whose
`max_time` is past the retention** (`sweepExpiredPartitions`, one grace day).
Without it, cutting `flows_log` from 180 to 60 days left ~38 GB of closed daily
partitions scheduled to expire months later. A daily **purge goroutine** physically deletes
`deleted=1` rows from the `ReplacingMergeTree` config tables (`alert_rules`,
`webhook_configs`, `hostgroups`, `report_schedules`) older than
`CONFIG_PURGE_DAYS` (default 30). Storage observability is served by
`GET /admin/storage` (`system.parts`/`tables`/`mutations`/`disks`/`storage_policies`:
every disk with path, per-table bytes per disk and hot/cold split) and edited via
`PUT /admin/retention/{table}` (admin + CSRF). All table/column names in dynamic
retention SQL come from the hardcoded whitelist in `store/retention.go`.

**Hot / cold tier** (migration 000016, `retention_policies.cold_after_days`,
0 = off, else `1 ≤ c < ttl_days`; omitted in the PUT body = keep current).
Effective only when the table's live storage policy (`system.tables.storage_policy`)
has a volume named exactly `cold` (`system.storage_policies`); otherwise it is
logged once per process (`warnColdOnce`) and the table is reconciled as if
`c = 0`. The operator configures the policy (README › "Cold storage on a second
disk", `clickhouse/config.d/storage-hot-cold.xml.example`) — its hot volume
**must be named `default`**, or `MODIFY SETTING storage_policy` is refused
("shall contain volumes of the old storage policy"). With a cold volume:

- TTL becomes `<expr> + INTERVAL c DAY TO VOLUME 'cold', <expr> + INTERVAL d DAY`
  (`buildModifyTTLStatement(table, d, c)`, still pinning
  `materialize_ttl_after_modify = 0`). 24.3 (and 26.8, byte for byte) normalises it in
  `create_table_query` to `ts + toIntervalDay(14) TO VOLUME 'cold', ts + toIntervalDay(180)`;
  `parseTableTTL` reads the clause between ` TTL ` (after ` ENGINE = `) and
  ` SETTINGS `, `tableTTL.matches(d, c)` compares both intervals and the move
  clause (no ALTER every cycle, no missed change, `c = 0` removes the clause),
  and `liveTTLDays` returns the **DELETE** interval, never the move one.
- `sweepColdPartitions`: with materialize off, parts written before the move
  clause have no move TTL and never move (same root cause as bug #13). It moves
  whole partitions with `max(max_time) < now() - c` that still have active
  parts on a non-cold disk (`MOVE PARTITION ID … TO VOLUME 'cold'`, same
  partition-id whitelist as DROP), oldest first, **≤ `maxColdMovesPerCycle` (3)
  per table per cycle** (HDD write-burst bound), skipping partitions
  `sweepExpiredPartitions` is about to drop. A failure (cold disk full) stops
  that table's sweep for the cycle, is logged, and other tables continue.
  Re-moving a partition already fully on cold errors in ClickHouse ("All parts
  of partition … are already on volume"), hence the hot-parts filter.
- **Policy adoption** (`adoptColdPolicy`): a table with `0 < c < ttl_days`
  still on ClickHouse's `default` storage policy is switched with
  `ALTER TABLE … MODIFY SETTING storage_policy = '<p>'` (logged) when exactly
  one policy qualifies (`coldPolicyCandidates`: first volume `default` on disk
  `default`, second volume `cold`, plain-identifier name). Zero / several
  candidates or a non-default current policy: left alone, logged once. This is
  how the 000017 `flows_log` (created on `default`) inherits prod's `hot_cold`.
- **Followers** (`retentionFollowers`: `flows_log_legacy` → `flows_log`,
  `traffic_by_port_legacy` → `traffic_by_port`): in the whitelist (so DDL builders accept them) but never seeded, `SetRetentionPolicy`
  refuses them, and the reconciler applies the leader's policy (adoption, TTL,
  expired and cold sweeps) to them — only while they exist **and have active
  parts**, so an empty legacy table is never touched. `StorageStats` marks
  them `legacy` / `retention_follows` (read-only row in Admin › Storage).
  `EnsureRetentionPolicies` seeds a leader without a policy row from its
  follower's live TTL when present (the new table only carries the migration
  default). This is how the 000018 `traffic_by_port` inherits prod's hot_cold
  policy (`adoptColdPolicy`) and the legacy table keeps aging out.
- `disk_usage` and `/status/live` use `fullestDisk` over **all** disks.
- `TestIntegrationColdTier` (needs a policy with a `cold` volume, skipped
  otherwise) checks moves, convergence, no-op cycles and ttl changes.

## API endpoints

All under `/api/v1/`. Common params: `from`, `to`, `period`
(`1h` / `3h` / `6h` / `24h` / `7d` / `30d`), `link`, `links` (multi),
`direction` (`in` / `out`), `ip_version` (`4` / `6`), `limit`, `offset`.

### Always available

| Method | Path | Handler |
|---|---|---|
| `GET` | `/overview` | `Handler.Overview` |
| `GET` | `/status` | `Handler.Status` (windowed; cached) |
| `GET` | `/status/live` | `Handler.StatusLive` — header health dot: last 10 min of `flows_raw` only, per-router `ok` (<120 s) / `stale` / `missing` (configured link, no flow), `disk_used_pct`; cached |
| `GET` | `/top/as` | `Handler.TopAS` |
| `GET` | `/top/ip` | `Handler.TopIP` (scope: `internal` / `external`) |
| `GET` | `/top/prefix` | `Handler.TopPrefix` |
| `GET` | `/as/{asn}` | `Handler.ASDetail` |
| `GET` | `/as/{asn}/peers` | `Handler.ASPeers` (uses `flows_raw`) |
| `GET` | `/as/{asn}/ips` | `Handler.ASTopIPs` (`traffic_by_ip_as`; `scope=external` reads `traffic_by_ip` direction `out` — never `flows_raw`) |
| `GET` | `/ip/{ip}` | `Handler.IPDetail` (1m / 2m resolution on ≤6h via `flows_raw`, see `ipSeriesPlan`; 400 on an unparseable IP; no PTR lookup — `ptr` is always `""`, use `/dns/ptr`) |
| `GET` | `/links` | `Handler.Links` |
| `GET` | `/links/traffic` | `Handler.LinksTraffic` |
| `GET` | `/link/{tag}` | `Handler.LinkDetail` |
| `GET` | `/dns/ptr?ip=X` | `Handler.DNSPtr` |
| `GET` | `/search?q=X` | `Handler.Search` |
| `GET` | `/features` | `Handler.Features` (frontend gate discovery) |
| `POST` | `/admin/links` | `Handler.LinkCreate` (CSRF) |
| `DELETE` | `/admin/links/{tag}` | `Handler.LinkDelete` (CSRF) |
| `GET` | `/top/country` | `Handler.TopCountry` (AS-level geo via `as_names.country`) |
| `GET` | `/links/capacity` | `Handler.LinksCapacity` (util% + forecast) |
| `GET` | `/link/{tag}/load-curve` | `Handler.LinkLoadCurve` (utilization CDF) |
| `GET` | `/changes/movers` | `Handler.Movers` (`dimension=as/prefix/port/country`) |
| `GET` | `/changes/talkers` | `Handler.Talkers` (`dimension=as/ip/prefix`, new/gone) |
| `GET` | `/traffic/heatmap` | `Handler.TrafficHeatmap` (7×24 dow×hour) |
| `GET` | `/admin/storage` | `Handler.StorageStatus` (admin — table sizes, disk, TTL lag) |
| `PUT` | `/admin/retention/{table}` | `Handler.SetRetention` (admin, CSRF) |
| `GET`/`POST`/`DELETE` | `/admin/tokens[/{id}]` | `Handler.*APIToken*` (admin, CSRF — read-only Bearer tokens) |

`/overview` carries totals, active-AS count and links only (no `top_as` /
`top_ip` — nothing rendered them; the dashboard calls `/top/*`).

**Response cache**: every identity-free GET above (overview, top-N, links,
heatmap, changes, status, `/as/*`, `/ip/*`, `/link/*`) sits in the 30 s
`middleware.Cache` group, keyed by request URI. Do not add a route that
reads the caller's identity to that group without adding the principal to
the key.

**`meta.step_seconds`**: every response holding a time series or
percentiles of per-bucket values (`/links/traffic`, `/link/{tag}`,
`/link/{tag}/load-curve`, `/links/capacity`, `/as/{asn}`, `/top/as/traffic`,
`/ip/{ip}`, `/traffic/heatmap`) reports the bucket width the backend
actually used. bytes-per-bucket × 8 / `step_seconds` = bps. Compute it
from the same plan the store query used (`store.LinkSeriesStepSeconds` &
co.) — never re-derive it in the handler or the frontend.

### Feature-gated

Registered only when the corresponding `FEATURE_*` env var is set.

| Method | Path | Feature | Notes |
|---|---|---|---|
| `GET` | `/flows/search` | `FLOW_SEARCH` | `?format=csv` for export, max 100k rows |
| `GET` | `/flows/timeseries` | `FLOW_SEARCH` | drill-down |
| `GET` | `/conversations` | `FLOW_SEARCH` | bidirectional top-talkers (`dim=src_dst_ip/src_dst_as/dst_port_proto`) |
| `GET` | `/top/protocol` | `PORT_STATS` | per (protocol, direction in/out); `meta.port_history_since` while the port history is younger than the window |
| `GET` | `/top/port` | `PORT_STATS` | service ports, `LIMIT … BY direction`; same `meta.port_history_since` (also on `/changes/movers?dimension=port`) |
| `GET` | `/alerts` | `ALERTS` | |
| `GET` | `/alerts/summary` | `ALERTS` | header badge counts |
| `GET` | `/threats/live` | `ALERTS` | Live Threats — local hosts ranked by deviation from their own history (`direction=in/out`, `window`, `limit`, `all=0/1`); 30 s cache group. See "Live Threats" |
| `POST` | `/alerts/{id}/ack` | `ALERTS` | CSRF |
| `POST` | `/alerts/{id}/resolve` | `ALERTS` | CSRF |
| `POST` | `/alerts/{id}/block` | `ALERTS` | CSRF, **admin only**. 409 when BGP is off or fell back to `NoopBlocker`; 422 when the alert has no single host target (link_capacity / anomaly / disk_usage / subnet_flood, zero or 0100::/64 address) |
| `GET` / `POST` / `PUT` / `DELETE` | `/admin/rules[/{id}]` | `ALERTS` | CSRF + admin for writes; `rule_type: "custom"` → 422 |
| `GET` / `POST` / `PUT` / `DELETE` | `/admin/webhooks[/{id}]` | `ALERTS` | CSRF + admin for writes |
| `GET` | `/admin/audit` | `ALERTS` | admin only |
| `GET` | `/anomaly/explain` | `ALERTS` | anomaly contributor breakdown for a target+window |
| `GET`/`POST`/`PUT`/`DELETE` | `/admin/reports[/{id}]` | `REPORTS` | CSRF + admin; `POST /admin/reports/{id}/test` sends now |
| `GET` | `/bgp/status` | `BGP_ENABLED` | session, rails, last RIB reconciliation; `?refresh=1` runs the status command |
| `GET` | `/bgp/blocks`, `/bgp/blocks/history` | `BGP_ENABLED` | active rows / last N (any status) |
| `POST` | `/bgp/blocks` | `BGP_ENABLED` | CSRF, admin; 201 created / 200 already blocked; 400/422/429/502/503 (see BGP integration) |
| `DELETE` | `/bgp/blocks/{ip}?prefix_len=` | `BGP_ENABLED` | CSRF, admin; 404 when nothing active (unless a daemon orphan) |
| `POST` | `/internal/bgp/blocks` (not under `/api/v1`) | `BGP_ENABLED` + `BGP_API_TOKEN` | collector auto-block, bearer token only |

## Auth

- OIDC with PKCE (Authorization Code Flow) via `coreos/go-oidc` +
  `golang.org/x/oauth2`.
- Session cookies: `SameSite=Strict`, `HttpOnly`, `Secure`.
- RBAC: `admin` / `viewer`. The callback grants **admin** to any user whose
  `roles` (or `groups`) claim contains the literal string **`Admin.All`**
  — chosen to map cleanly onto Azure AD App Roles. Anything else → `viewer`.
- CSRF: double-submit cookie on POST/PUT/DELETE (`X-CSRF-Token` header).
- **Existing sessions keep their old role until the user logs out and back
  in** — the role is captured at session creation time, not on every
  request.

## Feature flags

Four independent flags. All default to `false` to keep the base install
lightweight.

| Flag | Enables | New tables | Storage impact (1 Gbps, 1:1000) |
|---|---|---|---|
| `FEATURE_FLOW_SEARCH` | Forensic flow log + Search UI + CSV export + Conversations explorer | `flows_log` | ~250 GB / 6 mo |
| `FEATURE_PORT_STATS` | Top Protocols + Top Ports | `traffic_by_port` | ~5 MB/day |
| `FEATURE_ALERTS` | Alert engine + Alerts dashboard + Live Threats + webhooks + audit log + admin tabs | `traffic_by_dst_1min`, `traffic_by_src_1min`, `alert_rules`, `alerts`, `webhook_configs`, `audit_log` | ~50 GB |
| `FEATURE_REPORTS` | Scheduled HTML+CSV email reports + Admin → Reports tab | `report_schedules` | negligible |

`FEATURE_REPORTS` requires SMTP settings (`SMTP_HOST`, `SMTP_FROM`, and
optionally `SMTP_PORT`/`SMTP_USER`/`SMTP_PASS`/`SMTP_STARTTLS`); the collector
runs the report scheduler goroutine when it is on.

The collector reads `FEATURE_ALERTS` / `FEATURE_REPORTS` to start the alert
engine and report scheduler goroutines. The API server reads all four to gate
route registration. The frontend
calls `/api/v1/features` (cached forever) to conditionally render UI
elements via the `useFeatureFlags()` hook.

## Alert engine

Located in `internal/alerts/`. Runs as a goroutine inside the collector
when `FEATURE_ALERTS=true`.

### Pipeline

1. Auto-resolve alerts whose `last_seen_at` is older than
   `ALERT_STALE_THRESHOLD` (default 5m).
2. Load enabled rules from `alert_rules`.
3. Load webhooks once per cycle.
4. For each rule, run the corresponding `Eval*` against the hot 1-min table,
   over **complete minutes only** (`evalMinutes()` / `alertWindow()`:
   `ts >= @eval_end - window AND ts < @eval_end`, `@eval_end` = start of the
   current minute by the store clock — `SetClock` lets the replay test
   evaluate any past minute with the production SQL; window rounded down to
   whole minutes). The current minute is still being flushed; including it
   and dividing by the full window under-reads rates.
5. Baseline gate (`baseline.go`): violations of a volume rule with a
   `baseline_factor` are dropped when the target is not at least that many
   times its usual level (below).
6. For each violation, from the most recent alert of that rule + target
   (`LatestAlert`, any status), if it was last seen within the **re-arm
   window** `max(cooldown_seconds, ALERT_REARM_WINDOW)` (default 1 h):
   active / acknowledged / muted → heartbeat; resolved → **re-open** it
   (`ReopenAlert`: status active, `metric_value` = peak, `reopen_count + 1`,
   no notification; `auto_block` re-announces) unless the value is ≥
   `RearmEscalation` (2×) its peak, which is a new incident. Otherwise
   enrich with the top 5 source IPs from `flows_raw` and INSERT a new alert.
   An alert this process just inserted / re-opened is heartbeated by id for
   `recentTrust` (2 eval intervals) while the INSERT / mutation lands.
7. Dispatch webhooks asynchronously (new alerts only), with per-webhook
   severity filter.

Alert writes (`InsertAlert`, `UpsertAlertRule`, `UpsertWebhook`,
`WriteAuditLog`) go through `insertRow` (PrepareBatch + Append): an
`INSERT … VALUES` with named parameters renders `cast(5e+08, 'Float64')` /
`toDateTime('…')`, which the fast VALUES parser rejects before falling back
— ~16k `CANNOT_PARSE_INPUT_ASSERTION_FAILED` a day in production
(`TestIntegrationAlertWritesAndLifecycle` checks the counter stays put).

### Baseline mode (`alert_rules.baseline_factor`, migration 000020)

`volume_in` / `volume_out` only (API 400 otherwise; 0 or 1–100). With f > 0
a violation also needs `max(bps / usual_bps, pps / usual_pps) ≥ f` over the
thresholds the rule sets, usual = **p99** of the per-minute history of
`ThreatBaselines` (same time of day ±30 min, 7 days, minutes with traffic),
floors 1 Mb/s / 1000 pps (`alerts.UsualLevel`). The p99, not the Live
Threats p95: with bursts on ~5–8 % of minutes the p95 sometimes falls below
the burst level, and the replay caught a burst firing at 2× the p95. A host
with < `store.BaselineMinSamples` (30) minutes of history is judged on the
threshold alone (a flood on a new host fires). The lookup fails open
(absolute threshold). `details.extra.usual` records factor, samples,
usual / p50 bps and the ratio. Live Threats' `closestRule` counts a
baseline rule as *firing* only under the same condition (p99 ratio), so a
busy host at its usual level is not an incident there either. Known limit:
an attack lasting hours at the same time every day enters its own p99 after
~1 day; keep an absolute rule (the 2 Gb/s critical) for floods.

Seeded factors: "High inbound volume" / "High outbound volume" 2,
"Sustained outbound exfiltration" 3; "Critical inbound volume" (2 Gb/s)
absolute. **Existing installs**: `EnsureDefaultRules` only seeds an empty
table, so the collector also runs `ReconcileSeededRules` at startup: it
upgrades (factor + new description) only the 3.6 seeds still identical in
name, type, thresholds, window, severity, description, scope and factor 0 —
enabled / cooldown / action / webhooks are kept. Idempotent (the new
description no longer matches); an operator-edited rule is never touched.

### Rule types

| Type | Eval method | Source table |
|---|---|---|
| `volume_in` | `EvalVolumeInbound` | `traffic_by_dst_1min` |
| `volume_out` | `EvalVolumeOutbound` | `traffic_by_src_1min` |
| `syn_flood` | `EvalSynFlood` | `traffic_by_dst_1min` (filter on `protocol = 6`) |
| `amplification` | `EvalAmplification` | `traffic_by_dst_1min` (with optional `min_bps` floor reusing `ThresholdBps`) |
| `port_scan` | `EvalPortScan` | `traffic_by_src_1min` `unique_dst_ports` HLL (`PortScanCandidates`, ≤ 200 candidates, threshold × 0.9) → `flows_raw` for those sources: distinct dst ports of **probe** flows (`portScanProbeSQL`: TCP SYN without ACK; flagless TCP / UDP from port ≥ 1024 to port < 1024). Bug #24 |
| `icmp_flood` | `EvalProtocolFlood(proto=1)` | `traffic_by_dst_1min` |
| `udp_flood` | `EvalProtocolFlood(proto=17)` | `traffic_by_dst_1min` |
| `connection_flood` | `EvalConnectionFlood` | `traffic_by_dst_1min` (`flow_count` per dst) |
| `disk_usage` | `EvalDiskUsage` | `system.disks` (percent threshold via `ThresholdCount`) |
| `link_capacity` | `EvalLinkCapacity` | `traffic_by_link_hourly` vs `links.capacity_mbps` (util% via `ThresholdCount`): p95 of the **busier direction** over complete hours |
| `anomaly` | `EvalAnomaly` | `traffic_by_link_hourly` — median + k·MAD over same-hour-of-week history; k = `ThresholdCount`/10; target link via `TargetFilter` |
| `custom` | not implemented — rejected with 422 by the rules API | — |

Rules that target a link (not an IP) carry the link tag via
`AlertViolation.TargetLabel` — the alerts table dedups on the IPv6 `target_ip`
column, so these report the single worst link per cycle. `disk_usage` /
`link_capacity` / `anomaly` reuse `ThresholdCount` as their numeric threshold
(percent, percent, k×10 respectively) to avoid an `alert_rules` migration.

### Re-arm, cooldown & cleanup

- The DB is the source of truth (`LatestAlert`); `cooldown_seconds` is the
  rule's re-arm window (floored by `ALERT_REARM_WINDOW`, default 1 h; the
  UI labels it "Re-arm"). Auto-resolution stays `ALERT_STALE_THRESHOLD`.
- In-memory `recent` map (rule|target → alert id just inserted / re-opened)
  only bridges the async write; `cooldownCleanupLoop` runs every 5 min and
  prunes entries older than 1 hour so the map cannot grow unbounded.

### Top sources enrichment

After a violation, the engine queries `flows_raw` filtered by destination
IP + window to extract the top 5 source IPs by bytes. Stored in
`alert.details.top_sources` so triage doesn't need a separate flow search.
The query uses an explicit IPv6-mapped probe so it returns correct results
for IPv4 destinations too.

### Local prefix filter

Rules only fire on IPs in the local AS prefixes (loaded from RIPEstat at
startup). A rule's `target_filter` field can override this with a
rule-specific CIDR. The filter goes through `buildCIDRFilter` in
`alert_queries.go`, which **must** normalise IPv4 CIDRs to their
IPv6-mapped form (`x.x.x.0/24` → `::ffff:x.x.x.0/120`) — see the long
docstring there. This bug used to silently drop every IPv4 row from every
alert evaluation; do not undo it.

### Default rules

Seeded on first startup if `alert_rules` is empty. **10 rules** covering:

- Inbound volume — warning at 500 Mbps and 2× usual, critical at 2 Gbps (absolute)
- TCP SYN flood (50k SYN/s)
- Connection-rate flood (200k flows / 60s — Slowloris signature)
- Reflection / amplification (10k unique srcs **AND** ≥100 Mbps sustained)
- ICMP flood (20k pps)
- UDP flood (100k pps)
- Port scan from internal host (1k distinct probed dst ports)
- High outbound volume (500 Mbps and 2× usual)
- Sustained outbound exfiltration (50 Mbps over 5 min and 3× usual, "info" severity)

The full list with thresholds lives in `EnsureDefaultRules` in
`internal/alerts/engine.go`.

### BGP integration

RTBH (RFC 7999). **The API owns everything**: `bgp.Service` (built in
`api.NewRouter` when `BGP_ENABLED=true`, loop bound to the server ctx) is the
only writer of `bgp_blocks` and the only caller of the daemon. User docs:
`docs/bgp-blackhole.md`; review: `docs/design-review/bgp-review-2026-10.md`.

- **Entry points**, all → `Service.Block(BlockRequest)`: `POST /bgp/blocks`
  (source `manual`), `POST /alerts/{id}/block` (`alert`, with alert id / rule /
  metrics; handler in `handler/bgp.go`, 409 BGP off, 422 non-host target),
  `POST /internal/bgp/blocks` (`auto_block`, by `engine`): the collector's
  `bgp.Client`, `Authorization: Bearer $BGP_API_TOKEN` (≥ 32 chars, constant
  time), registered outside `/api/v1` only when the token is set, so no
  session / CSRF; nginx does not proxy it. The alert engine never writes
  `bgp_blocks` and only asks for host rule types (`autoBlockableRule`).
- **Rails** (`Policy.Check` + `Limits`, every source): allow-list
  (`BGP_ALLOWED_PREFIXES`, else LOCAL_AS prefixes, else refuse all), protected
  (peer, next hops, router id, `BGP_PROTECTED_PREFIXES`), reserved ranges,
  min prefix length (32/128 default, floors 24/48), max active (50), auto per
  hour (10, seeded from `ListBlocksSince` so restarts don't reset it),
  duration 1 min–`BGP_MAX_DURATION`. Errors: `ErrInvalidRequest` 400,
  `*PolicyError` 422, `*LimitError` 429, `ErrNotFound` 404, `ErrNotReady` 503,
  `*CommandError` 502 (`writeBGPError`; command output never sent to clients).
- **Commands**: `ParseTemplate` splits once (POSIX quoting), refuses unquoted
  shell operators and unknown `{placeholders}`; `runCommand` = exec, no shell,
  env `PATH`/`LANG=C`/`ASSTATS_BGP_*` only, timeout + `WaitDelay`, 64 KiB
  output. Never reintroduce `sh -c` or pass `os.Environ()`.
- **State machine**: insert row (`active`) → announce → on error mark `failed`.
  Withdraw: daemon first, then `SetBlockStatus` (by id, all legacy duplicate
  ids); a store failure keeps the entry as `withdrawn` and retries (never
  re-announced). `Reconcile` (every 15 s): load until ClickHouse answers
  (blocks refused meanwhile), expire, retry pending; every
  `BGP_RECONCILE_INTERVAL`: status + RIB (`BGP_ROUTES_CMD`) — missing
  re-announced, orphans reported / withdrawable, never auto-withdrawn; a
  session back to established re-pushes all. Dry-run rows (`dry_run=1`) go to
  `DryRunDaemon` forever, even after dry run is switched off.
- **Single API instance assumed** (like the in-memory sessions): two API
  replicas with BGP enabled would both reconcile.
- `bgp_blocks` stays a MergeTree + synchronous `ALTER UPDATE` keyed by id
  (single serialised writer, a few rows a day): no FINAL needed.
- Label-targeted alerts (link tags) are stored under synthetic `0100::/64`
  addresses (`labelTargetIP` in the engine) — never blackhole those (reserved
  in `Policy` too).
- Tests: `internal/bgp/*_test.go` (fake daemon / store / clock),
  `TestIntegrationBGPBlackholing` in `internal/api` (real router, fake daemon
  script, own scratch DB, restart + dry run) on 24.3 and 26.8.

## Live Threats

`GET /threats/live` (handler `threats.go`, classifier `threat_classify.go`,
queries `store/threats.go`). Ranks **local** hosts by how far they are from
their own usual traffic, not by volume. `direction=in` reads
`traffic_by_dst_1min` (local destinations), `out` reads `traffic_by_src_1min`
(local sources). Window = complete minutes (`alertWindow`), 60–3600 s.

**Queries** (all `clickhouse.Named`, typed IPv6 membership via
`ipsInList` → `col IN (SELECT toIPv6(arrayJoin(@ips)))`, table-aliased, later
ones pinned to the anchor minute the first returns):

1. `ThreatCandidates` — one pass over the window: per (host, minute), then
   per host (HLL states carried with `uniqMergeState`, so window-wide peers
   *and* the max per-minute peers come out of one scan), then
   `row_number()` four ways. Candidates = top 50 bytes ∪ top 50 packets ∪
   top 25 peers ∪ top 25 SYN, local prefixes only (`buildCIDRFilter`).
2. `ThreatBaselines` — candidates only; the 60 minutes centred on the same
   time of day, 1..7 days ago, as 7 OR-ed `ts` ranges (key pruning).
   Per-minute p50/p95 of bytes and packets over minutes **with traffic**
   (`samples` = minutes found; 0 = new host). Does **not** read the HLL
   column: it tripled the bytes read (10k hosts: 283 MiB vs ~1 GB).
3. `ThreatPeerBaselines` — usual per-minute peers, only for ≤ 25 inbound
   hosts with ≥ 1000 peers and history (the only use: `many_sources`).
4. `ThreatPorts` — `flows_raw`, ≤ 25 hosts that already look abnormal (or
   have the amplification shape), window ≤ 15 min: remote port breakdown
   (source port in, destination port out), shares exact (window functions
   before `LIMIT 6 BY`), TCP/25 always kept for `smtp_burst`.
5. `ThreatSeries` — the 60 complete minutes before the anchor, rows returned
   only, zero-filled in Go.

Measured on a throwaway 24.3 with 10k local hosts × 2 protocols × (last hour
+ 7 × 1 h history) = 9.6 M hot rows: full inbound request ~210 ms
(candidates 90 ms / 12 MiB, baseline 40 ms / 283 MiB, peers 18 ms / 70 MiB,
ports 7 ms, series 5–8 ms). A prototype of candidates + baseline measured
on prod (148 candidates) took 178 ms and read 103 MiB.

**"Normal"** = at its usual level: ratio = max(bps / max(p95_bps, 1 Mb/s),
pps / max(p95_pps, 1000)); a host is *above usual* when ratio > 1 or it has
no history. **Levels** (constants in `threat_classify.go`):

- `incident`: a non-`info` rule at ≥ 100 %, OR an attack signature
  (syn/udp/icmp flood, amplification, port_scan, host_sweep, outbound_flood)
  AND ratio ≥ 10.
- `watch`: a rule at ≥ 50 % **and above usual**, OR any signature, OR
  (history AND ratio ≥ 3 AND (≥ 20 Mb/s OR ≥ 5000 pps)), OR (no history AND
  (≥ 100 Mb/s OR ≥ 20 000 pps)).
- `normal` otherwise; counted (`normal`) but listed only with `all=1`.

The "above usual" gate on rule proximity and the non-`info` restriction are
deliberate: a busy server at 63 % of the 500 Mb/s rule every day, or any
server above the 50 Mb/s *info* exfiltration rule, would otherwise be
permanently flagged (`TestClassifyProdSnapshot` pins real prod rows that
must stay normal). Score (ordering inside a level) = max(ratio,
rule_pct / 10) + 2 per signature.

**Signatures**: in — `syn_flood` (≥ 1000 SYN/s and ≥ 50 % of TCP pps),
`udp_flood` (≥ 80 % UDP, ≥ 10k pps), `icmp_flood` (≥ 80 % ICMP/ICMPv6,
≥ 2000 pps), `amplification` (one reflector UDP source port — 0, 19, 53,
111, 123, 137, 161, 389, 1900, 3283, 3702, 5353, 11211, 27015 — ≥ 50 % of
bytes, ≥ 50 peers, avg packet ≥ 400 B; `top_ports`), `small_packets`
(< 100 B avg, ≥ 5000 pps), `many_sources` (≥ 1000 peers and max per-minute
peers ≥ 10 × usual). Out — `port_scan` (≥ 500 peer ports), `host_sweep`
(≥ 1000 peers, < 200 B), `smtp_burst` (≥ 100 flows/min to TCP/25;
`top_ports`), `outbound_flood` (ratio ≥ 5, ≥ 10k pps), `small_packets`.
Shape-only signatures (udp/icmp flood, amplification, small_packets,
host_sweep) require *above usual* — an authoritative DNS server is all small
UDP packets every day.

**Rule proximity** (`closestRule`): in → volume_in, syn_flood, icmp_flood /
udp_flood (per-protocol pps — the hot table is keyed by protocol),
amplification (the farther of peers / bps), connection_flood (flows/s × rule
window), subnet_flood (per-host lower bound); out → volume_out, port_scan.
Peers are over the Live Threats window (may exceed the rule's), so proximity
over-estimates, never under-estimates.

`since` = first minute of the run of minutes above the usual p95 (floors
applied) that reaches the last complete minute; `""` if that minute is not
above. Integration test: `TestIntegrationLiveThreats` (handler package)
seeds busy-steady hosts, a ×20 jump, a SYN flood, an NTP amplification and
an outbound port scan and checks every level end to end.

## Time-series resolution rules

Every series / percentile query picks its **source table first**, then
clamps the bucket width to that table's granularity (`store/step.go`):

| Plan | Raw `flows_raw` when | Otherwise |
|---|---|---|
| `linkSeriesPlan` (link series, LinksTrafficSeries, LinkPercentiles, load curve) | `to - from ≤ 3h` | `pickLinkTable` (5-min ≤90d, hourly ≤2y, daily) |
| `asSeriesPlan` (AS series, ASP95, LinkASTimeSeries, TopASTrafficSeries) | `≤ 3h` | `pickASTable` |
| `ipSeriesPlan` (IPTimeSeries) | `≤ 6h` (IP-filtered raw scans are cheap) | `traffic_by_ip` (5-min only) |
| `capacityPlan` (LinksCapacity) | never | `pickLinkTable` |

Bucket = `max(autoStep(), tableGranularity(table))`. `autoStep()` alone is
display-driven (1 min ≤3h, 2 min ≤6h, 5 min ≤36h, 15 / 30 min, 1 h) and
used to be finer than the rows it bucketed: 120 s buckets over 5-min rows
(3h–6h windows) read p95 2.5x too high, and the Capacity page's 60 s buckets
over 5-min rows read utilisation up to 5x too high. The width used is
returned as `meta.step_seconds`; `TestSeriesPlansNeverFinerThanSource`
pins the invariant.

## Capacity semantics

A link is **full duplex**: utilisation is the busier direction against
capacity, never in+out. `LinksCapacity` returns per-direction
`current_*` (last **complete** bucket — the one containing `to`/now is
still filling), `peak_*`, `p95_*`; `current_bps` / `p95_bps` are
`max(in, out)`, `utilization_pct = p95_bps / capacity`,
`peak_utilization_pct = max(peak_in, peak_out) / capacity`. Only whole
buckets inside the window are used (`completeBuckets`). The trend /
forecast is a linear regression over the **daily p95 of max(in, out)**
from the 5-min rollup (last 90 whole days), x = real day numbers.
`/link/{tag}` `utilization_pct`, the load curve and the `link_capacity`
alert rule use the same busier-direction definition.

## Code conventions

- **Go**: `golangci-lint` with `errcheck` — handle all errors. `_ =` only
  for intentional discards.
- **Go**: no `any` types in handler responses — use concrete model types.
- **Go**: security headers set in router middleware, not per-handler.
- **Frontend**: no `any` types — typed API client with generics.
- **Frontend**: URL-synced filters (search params), not component state.
- **Frontend**: all data fetching via TanStack Query hooks, never raw
  `useEffect` for API calls.
- **Frontend**: feature-gated UI uses `useFeatureFlags()` with safe
  defaults while loading.
- **Frontend**: cards use the `<CardHeader> + <CardContent>` pattern from
  `components/ui/card.tsx` — packing the title into a bare `<CardContent>`
  produces inconsistent vertical baselines vs. the rest of the app.
- **SQL**: always `clickhouse.Named()` for parameters, never string
  concatenation.
- **SQL**: in any query that aggregates a column it also filters on, use a
  table alias and qualify column references in `WHERE` / `GROUP BY` —
  ClickHouse alias scoping bites otherwise.
- **SQL**: aggregate (and rank / LIMIT) first, then join names onto the
  result — and only ever join `asNamesLatest` / `linksLatest`, never the raw
  `as_names` / `links` tables (bug #15).
- **SQL**: group and filter on typed `IPv6` columns; `toString()` only the
  rows you return (after `LIMIT`).
- **SQL**: every list with one row per AS groups on `groupedAS(col)` and
  labels with `groupedASName` (private-use ASNs → one `PrivateASGroup` row,
  sentinel 4294967295); add new readers to `TestPerASListsGroupPrivateASNs`.
  The frontend renders AS identities with `ASRef` / `asPath` (the sentinel is
  a non-clickable "Private / Internal") and never builds a flow filter from it.
  Per-IP / per-prefix rows keep the address's real origin ASN.
- **Tests**: `ASSTATS_IT_CLICKHOUSE=host:9000 go test ./internal/store
  ./internal/api/handler -run Integration` runs every read / alert query
  (and Live Threats end to end) against a throwaway ClickHouse with the
  migrations applied (skipped when unset). `TestIntegrationFlowLogMigration` /
  `…FreshInstall` build their own scratch databases with the real migration
  runner (`scratchSchema`); the former needs exactly one hot/cold storage
  policy on the server (see `retention_integration_test.go` for the
  `docker run` line) and skips otherwise. CI's `go-integration` job runs
  all of this on ClickHouse 24.3 and 26.8. Apply migrations by hand with
  `clickhouse-client --database asstats`: the 000017 / 000018 guards use
  `currentDatabase()`.
- **Docker**: `FROM --platform=$BUILDPLATFORM` + `TARGETARCH` for
  cross-compilation.

## Running locally

```bash
make docker-up           # ClickHouse only
make run-collector       # Terminal 1
make run-api             # Terminal 2
cd frontend && npm run dev   # Terminal 3
```

`make ci` runs everything CI runs locally (Go lint + test + build,
frontend lint + typecheck + build).

## CI / CD

- **CI** (`.github/workflows/ci.yml`): lint, test, build, Docker push to
  GHCR on `main`.
- **Release** (`.github/workflows/release.yml`): on `v*` tag push or
  manual dispatch → Go binaries (4 platforms), Docker multi-arch
  (amd64 + arm64), GitHub Release with auto-generated changelog.
- **Dependabot**: Go, npm, Docker, GitHub Actions (weekly).
- **Images**: `ghcr.io/nextmap-io/as-stats-{collector,api,frontend}`,
  tagged with both `latest` and the semver version.

## Bugs that have already cost us — please don't reintroduce

1. **`isIPAddressInRange` with IPv4 CIDR vs IPv4-mapped IPv6 column**.
   Fixed in `buildCIDRFilter`. Symptom: alerts never fire on IPv4 traffic
   even though the table has data. Always build CIDR predicates through
   `cidrBounds()` / `normalizeCIDRForIPv6Column()` (typed ranges, see the
   Key decisions note); `TestIntegrationCIDRRangeMatchesIsIPAddressInRange`
   and `TestCIDRBounds` pin the mapping.

2. **ClickHouse alias shadowing the source column in `WHERE`**. Fixed in
   `SearchFlowLog`, `TopProtocols`, `TopPorts`. Symptom: HTTP 500 with
   `Aggregate function min(ts) AS ts is found in WHERE`. Always alias the
   table (`FROM flows_log f`) and qualify (`f.ts`) in any query that
   `SELECT min(ts) AS ts ... WHERE ts >= ...`.

3. **Cooldown map memory leak**. Fixed by `cooldownCleanupLoop`. Don't
   re-add unbounded maps keyed by attacker IPs.

4. **Empty title row in `CardContent` instead of `CardHeader`**. Causes
   misaligned baselines vs. cards built the standard way. Use
   `<CardHeader> <CardTitle>` for the title row.

5. **Bitwise `&` in ClickHouse SQL**. ClickHouse has no infix bitwise
   operator — use `bitAnd(a, b)`. Fixed in migration 000008.

6. **`SetFlowLogRetention` scans `count()` into `*uint8`**. FIXED (migration
   000012 / Module A) — `SetFlowLogRetention` was removed entirely; retention
   is now DB-backed via `retention_policies` + the reconciler (all `count()`
   scans use `uint64`). See the Retention subsystem note below.

7. **`ALTER TABLE … MODIFY TTL` without `SETTINGS materialize_ttl_after_modify = 0`**.
   ClickHouse defaults the setting to `1`, so the statement enqueues a
   `MATERIALIZE TTL` mutation that **rewrites every part of the table**. On
   `flows_log` / `flows_raw` that is tens of GB of write amplification on the
   very disk you are usually trying to free — it turned a full disk into a 20h
   ingestion outage. `buildModifyTTLStatement` in `store/retention.go` pins the
   setting; a test asserts it. Never build a `MODIFY TTL` by hand without it.

8. **Plain `UInt64` counters in an `AggregatingMergeTree`**. That engine only
   combines `AggregateFunction` / `SimpleAggregateFunction` columns; for an
   ordinary column it keeps **one arbitrary row's value** and silently drops
   the rest. `traffic_by_dst_1min` / `traffic_by_src_1min` shipped this way and
   under-reported by ~4x, so every alert rule fired at ~4x its configured
   threshold. Fixed in migration 000015 —
   `SimpleAggregateFunction(sum, UInt64)`, which is metadata-only (no mutation).
   Any new counter column on these tables must use it.

9. **A decoder loop that advances by a router-supplied length**. A NetFlow v9 /
   IPFIX template whose fields are all zero-length gives `TotalLen == 0`, and
   `decodeDataSet` then loops forever allocating a `FlowRecord` per iteration —
   reachable from a single malformed or spoofed UDP packet. Templates with
   `TotalLen <= 0` are rejected at parse time and both decode loops are guarded.
   Apply the same reasoning to any new length-driven parser.

10. **Trusting exporter timestamps.** `flows_raw.timestamp` is *both* the
    partition key and the TTL column, so a router with a broken clock creates
    partitions that never expire. The enricher clamps anything outside
    `[-24h, +5m]` to receive time and counts it
    (`asstats_timestamps_clamped_total`).

11. **Left-most `X-Forwarded-For` as a trust boundary.** The shipped nginx uses
    `$proxy_add_x_forwarded_for`, which *appends* to whatever the client sent,
    so the left-most entry is attacker-controlled. Honour forwarded headers only
    from a trusted peer and take the **right-most** entry (see
    `middleware/ratelimit.go`).

12. **Mounting CSRF only on write-only route groups.** `setCSRFCookie` runs on
    *safe* methods, so if no GET passes through a CSRF-wrapped route the client
    never receives the cookie and every write 403s. CSRF is mounted once on the
    whole `/api/v1` tree — keep it there.

13. **Assuming a lowered TTL frees old data.** With
    `materialize_ttl_after_modify = 0` (entry 7), parts already merged keep the
    `delete_ttl_info_max` computed under the old retention, and closed
    partitions are never merged again — so they only expire at the *old*
    deadline. `sweepExpiredPartitions` drops them; do not remove it on the
    assumption that "TTL will get there".

14. **Healthchecking a UDP flow port with `nc -zu`.** The probe is an empty
    datagram the decoder rejects, so it logged ~5,700 decode errors a day and
    drowned `asstats_decode_errors_total`. The collector serves `/healthz` on
    its metrics listener instead.

15. **Plain `LEFT JOIN` on a `ReplacingMergeTree` inflates sums.** `as_names`
    and `links` keep every inserted version until a background merge; a
    plain (ALL) join matches each one, duplicating the fact rows. An AS
    rename doubled that AS's bytes and a link colour edit doubled the link's
    traffic until the merge (reproduced on 24.3). Join the deduplicated
    `asNamesLatest` / `linksLatest` views (`store/names.go`) after
    aggregating; `TestIntegrationNameJoinsDoNotInflate` pins it.

16. **Bucketing finer than the source table.** Putting 5-minute rollup rows
    into 60 s / 120 s buckets over-reads every bps and percentile by
    granularity/step (up to 5x on Capacity). Always go through a
    `seriesPlan` (`store/step.go`) and report its width as
    `meta.step_seconds`.

17. **Reading the bucket that is still filling.** The current minute of the
    hot tables and the current 5-min / hourly rollup row are partial; dividing
    them by the full width under-reads. Alert rules use complete minutes
    (`completeMinutes`), capacity "current" uses the last complete bucket
    (`completeBuckets`), `link_capacity` complete hours.

18. **Relying on ClickHouse to derive a function-of-`ts` key range.** With
    `flows_log` keyed on `toStartOfFifteenMinutes(ts)` first, 24.3 only
    partially derives that range from `ts >= a AND ts < b`: a 15-minute window
    read ~80 % of a production day, and 99 % of a synthetic one (19.8 M of
    20 M rows vs 213 k with the predicate) — and the `dst_ip` key column behind
    it is useless too. Every flow-log query must also carry
    `toStartOfFifteenMinutes(t.ts) BETWEEN toStartOfFifteenMinutes(@from) AND
    toStartOfFifteenMinutes(@to_incl)`: use `flowLogTimeFilter`, never write
    the time predicate by hand (two unit tests enforce it).

19. **Renaming the target of a `TO` materialised view.** The MV resolves its
    target by name on every insert; between `RENAME flows_log TO x` and the
    re-creation of `flows_log`, every `INSERT INTO flows_raw` fails ("Target
    table … of view … doesn't exists") — and the collector never retries a
    batch. Swap tables with `EXCHANGE TABLES` (atomic) instead.

20. **Binding a `time.Time` positionally where sub-second order matters.**
    clickhouse-go formats a positional / named `time.Time` as
    `toDateTime('…')` — whole seconds. `schema_migrations` events of one
    migration tied, and across unmerged parts the `applying` row could sort
    last, so a completed migration read as dirty. `Append` now binds a
    nanosecond string into `toDateTime64(?, 9, 'UTC')`, and `History`
    tie-breaks `applying` first.

21. **Two views writing the same entity of one flow.** 000007 fed
    `traffic_by_port` from `_in_mv` (direction `'in'`, `dst_port`) and `_out_mv`
    (direction `'out'`, `src_port`) over the *same* `flows_raw` rows: every flow
    was stored twice, "direction" only said which port column had been copied,
    Top Protocols showed in == out by construction (~35 GiB a year of it in
    production), and Top Ports "in" listed every download's ephemeral client
    port. Fixed by 000018 (one view, `flows_raw.direction`, service port). A
    per-direction view pair is only valid when the two write different
    entities (src AS vs dst AS); test any new rollup with
    `sum(flow_count) == count(flows_raw)`.

22. **A per-AS list that skips the private-use fold.** The IP detail page's
    "Traffic by AS" (and AS peers, the anomaly explanation) listed 64512,
    65001, 4200000001… one by one, nameless, while every other view showed one
    "Private / Internal" row. Group on `groupedAS` (code conventions above).

23. **A machine-to-machine call routed through the browser API.** The
    collector's auto-block posted to `/api/v1/bgp/blocks` without session or
    CSRF token: every request was a 403 and auto-block never worked (failures
    were only logged). Collector → API calls
    use a dedicated route outside `/api/v1` with a shared bearer token
    (`/internal/bgp/blocks`, `BGP_API_TOKEN`) and a metric for refusals.
    Relatedly, every BGP block path must go through `bgp.Service` (rails +
    `bgp_blocks` row before the route): `/alerts/{id}/block` once announced
    without a row, and a restart then left the route announced forever.
24. **Counting every destination port of a local source as a port scan.** 3.6
    `port_scan` read `traffic_by_src_1min.unique_dst_ports`: a server replying
    to clients sends to their ephemeral ports, so busy servers "scanned" 1000+
    ports a minute — 1,938 alerts on 33 servers in 10 days, none a scan. A
    scan is *probes*: the rule now confirms its hot-table candidates on
    `flows_raw` and counts only TCP SYN-without-ACK flows (flagless TCP / UDP:
    port ≥ 1024 → port < 1024). Never count replies; the replay test
    (`TestIntegrationAlertReplay`) has responders on TCP 443 / 8443 / UDP 53
    and a NAT gateway with 8 % unanswered SYNs that must stay quiet.

25. **Flapping alerts.** Alerts auto-resolve after 5 min without a violation;
    a busy host bursting above a static threshold a few minutes an hour got a
    new alert (and notification) every ~20 min all day (5,150 alerts in 10
    days, none acknowledged), and acknowledging a running incident spawned a
    duplicate. The engine now re-opens the last alert of the rule + target
    within the re-arm window (`LatestAlert` / `ReopenAlert`, `reopen_count`),
    heartbeats acknowledged ones, and only escalation (≥ 2× the peak) makes a
    new incident. Static thresholds on hosts whose normal traffic is above
    them are the other half: use `baseline_factor` (2× / 3× usual on the
    seeded volume rules).

## Operational notes — ClickHouse

- **Two server versions are supported: 24.3 LTS (compose default, production
  today) and 26.8 LTS.** `CLICKHOUSE_IMAGE` selects the image, and CI
  (`go-integration`) runs the integration suite on both. Code must work on
  both until 24.3 is dropped. Runbook and rehearsal results:
  `docs/upgrades/clickhouse-26.8.md`. 26.8 differences that bit or could bite:
  - `async_insert` defaults to 1 (26.2). The store and migrator connections
    pin `async_insert = 0`; keep it on any new connection.
  - A TTL-drop merge leaves an **empty part** (`rows = 0`,
    `min_time = 1970`). Any `system.parts` read that infers "has data" or a
    min/max time must filter `rows > 0` (StorageStats, `PortHistorySince`,
    `tablesWithActiveParts`, the sweeps).
  - `EXPLAIN` draws a tree (`│ ├──`) for multi-child steps; strip it before
    matching step names.
  - `create_table_query` TTL text is byte-identical to 24.3, so
    `parseTableTTL` works unchanged.
  - 26.8 writes part formats 24.3 cannot read: a rollback needs a snapshot or
    `merge-tree-downgrade-compat.xml.example`, plus
    `scripts/clickhouse/drop-system-logs.sh`.
  - The amd64 build needs AVX2 (26.6+).

- **Never `rm` ClickHouse's own log files** (`/var/log/clickhouse-server/*.log`).
  Doing so during an incident left the Poco file logger throwing on every write,
  and **TTL merges stopped running for six days** — every table silently blew
  past its retention while regular merges kept working, so nothing looked wrong.
  Only a server restart recovers it. Cap the logs with a TTL / logrotate instead.
- **A full disk deadlocks retention**: with no free space ClickHouse cannot
  merge, and TTL eviction happens *during* merges — so the disk can never drain
  itself. Recover by `DROP PARTITION` (unlinks immediately, needs no scratch),
  never by `OPTIMIZE` or a TTL change.
- `system.*_log` tables live on the same volume as flow data and are unbounded
  by default; they reached ~12 GiB here. They carry a 3-day TTL in production.
  `clickhouse/config.d/system-logs-ttl.xml` also caps the logs that are on by
  default from 24.8 (`text_log` at information level, `error_log`,
  `query_metric_log`, `background_schedule_pool_log`). An idle 26.8 writes
  ~1.3 M trace rows a day to `text_log` otherwise.
