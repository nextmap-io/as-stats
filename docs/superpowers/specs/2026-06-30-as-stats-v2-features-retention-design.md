# as-stats v2 — Features + Retention Management (Design Spec)

Date: 2026-06-30
Branch: `feat/v2-features-retention`
Status: approved core (A–E) + enriched additions from discovery workflow

## Goal

Make as-stats more complete and closer to commercial netflow products (Kentik,
NETSCOUT/Arbor, Akvorado, Elastiflow), and give it proper ClickHouse retention
management with progressive, observable data cleanup.

Two primary axes:
1. **Retention/storage management** — runtime-controllable TTLs, bounded growth,
   observability, progressive cleanup.
2. **More useful analytics + UI** — leverage data/fields already present, add
   high-value light-dependency features, and lift UI/UX quality.

## Hard constraints (from CLAUDE.md — all work must respect)

- ClickHouse only. `SummingMergeTree` + MVs. **Always `sum()` in reads.**
- IPv4 stored as IPv4-mapped IPv6 (`::ffff:x.x.x.x`). Use `buildCIDRFilter` /
  IPv6-mapped probes for any IP filtering. Never `isIPAddressInRange` on raw CIDR.
- ClickHouse alias shadowing: alias the table (`FROM t f`) and qualify (`f.ts`)
  in any query that aggregates a column it also filters on.
- No infix bitwise — use `bitAnd()`.
- Raw parameterized SQL via `clickhouse.Named()`. No ORM, no string concat for
  user input. ORDER BY columns must be whitelisted, never concatenated raw.
- Go: handle all errors (errcheck), no `any` in handler responses.
- Frontend: TanStack Query for all fetches, URL-synced filters, no `any`,
  feature-flag-gated UI, JetBrains Mono tabular-nums, dark-first.
- Prefer zero / light new dependencies (supply-chain caution).
- Migrations numbered sequentially after `000011`. New optional tables are
  feature-gated where appropriate.

## Delivery model

Single branch `feat/v2-features-retention`, one commit per module, CI (Docker)
green before moving on. Implemented autonomously via Opus subagents orchestrated
by the main session; modules sequenced to avoid conflicts on shared files
(`router.go`, `model/model.go`, `reader.go`, `App.tsx`, `lib/types.ts`,
`lib/api.ts`, `hooks/useApi.ts`).

---

## Module A — Retention & Storage observability (PRIMARY GOAL)

### A1. DB-backed retention policies + reconciler
- New table `retention_policies` (`ReplacingMergeTree(updated_at)`), columns:
  `table_name String, ttl_days UInt32, enabled UInt8, updated_at DateTime`.
  ORDER BY `table_name`.
- Seeded on first startup (idempotent — only if empty) with current migration
  TTLs for every TTL-bearing table (flows_raw 3, traffic_by_ip 14, prefix 30,
  as/link 90, port 365, hourly 730, daily 1825, dst/src_1min 7, alerts/audit 365,
  bgp_blocks 365, flows_log = `FLOW_LOG_RETENTION_DAYS`).
- **Reconciler goroutine** (collector): every `RETENTION_RECONCILE_INTERVAL`
  (default 15m), reads policies, and for each `enabled` row whose desired TTL
  differs from the live TTL, runs `ALTER TABLE <t> MODIFY TTL <tscol> + INTERVAL N DAY`.
  Idempotent, metadata-only. Skips tables not present (feature-gated ones).
- Generalizes and **replaces** `SetFlowLogRetention`; **fixes the `count()`
  into `*uint8` bug** (use `uint64`). Centralize the table→ts-column map.

### A2. Config-table soft-delete purge
- Goroutine (collector), runs daily: for each `ReplacingMergeTree` config table
  with a `deleted` column (`alert_rules`, `webhook_configs`, `hostgroups`),
  physically remove rows where `deleted = 1 AND updated_at < now() - INTERVAL
  CONFIG_PURGE_DAYS DAY` (default 30) via `ALTER TABLE ... DELETE`. Then
  `OPTIMIZE TABLE ... FINAL` is NOT forced (expensive) — rely on the lightweight
  mutation. Log counts.

### A3. Storage observability
- `GET /api/v1/admin/storage` (admin-only): returns per-table compressed +
  uncompressed bytes, part count, rows, oldest/newest partition, configured TTL
  (from `retention_policies`), and pending TTL mutations; plus disk totals
  (`system.disks`: free/total/used %). All from `system.parts`, `system.tables`,
  `system.mutations`, `system.disks`. No user input → no injection surface, but
  still parameterized where any value is interpolated.
