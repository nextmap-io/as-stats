# AS-Stats Modern

Modern replacement for AS-Stats — collects NetFlow/sFlow/IPFIX from routers, stores in ClickHouse, serves via REST API + React UI.

## Architecture

```
Routers --UDP(2055/6343)--> Collector(Go) --batch INSERT--> ClickHouse
                                                                |
                                                          API Server(Go) --REST JSON--> Frontend(React)
```

Single Go module, two binaries (`cmd/collector`, `cmd/api`), React frontend in `frontend/`.

## Key Decisions

- **ClickHouse** for storage: `SummingMergeTree` for aggregation, materialized views fire on INSERT to `flows_raw`, no application-side aggregation needed. Always `sum()` in queries (not-yet-merged rows).
- **Separate MVs** per direction (in/out) writing to same target table — no UNION ALL in MVs (ClickHouse compat).
- **IPv6 columns everywhere** — IPv4 stored as IPv4-mapped IPv6.
- **Channel-based pipeline** in collector: UDP listeners → parser goroutines → enricher → batch writer. Backpressure via buffered channels; UDP drops acceptable for sampled data.
- **LOCAL_AS enrichment**: at startup, fetches announced prefixes from RIPEstat API. Flows with src/dst IPs in these prefixes get private AS numbers remapped to the public AS. Refreshed at collector startup.
- **In-memory API cache**: middleware caches GET responses for 30s on heavy endpoints (overview, top AS/IP, links). Bypass with `Cache-Control: no-cache`. No Redis dependency.
- **chi router** (stdlib `http.Handler`), not echo/gin.
- **No ORM** — raw parameterized SQL via `clickhouse.Named()`.
- **TanStack Query** for frontend data fetching, not raw `useEffect`.
- **Dark-first** UI theme (NOC-inspired), JetBrains Mono with tabular numbers.

## Collector Pipeline

```
UDP Socket ──► [1 Reader goroutine] ──► packets channel (workers×64 buffer)
                                              │
                    ┌─────────────────────────┼─────────────────────────┐
                    ▼                         ▼                         ▼
           [Decoder worker 1]        [Decoder worker 2]  ...  [Decoder worker N]
           Parse NetFlow/sFlow       Parse NetFlow/sFlow       Parse NetFlow/sFlow
                    │                         │                         │
                    └─────────────────────────┼─────────────────────────┘
                                              ▼
                                    flows channel (batchSize×4 buffer)
                                              │
                                    [1 Enricher goroutine]
                                    - Link tag + direction (SNMP ifindex match)
                                    - LOCAL_AS prefix remap
                                              │
                                    enriched channel (batchSize×4 buffer)
                                              │
                                    [1 BatchWriter goroutine]
                                    - Accumulates flows up to batchSize
                                    - Flushes on size OR timer (flushInterval)
                                    - Single batch INSERT to ClickHouse
```

- **Reader**: single goroutine (1 UDP socket = 1 reader). Reads packets, copies router IP, sends to decoder channel.
- **Decoders**: `COLLECTOR_WORKERS` goroutines (default 4). Parse NetFlow v5/v9/IPFIX or sFlow v5 in parallel. Each worker decodes independently — no shared state except the global template cache (mutex-protected).
- **Enricher**: single goroutine. Maps `(router_ip, snmp_index)` → link tag + direction. Remaps private AS to `LOCAL_AS` for matching prefixes. Uses `sync.RWMutex` — link config reloaded every 60s without blocking.
- **BatchWriter**: single goroutine. Buffers flows and writes to ClickHouse in batches. Flushes when buffer reaches `COLLECTOR_BATCH_SIZE` or `COLLECTOR_FLUSH_INTERVAL` expires. Tracks metrics (flows received/written, errors, batch timing).
- **Backpressure**: all channels are buffered. If ClickHouse is slow, channels fill up and UDP packets are dropped at the socket level — acceptable for sampled flow data.
- **Template cache**: global in-memory cache keyed by `(router_ip, source_id, template_id)`. Templates expire after 30 minutes. Cisco IOS-XE with source port 0 requires `NOTRACK` iptables rule to bypass conntrack.

### Scaling

- Increase `COLLECTOR_WORKERS` if CPU-bound on decoding (check with `top`)
- The enricher is single-threaded but fast (map lookup + prefix match)
- The batch writer is I/O-bound (ClickHouse INSERT) — increase `COLLECTOR_BATCH_SIZE` for higher throughput
- For 500k+ flows/sec, consider multiple collector instances on different ports

## Directory Map

