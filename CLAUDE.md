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
  this pattern).
- **IPv6 columns everywhere** — IPv4 is stored as IPv4-mapped IPv6
  (`::ffff:1.2.3.4`). This is the source of two distinct bug families that
  have already cost us:
  1. `isIPAddressInRange("::ffff:x.x.x.x", "x.x.x.0/24")` returns `0`. Use
     `buildCIDRFilter()` which normalises IPv4 CIDRs to their IPv6-mapped
     equivalent (`x.x.x.0/24` → `::ffff:x.x.x.0/120`). See `internal/store/alert_queries.go`.
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
- **Dark-first** NOC-inspired UI, JetBrains Mono with `tabular-nums`.

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
                              flows channel (batchSize×4 buffer)
                                             │
                                  [Enricher, 1 goroutine]
                                  - (router_ip, snmp_ifindex) → link tag + direction
                                  - private AS → LOCAL_AS prefix remap
                                             │
                              enriched channel (batchSize×4 buffer)
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
  metrics (flows received / written, errors, batch timing).
- **Backpressure**: all channels are buffered. If ClickHouse is slow,
  channels fill up and UDP packets are dropped at the socket level —
  acceptable for sampled data.
- **Template cache**: global, keyed by `(router_ip, source_id, template_id)`.
  Templates expire after 30 minutes. Cisco IOS-XE with source port 0
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
| `internal/collector/netflow/` | NetFlow v5 (fixed), v9/IPFIX (template-based) parsers. |
| `internal/collector/sflow/` | sFlow v5 parser (raw packet header decoding). |
| `internal/collector/enricher/` | `(router_ip, snmp_index)` → link tag + direction. |
| `internal/collector/writer/` | Batch writer to ClickHouse. |
| `internal/api/handler/` | One file per resource: `as.go`, `ip.go`, `links.go`, `top.go`, `search.go`, `auth.go`, `dns.go`, `status.go`, `flows.go`, `alerts.go`, `threats.go`, `features.go`. |
| `internal/api/middleware/` | Auth (OIDC sessions), CSRF (double-submit cookie), rate limiting, in-memory cache, audit. |
| `internal/api/router.go` | `chi` router wiring, middleware stack, security headers, feature-gated route registration. |
| `internal/ripestat/` | RIPEstat API client: fetch AS prefixes, generate SQL filter expressions. |
| `internal/store/store.go` | Interfaces: `FlowWriter`, `FlowReader`, `LinkStore`, `ASNameStore`. |
| `internal/store/clickhouse.go` | Write implementation (batch INSERT). |
| `internal/store/reader.go` | Read implementations — every aggregation query lives here. |
| `internal/store/flow_log.go` | `SearchFlowLog`, `FlowLogTimeSeries`, `TopProtocols`, `TopPorts`, `SetFlowLogRetention`. |
| `internal/store/threats.go` | `LiveThreats` aggregating query for the `/threats/live` endpoint. |
| `internal/store/alerts.go` | Alert rules / alerts / webhooks / audit log CRUD. `AlertDetails` JSON marshaling for `details` column. |
| `internal/store/alert_queries.go` | `Eval*` query helpers used by the alert engine. **`buildCIDRFilter` lives here** — read its docstring before touching any IP filtering code. |
| `internal/alerts/engine.go` | Alert engine goroutine — rule loop, cooldown map, top-source enrichment, auto-resolution, default-rule seeding. |
| `internal/alerts/webhook.go` | Slack / Teams / Discord / generic webhook notifier. |
| `internal/bgp/` | `Blocker` interface — `NoopBlocker` ships, swap in ExaBGP / GoBGP for real RFC 7999 blackholes. |
| `internal/services/wellknown.go` | IANA protocol + well-known port name resolution (used by Flow Search and Top Ports). |
| `internal/model/` | Shared types: `FlowRecord`, `ASTraffic`, `IPTraffic`, `LiveThreat`, `AlertRule`, etc. |
| `internal/config/` | Env-var config loading. `LoadCollector` and `LoadAPI`. |
| `migrations/` | ClickHouse DDL, numbered 000001–000009. The last three are feature-gated (only applied to fresh installs via `docker-entrypoint-initdb.d`). |
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
| `flows_raw` | `MergeTree` | per flow | 7 days |
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

Each aggregation table has **two MVs** (`_in_mv`, `_out_mv`) firing on
INSERT to `flows_raw`. The query layer picks the best table based on the
time range: 5-min for ≤90d, hourly for ≤2y, daily beyond.

### Optional tables (feature-gated)

Created by migrations 000007–000009. Used **only** when the corresponding
feature flag is enabled.