- `PUT /api/v1/admin/retention/{table}` (admin, CSRF): set `ttl_days` / `enabled`
  for a table; writes `retention_policies`; reconciler applies within interval
  (or apply immediately). Audit-logged.

### A4. Disk-usage alert rule
- New alert rule type `disk_usage`: evaluates disk used % vs `ThresholdPercent`
  (reuse a numeric threshold field). Fires a normal alert through the existing
  engine/webhook pipeline. Default rule seeded (warning 80%, critical 90%).

### A5. Frontend — Admin → Storage tab
- New tab in `Admin.tsx`: table of per-table size/rows/parts/oldest-partition,
  editable TTL (days) + enabled toggle per table (admin), disk-usage gauge,
  pending-mutation/“TTL lag” badge. TanStack Query, 30s refresh.

---

## Module B — Capacity planning (reuses `links.capacity_mbps`)

- `GET /api/v1/links/capacity`: per link, current bps (latest bucket), p95 bps
  (selected window), utilization% = p95 / (capacity_mbps*1e6), and a
  **linear-regression forecast** over `traffic_by_link_daily` p95 → estimated
  date crossing 80/95/100% (“days to saturation”), null if capacity unset or
  trend flat/negative.
- Add `utilization_pct` + `capacity_mbps` to `/link/{tag}` response.
- New alert rule type `link_capacity`: util% sustained > threshold.
- **B+ Load-duration curve**: `GET /api/v1/link/{tag}/load-curve` — sorted
  descending throughput samples + `quantiles(0.5,0.9,0.95,0.99,1.0)` + binned
  histogram, over the window. (folds in the discovery “load-duration” item.)
- Frontend: **Capacity** page (util% bars per link with capacity overlay,
  forecast), and a load-duration step chart on Link Detail.

## Module C — Geo / country (reuses `as_names.country`, AS-level)

- Populate `as_names.country` during AS-name enrichment (data source already
  used for names provides country; if absent, leave empty — degrade gracefully).
- `GET /api/v1/top/country`: `traffic_by_as` JOIN `as_names` GROUP BY country,
  `sum()`-wrapped, direction/link/ip_version filters. Add `country` to AS
  responses (`/top/as`, `/as/{asn}`).
- Frontend: **Countries** page (top countries table + lightweight inline SVG
  world choropleth, no map lib), country code/flag on AS pages.
- Explicitly NO MaxMind / per-IP geo (future backlog).

## Module D — Comparison + reporting + percentiles

- **Comparison**: `compare=prev` param on time-series endpoints → aligned prior
  window series + delta% in the response envelope.
- **Movers**: `GET /api/v1/changes/movers?dimension=as|prefix|port|country&window=`
  — current vs prior equal window, ranked gainers/losers (abs + rel delta),
  IPv6-mapped-aware. (discovery item)
- **New/disappeared talkers**: `GET /api/v1/changes/talkers?dimension=as|ip|prefix&window=`
  — anti-join current vs prior. (discovery item)
- **Percentiles**: extend p95 logic to expose p50/p95/p99 (`percentiles` param);
  95th-percentile billing surfaced on Capacity/Link.
- **Scheduled reports**: `report_schedules` table + cron goroutine → render
  HTML summary + CSV, deliver via SMTP (config like webhooks). NO PDF.
- Frontend: comparison toggle on charts, **Admin → Reports** CRUD, movers/talkers
  surfaced on Dashboard (“What changed”).

## Module E — Anomaly detection (statistical baseline)

- New rule type `anomaly`: for each target, compute baseline = median + k·MAD
  over the last N samples at the same hour-of-week from `traffic_by_*_hourly`;
  fire if current > baseline (k = configurable sensitivity). Reuses cooldown /
  webhook / lifecycle. NO ML.
- **E+ Explainability**: `GET /api/v1/anomaly/explain?target=&from=&to=` —
  decompose flagged window vs baseline, ranking top contributing src IPs / ports
  / protocols by delta; store “why” in alert `details`. (discovery item)
- Frontend: `anomaly` type + sensitivity in the rule editor; explanation panel
  on alert detail.

---

## Module F — Light analytics additions (S-effort, pure read-layer)

- **F1 Multi-metric Top-N**: `metric=bytes|packets|flows` (whitelisted) on
  `/top/as`,`/top/ip`,`/top/prefix`; derive `avg_pkt_size = sum(bytes)/sum(packets)`.
  Frontend sort/column toggle.