| Path | Purpose |
|------|---------|
| `cmd/collector/` | Flow collector entrypoint |
| `cmd/api/` | API server entrypoint |
| `internal/collector/netflow/` | NetFlow v5 (fixed), v9/IPFIX (template-based) parsers |
| `internal/collector/sflow/` | sFlow v5 parser (raw packet header decoding) |
| `internal/collector/enricher/` | Maps (router_ip, snmp_index) → link tag + direction |
| `internal/collector/writer/` | Batch writer to ClickHouse (size or timer flush) |
| `internal/api/handler/` | HTTP handlers (one file per resource: as, ip, links, top, search, auth, dns, status) |
| `internal/api/middleware/` | Auth (OIDC sessions), CSRF (double-submit cookie), rate limiting, in-memory cache |
| `internal/ripestat/` | RIPEstat API client: fetch AS prefixes, generate SQL filters |
| `internal/api/router.go` | chi router wiring, middleware stack, security headers |
| `internal/store/store.go` | Interfaces: `FlowWriter`, `FlowReader`, `LinkStore`, `ASNameStore` |
| `internal/store/clickhouse.go` | Write implementation (batch INSERT) |
| `internal/store/reader.go` | Read implementations (all queries, parameterized) |
| `internal/model/` | Shared types: `FlowRecord`, `ASTraffic`, `IPTraffic`, etc. |
| `internal/config/` | Env-var config loading, validation |
| `migrations/` | ClickHouse DDL (numbered migrations: 000001-000009) — last three are feature-gated tables |
| `internal/services/wellknown.go` | IANA protocol + well-known port name resolution (used by flow search and top ports) |
| `internal/alerts/` | Alert engine (rule evaluator, webhook notifier, default rules) |
| `internal/bgp/` | BGP blackhole `Blocker` interface (NoopBlocker stub for phase 1) |
| `frontend/src/pages/` | React pages matching routes in App.tsx |
| `frontend/src/hooks/` | TanStack Query hooks (`useApi.ts`), URL-synced filters (`useFilters.ts`), unit toggle (`useUnit.ts`), chart theme (`useChartColors.ts`), DNS (`useDns.ts`) |
| `frontend/src/components/charts/` | `TrafficChart` (single in/out), `LinkTrafficChart` (stacked by link), `ASTrafficChart` (stacked by AS) — all Recharts AreaChart with stepAfter |
| `frontend/src/components/` | `ExpandableChart` (modal on click), `ChartModal` (fullscreen with period selector), `PTR` (reverse DNS display), `ErrorBoundary` |
| `frontend/src/lib/api.ts` | Typed fetch wrapper with CSRF token injection |

## Database Schema

**Core tables** (all in `asstats` database):
- `flows_raw` — all received flows, 3d TTL, `MergeTree`
- `traffic_by_as` — 5-min buckets, 90d TTL, `SummingMergeTree` (includes `ip_version`)
- `traffic_by_ip` — 5-min, 14d TTL
- `traffic_by_prefix` — 5-min, 30d TTL
- `traffic_by_link` — 5-min, 90d TTL (includes `ip_version`)
- `traffic_by_ip_as` — IP×AS cross-reference, 5-min, 14d TTL
- `traffic_by_as_hourly` — hourly rollup, 2y TTL (includes `ip_version`)
- `traffic_by_as_daily` — daily rollup, 5y TTL (includes `ip_version`)
- `traffic_by_link_hourly` — hourly rollup, 2y TTL (includes `ip_version`)
- `traffic_by_link_daily` — daily rollup, 5y TTL (includes `ip_version`)
- `links` — known link config (tag, router_ip, snmp_index, color), `ReplacingMergeTree`
- `as_names` — AS registry (imported from RIPE), `ReplacingMergeTree`

**Optional feature tables** (created by migrations 000007-000009, used only when corresponding feature flag is enabled):
- `flows_log` — full per-tuple flow records (src/dst IP+port, protocol, TCP flags), 1-min buckets, 180d TTL, `SummingMergeTree`. Bloom-filter skip indexes on `src_ip`, `dst_ip`. Used by Flow Search (FEATURE_FLOW_SEARCH).
- `traffic_by_port` — aggregated per (link, direction, protocol, port), 5-min buckets, 1y TTL. Used by Top Protocols/Top Ports (FEATURE_PORT_STATS).
- `traffic_by_dst_1min` — `AggregatingMergeTree` keyed by (ts, dst_ip, protocol). Stores `bytes`, `packets`, `flow_count`, `syn_count`, plus HyperLogLog sketches `unique_src_ips` / `unique_src_ports`. 7d TTL. Used by alert engine for DDoS detection (volume_in, syn_flood, amplification).
- `traffic_by_src_1min` — symmetric for outbound traffic. Used for `volume_out` and `port_scan` alert rules.
- `alert_rules` — configurable DDoS detection rules (`ReplacingMergeTree`). Default rules seeded on first startup.
- `alerts` — triggered alert instances with full lifecycle (active → ack → resolved). 1y TTL.
- `webhook_configs` — Slack/Teams/Discord/generic notification destinations (`ReplacingMergeTree`).
- `audit_log` — compliance trail of sensitive actions (flow_search, alert_action, link_create/delete, rule_*, webhook_*). 1y TTL.