| Table | Feature | Purpose |
|---|---|---|
| `flows_log` | `FEATURE_FLOW_SEARCH` | Full per-tuple log, 1-min buckets, 180d default TTL, bloom-filter skip indexes on `src_ip` / `dst_ip`. |
| `traffic_by_port` | `FEATURE_PORT_STATS` | Aggregates per `(link, direction, protocol, port)`, 5-min, 1y TTL. |
| `traffic_by_dst_1min` | `FEATURE_ALERTS` | `AggregatingMergeTree` keyed by `(ts, dst_ip, protocol)`. Stores `bytes`, `packets`, `flow_count`, `syn_count`, plus HyperLogLog sketches `unique_src_ips` / `unique_src_ports`. 7d TTL. **Used exclusively by the alert engine and Live Threats** — no on-demand queries. |
| `traffic_by_src_1min` | `FEATURE_ALERTS` | Symmetric for outbound traffic. Used by `volume_out` / `port_scan`. |
| `alert_rules` | `FEATURE_ALERTS` | Configurable detection rules (`ReplacingMergeTree`). 10 default rules seeded on first startup (idempotent — only if the table is empty). |
| `alerts` | `FEATURE_ALERTS` | Triggered instances with full lifecycle (active → ack → resolved). 1y TTL. |
| `webhook_configs` | `FEATURE_ALERTS` | Slack / Teams / Discord / generic destinations (`ReplacingMergeTree`). |
| `audit_log` | `FEATURE_ALERTS` | Compliance trail for all sensitive actions. 1y TTL. |

## API endpoints

All under `/api/v1/`. Common params: `from`, `to`, `period`
(`1h` / `3h` / `6h` / `24h` / `7d` / `30d`), `link`, `links` (multi),
`direction` (`in` / `out`), `ip_version` (`4` / `6`), `limit`, `offset`.

### Always available

| Method | Path | Handler |
|---|---|---|
| `GET` | `/overview` | `Handler.Overview` |
| `GET` | `/status` | `Handler.Status` |
| `GET` | `/top/as` | `Handler.TopAS` |
| `GET` | `/top/ip` | `Handler.TopIP` (scope: `internal` / `external`) |
| `GET` | `/top/prefix` | `Handler.TopPrefix` |
| `GET` | `/as/{asn}` | `Handler.ASDetail` |
| `GET` | `/as/{asn}/peers` | `Handler.ASPeers` (uses `flows_raw`) |
| `GET` | `/as/{asn}/ips` | `Handler.ASTopIPs` (uses `traffic_by_ip_as`) |
| `GET` | `/ip/{ip}` | `Handler.IPDetail` (1m / 2m resolution on ≤6h via `flows_raw`, see `useRawTableForIP`) |
| `GET` | `/links` | `Handler.Links` |
| `GET` | `/links/traffic` | `Handler.LinksTraffic` |
| `GET` | `/link/{tag}` | `Handler.LinkDetail` |
| `GET` | `/dns/ptr?ip=X` | `Handler.DNSPtr` |
| `GET` | `/search?q=X` | `Handler.Search` |
| `GET` | `/features` | `Handler.Features` (frontend gate discovery) |
| `POST` | `/admin/links` | `Handler.LinkCreate` (CSRF) |
| `DELETE` | `/admin/links/{tag}` | `Handler.LinkDelete` (CSRF) |

### Feature-gated

Registered only when the corresponding `FEATURE_*` env var is set.

| Method | Path | Feature | Notes |
|---|---|---|---|
| `GET` | `/flows/search` | `FLOW_SEARCH` | `?format=csv` for export, max 100k rows |
| `GET` | `/flows/timeseries` | `FLOW_SEARCH` | drill-down |
| `GET` | `/top/protocol` | `PORT_STATS` | |
| `GET` | `/top/port` | `PORT_STATS` | |
| `GET` | `/alerts` | `ALERTS` | |
| `GET` | `/alerts/summary` | `ALERTS` | header badge counts |
| `GET` | `/threats/live` | `ALERTS` | Live Threats — real-time top destinations |
| `POST` | `/alerts/{id}/ack` | `ALERTS` | CSRF |
| `POST` | `/alerts/{id}/resolve` | `ALERTS` | CSRF |
| `POST` | `/alerts/{id}/block` | `ALERTS` | CSRF, **admin only** |
| `GET` / `POST` / `PUT` / `DELETE` | `/admin/rules[/{id}]` | `ALERTS` | CSRF + admin for writes |
| `GET` / `POST` / `PUT` / `DELETE` | `/admin/webhooks[/{id}]` | `ALERTS` | CSRF + admin for writes |
| `GET` | `/admin/audit` | `ALERTS` | admin only |

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

