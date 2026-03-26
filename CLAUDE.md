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
- **chi router** (stdlib `http.Handler`), not echo/gin.
- **No ORM** — raw parameterized SQL via `clickhouse.Named()`.
- **TanStack Query** for frontend data fetching, not raw `useEffect`.
- **Dark-first** UI theme (NOC-inspired), JetBrains Mono with tabular numbers.

## Directory Map

| Path | Purpose |
|------|---------|
| `cmd/collector/` | Flow collector entrypoint |
| `cmd/api/` | API server entrypoint |
| `internal/collector/netflow/` | NetFlow v5 (fixed), v9/IPFIX (template-based) parsers |
| `internal/collector/sflow/` | sFlow v5 parser (raw packet header decoding) |
| `internal/collector/enricher/` | Maps (router_ip, snmp_index) → link tag + direction |
| `internal/collector/writer/` | Batch writer to ClickHouse (size or timer flush) |
| `internal/api/handler/` | HTTP handlers (one file per resource: as, ip, links, top, search, auth) |
| `internal/api/middleware/` | Auth (OIDC sessions), CSRF (double-submit cookie), rate limiting |
| `internal/api/router.go` | chi router wiring, middleware stack, security headers |
| `internal/store/store.go` | Interfaces: `FlowWriter`, `FlowReader`, `LinkStore`, `ASNameStore` |
| `internal/store/clickhouse.go` | Write implementation (batch INSERT) |
| `internal/store/reader.go` | Read implementations (all queries, parameterized) |
| `internal/model/` | Shared types: `FlowRecord`, `ASTraffic`, `IPTraffic`, etc. |
| `internal/config/` | Env-var config loading, validation |
| `migrations/` | ClickHouse DDL (single file, not versioned — no deployment yet) |
| `frontend/src/pages/` | React pages matching routes in App.tsx |
| `frontend/src/hooks/` | TanStack Query hooks (`useApi.ts`), URL-synced filters (`useFilters.ts`) |
| `frontend/src/components/charts/` | Recharts `TrafficChart` (stacked area, CSS custom properties for colors) |
| `frontend/src/lib/api.ts` | Typed fetch wrapper with CSRF token injection |

## Database Schema

**Tables** (all in `asstats` database):
- `flows_raw` — all received flows, 7d TTL, `MergeTree`
- `traffic_by_as` — 5-min buckets, 1y TTL, `SummingMergeTree`
- `traffic_by_ip` — 5-min, 30d TTL
- `traffic_by_prefix` — 5-min, 90d TTL
- `traffic_by_link` — 5-min, 1y TTL
- `traffic_by_ip_as` — IP×AS cross-reference, 5-min, 30d TTL
- `traffic_by_as_hourly` — hourly rollup, 2y TTL
- `links` — known link config, `ReplacingMergeTree`
- `as_names` — AS registry, `ReplacingMergeTree`

Each aggregation table has **two MVs** (one `_in_mv`, one `_out_mv`) that fire on INSERT to `flows_raw`.

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
| POST | `/admin/links` | `handler.LinkCreate` (CSRF protected) |
| DELETE | `/admin/links/{tag}` | `handler.LinkDelete` (CSRF protected) |

## Auth

- OIDC with PKCE (Authorization Code Flow) via `coreos/go-oidc` + `golang.org/x/oauth2`
- Session cookies (`SameSite=Strict`, `HttpOnly`, `Secure`)
- RBAC: `admin` / `viewer` mapped from OIDC `groups` or `roles` claims
- Disabled by default (`AUTH_ENABLED=false`)
- CSRF: double-submit cookie on POST/PUT/DELETE (`X-CSRF-Token` header)

## Running

```bash
make docker-up          # ClickHouse + Redis
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
- SQL: always `clickhouse.Named()` for parameters, never string concatenation
- Docker: `FROM --platform=$BUILDPLATFORM` + `TARGETARCH` for cross-compilation