Each aggregation table has **two MVs** (one `_in_mv`, one `_out_mv`) that fire on INSERT to `flows_raw`. The query layer picks the best table based on the time range (5-min for ≤90d, hourly for ≤2y, daily beyond). The hot 1-min tables are used **exclusively** by the alert engine — no on-demand queries — so DDoS detection scales to high flow volumes without scanning `flows_raw`.

## API Endpoints

All under `/api/v1/`. Common params: `from`, `to`, `period` (1h/6h/24h/7d/30d), `link`, `direction` (in/out), `limit`, `offset`.

| Method | Path | Handler |
|--------|------|---------|
| GET | `/overview` | `handler.Overview` |
| GET | `/top/as` | `handler.TopAS` |
| GET | `/top/ip` | `handler.TopIP` |
| GET | `/top/prefix` | `handler.TopPrefix` |
| GET | `/as/{asn}` | `handler.ASDetail` (time series) |
| GET | `/as/{asn}/peers` | `handler.ASPeers` (from flows_raw) |
| GET | `/as/{asn}/ips` | `handler.ASTopIPs` (from traffic_by_ip_as) |
| GET | `/ip/{ip}` | `handler.IPDetail` (time series + top AS) |
| GET | `/links` | `handler.Links` |
| GET | `/link/{tag}` | `handler.LinkDetail` (time series + top AS) |
| GET | `/search?q=` | `handler.Search` (AS name/number) |
| GET | `/features` | `handler.Features` (feature discovery for frontend) |
| POST | `/admin/links` | `handler.LinkCreate` (CSRF protected) |
| DELETE | `/admin/links/{tag}` | `handler.LinkDelete` (CSRF protected) |

### Optional endpoints (feature-gated)

Enabled only when the corresponding `FEATURE_*` env var is set:

| Method | Path | Feature | Handler |
|--------|------|---------|---------|
| GET | `/flows/search` | `FEATURE_FLOW_SEARCH` | `handler.FlowSearch` (`?format=csv` for export, max 100k rows) |
| GET | `/flows/timeseries` | `FEATURE_FLOW_SEARCH` | `handler.FlowTimeSeries` (drill-down) |
| GET | `/top/protocol` | `FEATURE_PORT_STATS` | `handler.TopProtocols` |
| GET | `/top/port` | `FEATURE_PORT_STATS` | `handler.TopPortsHandler` |
| GET | `/alerts` | `FEATURE_ALERTS` | `handler.ListAlerts` |
| GET | `/alerts/summary` | `FEATURE_ALERTS` | `handler.AlertsSummary` (header badge) |
| POST | `/alerts/{id}/ack` | `FEATURE_ALERTS` | `handler.AcknowledgeAlert` (CSRF) |
| POST | `/alerts/{id}/resolve` | `FEATURE_ALERTS` | `handler.ResolveAlert` (CSRF) |
| POST | `/alerts/{id}/block` | `FEATURE_ALERTS` | `handler.BlockAlertBGP` (CSRF, admin only) |
| GET | `/admin/rules` | `FEATURE_ALERTS` | `handler.ListRules` |
| POST | `/admin/rules` | `FEATURE_ALERTS` | `handler.CreateRule` (CSRF, admin) |
| PUT | `/admin/rules/{id}` | `FEATURE_ALERTS` | `handler.UpdateRule` (CSRF, admin) |
| DELETE | `/admin/rules/{id}` | `FEATURE_ALERTS` | `handler.DeleteRule` (CSRF, admin) |
| GET | `/admin/webhooks` | `FEATURE_ALERTS` | `handler.ListWebhooks` |
| POST | `/admin/webhooks` | `FEATURE_ALERTS` | `handler.CreateWebhook` (CSRF, admin) |
| PUT | `/admin/webhooks/{id}` | `FEATURE_ALERTS` | `handler.UpdateWebhook` (CSRF, admin) |
| DELETE | `/admin/webhooks/{id}` | `FEATURE_ALERTS` | `handler.DeleteWebhook` (CSRF, admin) |
| GET | `/admin/audit` | `FEATURE_ALERTS` | `handler.ListAuditLog` |

## Auth