Three independent flags. All default to `false` to keep the base install
lightweight.

| Flag | Enables | New tables | Storage impact (1 Gbps, 1:1000) |
|---|---|---|---|
| `FEATURE_FLOW_SEARCH` | Forensic flow log + Search UI + CSV export | `flows_log` | ~250 GB / 6 mo |
| `FEATURE_PORT_STATS` | Top Protocols + Top Ports | `traffic_by_port` | ~5 MB/day |
| `FEATURE_ALERTS` | Alert engine + Alerts dashboard + Live Threats + webhooks + audit log + admin tabs | `traffic_by_dst_1min`, `traffic_by_src_1min`, `alert_rules`, `alerts`, `webhook_configs`, `audit_log` | ~50 GB |

The collector reads `FEATURE_ALERTS` to start the alert engine goroutine.
The API server reads all three to gate route registration. The frontend
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
4. For each rule, run the corresponding `Eval*` against the hot 1-min table.
5. For each violation: enrich with the top 5 source IPs from `flows_raw`
   (bounded query, runs only on actual violations), then either INSERT a
   new alert or heartbeat the existing active one (DB-level dedup via
   `FindActiveAlert`).
6. Dispatch webhooks asynchronously, with per-webhook severity filter.

### Rule types

| Type | Eval method | Source table |
|---|---|---|
| `volume_in` | `EvalVolumeInbound` | `traffic_by_dst_1min` |
| `volume_out` | `EvalVolumeOutbound` | `traffic_by_src_1min` |
| `syn_flood` | `EvalSynFlood` | `traffic_by_dst_1min` (filter on `protocol = 6`) |
| `amplification` | `EvalAmplification` | `traffic_by_dst_1min` (with optional `min_bps` floor reusing `ThresholdBps`) |
| `port_scan` | `EvalPortScan` | `traffic_by_src_1min` (uses `unique_dst_ports` HLL) |
| `icmp_flood` | `EvalProtocolFlood(proto=1)` | `traffic_by_dst_1min` |
| `udp_flood` | `EvalProtocolFlood(proto=17)` | `traffic_by_dst_1min` |
| `connection_flood` | `EvalConnectionFlood` | `traffic_by_dst_1min` (`flow_count` per dst) |
| `custom` | not implemented in phase 1 | — |

### Cooldown & cleanup

- Per-`(rule_id, target_ip)` cooldown tracked in an in-memory map; respects
  `CooldownSeconds` per rule.
- A separate goroutine `cooldownCleanupLoop` runs every 5 min and prunes
  entries older than 1 hour so the map cannot grow unbounded.

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

- Inbound volume — warning at 500 Mbps, critical at 2 Gbps
- TCP SYN flood (50k SYN/s)
- Connection-rate flood (200k flows / 60s — Slowloris signature)
- Reflection / amplification (10k unique srcs **AND** ≥100 Mbps sustained)
- ICMP flood (20k pps)
- UDP flood (100k pps)
- Port scan from internal host (1k unique dst ports)
- High outbound volume (500 Mbps)
- Sustained outbound exfiltration (50 Mbps over 5 min, "info" severity)

The full list with thresholds lives in `EnsureDefaultRules` in
`internal/alerts/engine.go`.

### BGP integration

`internal/bgp/Blocker` interface with `Announce` / `Withdraw` / `List`.
Phase 1 ships only a `NoopBlocker` (logs but doesn't actually announce).
Phase 2 will add ExaBGP / GoBGP backends for RFC 7999 blackholes. The
`/alerts/{id}/block` endpoint already calls the blocker — drop in the
implementation when ready.

## Time-series resolution rules

Both `LinkTimeSeries` and `IPTimeSeries` have a "raw" branch that queries
`flows_raw` directly with the autoStep bucket size, giving sub-5-min
granularity for short windows. This avoids the artifacting you'd get
otherwise from bucketing 5-min pre-aggregated data into 1-min slots.

| Function | "Use raw" condition | Helper |
|---|---|---|
| `LinkTimeSeries` | `to - from ≤ 3h` | `useRawTable()` |
| `IPTimeSeries` | `to - from ≤ 6h` | `useRawTableForIP()` (more aggressive — IP-filtered scans are cheap) |

Both use `autoStep()` for the bucket size: 1 min for ≤3h, 2 min for ≤6h,
5 min for ≤36h, 15 min / 30 min / 1 h beyond.

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
   even though the table has data. Always normalise CIDRs through
   `normalizeCIDRForIPv6Column()`.

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

6. **`SetFlowLogRetention` scans `count()` into `*uint8`**. Still open as
   of writing — non-blocking warning at collector startup, fix is to use
   `*uint64` in the destination scan.