- **F2 In/out asymmetry**: per-AS `sumIf(bytes,direction='in')` vs `out`, ratio +
  class (eyeball/content/balanced); column on `/top/as` + `/as/{asn}`.
- **F3 Conversations explorer**: `GET /api/v1/conversations?dim=...` bidirectional
  top-talkers folding A→B and B→A; URL-synced dimension pickers; drill to Flow
  Search. Gated by `FEATURE_FLOW_SEARCH` when using flows_log, else flows_raw.

## Module G — Read-only API tokens

- `api_tokens` table: `id, name, token_hash (salted), scope, owner, created_at,
  last_used_at, expires_at, revoked`. Middleware accepts `Authorization: Bearer`
  for **GET-only** access, bypassing CSRF for token auth only; OIDC still applies
  for browser sessions. Admin UI to mint (show once)/scope/revoke; all mint/revoke
  audit-logged.

## Module U — UI/UX foundation + visualization (cross-cutting)

Applied across the app, built early so feature pages inherit them:
- **U1 QueryBoundary + EmptyState** — unified skeleton/error+retry/empty.
- **U2 DataTable<T>** — generic typed table: per-column sort (URL-synced),
  sticky header, tabular-nums numeric cells, reusable percent-bar cell,
  lightweight windowing for large lists (no react-window). Refactor existing
  hand-rolled tables onto it.
- **U3 ExportButton** — client-side CSV/JSON export on every Top-N/detail table.
- **U4 Command palette (⌘K / “/”)** — custom accessible modal: route fuzzy-match,
  pipes to `/search`, accepts raw IP/prefix/link (reuse `detectRedirect`),
  recently-viewed MRU (localStorage), preserves period/from/to.
- **U5 Expanded time-range control** — add 15m/4h/12h, yesterday, this week, and
  absolute from/to picker (frontend-only; backend already takes from/to).
- **U6 Copy shareable link** — freeze relative period to absolute from/to.
- **U7 Density toggle** — comfortable/compact, persisted; DataTable honors it.
- **U8 Heatmap** — 7×24 day-of-week × hour-of-day throughput (CSS/SVG, no lib)
  over hourly aggregate.
- **U9 Treemap + donut** — chart/table toggle on Top-N pages (Recharts Treemap +
  PieChart), data already fetched.
- **U10 a11y + design-token pass** — focus rings, ARIA on tables/charts (SR data
  fallback), keyboard nav, prefers-reduced-motion, contrast-verified tokens,
  consolidate color literals into CSS custom properties; verify with react-doctor
  (frontend/.claude) without score regression.

---

## Build order (sequenced on one branch)

1. **A** — Retention & storage observability (primary goal).
2. **U1–U3** — UI foundation (QueryBoundary, DataTable, ExportButton).
3. **B** — Capacity (+ load-duration).
4. **C** — Geo/country.
5. **F** — Light analytics (multi-metric, asymmetry, conversations).
6. **D** — Comparison + movers/talkers + percentiles + reports.
7. **E** — Anomaly + explainability.
8. **G** — API tokens.
9. **U4–U10** — Palette, time-range, copy-link, density, heatmap, treemap/donut, a11y.

Each module: migration (if needed) → store → handler → route → model → TS types
→ hook → page/UI → Go tests → Docker verify → commit.

## Feature flags

- Retention reconciler + storage tab: always on (core).
- `FEATURE_REPORTS` (new): scheduled reports + SMTP.
- API tokens: gated by `FEATURE_API_TOKENS` (new) or always-on admin feature.
- Reuse `FEATURE_ALERTS` for anomaly/disk_usage/link_capacity rule types.
- Reuse `FEATURE_PORT_STATS` / `FEATURE_FLOW_SEARCH` for relevant analytics.

## Out of scope (future backlog, with blockers)

Traffic matrix AS×AS + Sankey (needs as-pair tables + MVs on hot path),
peering/transit cost (needs relationship model), threat-intel feeds (external
fetch / supply-chain), SNMP polling (gosnmp dep), DSCP/ToS (collector parser +
flows_raw migration on hot path), custom tagging (query-time SQL gen risk),
drag-drop dashboard (large FE build), sampling-correction column (schema-wide
churn). Each deferred deliberately; revisit after v2 lands.

## Testing & verification

- Go: unit tests for new store query builders (SQL shape, param binding,
  IPv6-mapped handling), reconciler logic, percentile math, forecast regression.
- Verify each module with `golangci-lint` + `go build ./...` + `go test ./...`
  and frontend lint/typecheck/build, all in Docker (per sandboxing policy).
- CI (GitHub Actions) green on the branch before opening the final PR.