- OIDC with PKCE (Authorization Code Flow) via `coreos/go-oidc` + `golang.org/x/oauth2`
- Session cookies (`SameSite=Strict`, `HttpOnly`, `Secure`)
- RBAC: `admin` / `viewer` mapped from OIDC `groups` or `roles` claims
- Disabled by default (`AUTH_ENABLED=false`)
- CSRF: double-submit cookie on POST/PUT/DELETE (`X-CSRF-Token` header)

## Running

```bash
make docker-up          # ClickHouse
make run-collector      # Terminal 1
make run-api            # Terminal 2
cd frontend && npm run dev  # Terminal 3
```

`make ci` runs all checks locally (Go lint/test/build + frontend lint/typecheck/build).

## CI/CD

- **CI** (`.github/workflows/ci.yml`): lint, test, build, Docker push to GHCR on `main`
- **Release** (`.github/workflows/release.yml`): manual dispatch or tag push → Go binaries (4 platforms), Docker multi-arch (amd64+arm64), GitHub Release with changelog
- **Dependabot**: Go, npm, Docker, GitHub Actions (weekly)
- Docker images: `ghcr.io/nextmap-io/as-stats-{collector,api,frontend}`

## Code Conventions

- Go: `golangci-lint` with `errcheck` — handle all errors, use `_ =` only for intentional discards
- Go: no `any` types in handler responses — use concrete model types
- Go: security headers set in router middleware, not per-handler
- Frontend: no `any` types — typed API client with generics
- Frontend: URL-synced filters (search params), not component state
- Frontend: all data fetching via TanStack Query hooks, never raw `useEffect` for API calls
- Frontend: feature-gated UI uses `useFeatureFlags()` hook with safe defaults while loading
- SQL: always `clickhouse.Named()` for parameters, never string concatenation
- Docker: `FROM --platform=$BUILDPLATFORM` + `TARGETARCH` for cross-compilation

## Feature Flags

Three independent flags control optional functionality. All default to `false` to keep base install lightweight.

| Flag | What it enables | Tables created | Storage impact (1G/1:1000) |
|------|----------------|----------------|----------------------------|
| `FEATURE_FLOW_SEARCH` | Forensic flow log + Search UI + CSV export (cap 100k rows) | `flows_log` | ~250 GB / 6 mo |
| `FEATURE_PORT_STATS` | Top Protocols + Top Ports views | `traffic_by_port` | ~5 MB/day |
| `FEATURE_ALERTS` | DDoS detection engine + Alerts dashboard + webhooks + audit log + admin UI tabs | `traffic_by_dst_1min`, `traffic_by_src_1min`, `alert_rules`, `alerts`, `webhook_configs`, `audit_log` | ~50 GB |

The collector reads `FEATURE_ALERTS` to start the alert engine goroutine. The API server reads all three to gate route registration. The frontend reads `/api/v1/features` (cached forever) to conditionally render UI elements.

## Alert Engine

Located in `internal/alerts/`. Runs as a goroutine in the collector when `FEATURE_ALERTS=true`.

**Pipeline**:
1. Loads enabled rules from `alert_rules` table
2. For each rule, runs the corresponding query against the hot 1-min table (`traffic_by_dst_1min` or `traffic_by_src_1min`)
3. For each violation: insert new alert OR heartbeat existing active alert (DB-level dedup via `FindActiveAlert`)
4. Auto-resolves alerts whose `last_seen_at` is older than `ALERT_STALE_THRESHOLD` (default 5m)
5. Notifies via configured webhooks (async, with severity filter)

**Built-in rule types**:
- `volume_in` — bps/pps received by a single destination
- `volume_out` — bps/pps sent from a single source (compromised host)
- `syn_flood` — TCP SYN-only packet rate to a destination
- `amplification` — unique source IPs to one destination (HyperLogLog)
- `port_scan` — unique destination ports from one source (HyperLogLog)
- `custom` — placeholder for SQL-based rules (phase 2)

**LOCAL_AS prefix filter**: rules only fire on IPs in our announced prefixes. Loaded from RIPEstat at collector startup. This prevents alerting on remote IPs we can't act on.

**Default rules**: seeded on first startup if `alert_rules` is empty (idempotent). Includes "High inbound volume" (500 Mbps warning, 2 Gbps critical), "SYN flood", "Reflection/amplification attack", "Port scan from internal host", "High outbound volume".

**BGP integration**: `internal/bgp/` defines a `Blocker` interface with `Announce` / `Withdraw` / `List`. Phase 1 ships only a `NoopBlocker` (logs but doesn't actually announce). Phase 2 will add ExaBGP/GoBGP backends for RFC 7999 blackholes. The `/alerts/{id}/block` endpoint already calls the blocker — swap out the implementation when ready.
