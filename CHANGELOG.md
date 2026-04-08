# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.4.2] - 2026-04-08

### Added
- **Admin > Alert Rules — create form**. The Rules tab now has an "Add rule"
  button next to the title. The form switches its threshold inputs depending
  on the selected rule type (e.g. amplification asks for `min unique sources`
  AND `min sustained bps`, syn_flood asks for `pps` only) and shows a short
  inline description for each type. Covers all 9 rule types including the new
  `icmp_flood` / `udp_flood` / `connection_flood` from v1.4.0.

### Fixed
- **`/flows/search` returned HTTP 500** as soon as any filter was provided.
  Same family of bug as the `/top/port` regression in v1.2.1: the SELECT
  aliased `min(ts) AS ts` (and unqualified column names like `src_ip`,
  `protocol`, ...), so ClickHouse resolved `ts` in the WHERE clause as the
  aggregated alias and threw `Aggregate function min(ts) AS ts is found in
  WHERE in query`. Added a `flows_log f` table alias and qualified every
  column reference in the WHERE/GROUP BY clauses. Validated end-to-end against
  the production `flows_log` table.
- **Admin > Alert Rules — threshold display**. When a rule had both
  `threshold_bps` and `threshold_count` set (the v1.4.0 amplification default,
  for instance, has `100 Mbps` floor + `10000` unique sources), the cell
  rendered them as one concatenated string (`100 Mbps10 000`). Each populated
  threshold now appears on its own line with a unit suffix (`srcs` /
  `ports` / `flows`) so the meaning of each value is unambiguous.
- **Live Threats summary cards — vertical baseline**. The CRITICAL / WARN / OK
  cards used a single `CardContent` with the title and icon packed into a
  flex row at the top, which produced inconsistent vertical positioning vs.
  cards built with the standard `CardHeader > CardTitle` pattern (Top
  Protocols, etc.). Refactored to use `CardHeader + CardContent` so the title
  baseline lines up with every other card on the page.
- **Dashboard — IPv4/IPv6 traffic-by-link card titles**. Same root cause as
  the Live Threats fix: the chart titles were rendered as inline `<h3>`
  inside `LinkTrafficChart` (whose parent was a bare `CardContent`), which
  put them at a different vertical baseline than every other card. Moved the
  titles into proper `CardHeader > CardTitle` and dropped the `title` prop
  from the chart inside the cards.

## [1.4.1] - 2026-04-08

### Added
- Show reverse DNS (`IPWithPTR`) next to each destination on the Live Threats
  page, matching how Top IP / Flow Search / IP Detail render addresses.

## [1.4.0] - 2026-04-08

### Added — Alert engine improvements

- **Three new rule types** (no schema migration required — `rule_type` is a
  `LowCardinality(String)`):
  - `icmp_flood` — high pps of ICMP (proto 1) to a single destination
  - `udp_flood` — high pps of UDP (proto 17) to a single destination, e.g. DNS
    query flood / NTP query flood signatures
  - `connection_flood` — high `flow_count` per destination regardless of
    bytes/packets, catches Slowloris-class connection-rate abuse and half-open
    scans that slip past `volume_in` and `syn_flood`

- **Top source IP enrichment** on every triggered alert. The engine now does a
  bounded `flows_raw` lookup (5 sources max, time-windowed, only on actual
  violations) and stores the result in the alert `details.top_sources` JSON
  field. Operators no longer need to run a separate flow search to figure out
  *who* is hitting the target.

- **Bandwidth floor on `amplification`** rules. `ThresholdBps` is now reused
  as a "minimum sustained bps" filter. Without it, every scanner that touches
  one of our IPs from many sources at trivial volume produced a constant
  amplification false positive. Default seeded amplification rule now requires
  ≥ 100 Mbps as well as 10k unique sources.

- **Cooldown map cleanup loop** — a new background goroutine prunes
  `(rule_id, target_ip)` cooldown entries older than 1 hour every 5 minutes.
  Without this the in-memory map grew unboundedly: every unique attacker IP
  that ever fired a rule kept an entry forever.

- **Default rules expanded from 6 to 10**. New seeded rules:
  - "Connection-rate flood" (`connection_flood`, 200k flows / 60s, warning)
  - "ICMP flood" (`icmp_flood`, 20k pps / 60s, warning)
  - "UDP flood" (`udp_flood`, 100k pps / 60s, warning)
  - "Sustained outbound exfiltration" (`volume_out`, 50 Mbps / 5 min / 30 min
    cooldown, info — slow exfil signature distinct from the existing
    high-volume outbound rule)

  The existing "Reflection/amplification attack" rule was tightened to require
  ≥ 100 Mbps in addition to 10k unique sources.

  Default rules are only seeded on first startup (when `alert_rules` is
  empty). Existing installations keep their tuned rules unchanged — operators
  who want the new defaults can either delete an existing rule and let
  `EnsureDefaultRules` reseed, or recreate them by hand from the Admin UI.

### Tests
- New unit tests for `icmp_flood` / `udp_flood` (`EvalProtocolFlood` routing),
  `connection_flood`, and `cleanupCooldown`.

## [1.3.0] - 2026-04-08

### Added
- **Live Threats page** (`/live`, gated by `FEATURE_ALERTS`) — pre-trigger view
  of the top inbound destinations from `traffic_by_dst_1min`. Shows real-time
  bps, pps, SYN/sec and unique source IP counts, evaluated against every
  active alert rule. Each row gets a status (`ok` / `warn ≥50%` / `critical
  ≥100%`) and the name of the closest matching rule, so operators can spot a
  building DDoS *before* the rule actually fires. Auto-refreshes every 10s
  with selectable window (1m / 5m / 15m / 1h).
- New API endpoint: `GET /api/v1/threats/live?window=300&limit=50`
- New `LiveThreats` store query: a single aggregating SQL pass over
  `traffic_by_dst_1min` with the local-prefix filter.

### Fixed
- **Alert engine never matched IPv4 destinations**: `buildCIDRFilter` was
  building expressions of the form
  `isIPAddressInRange(toString(dst_ip), '85.208.144.0/22')`, but ClickHouse
  stores IPv4 in `IPv6` columns and `toString()` returns the IPv4-mapped form
  (`::ffff:85.208.145.137`). `isIPAddressInRange("::ffff:1.2.3.4", "1.2.3.0/24")`
  returns `0`, so every alert rule that fed `localPrefixes` (i.e. all of them)
  silently dropped every IPv4 row and could never trigger on IPv4 traffic.

  The helper now normalizes any input CIDR to the IPv6-mapped equivalent
  before passing it to ClickHouse: `1.2.3.0/24` → `::ffff:1.2.3.0/120`
  (24 + 96 host bits). IPv6 CIDRs are passed through unchanged. Bare IPs are
  expanded to `/128`. This restores volume_in / syn_flood / amplification /
  port_scan / volume_out evaluations on the IPv4 side, and is also what the
  new `LiveThreats` query depends on.

## [1.2.2] - 2026-04-08

### Fixed
- OIDC role mapping: every Azure AD user was being mapped to `viewer` because
  the callback only recognised the literal role names `admin` / `admins`. The
  Azure AD App Role used in production is named `Admin.All`, so no user had the
  admin role and all `/admin/{rules,webhooks,audit}` requests returned 403.
  The callback now grants admin to any user whose `roles` (or `groups`) claim
  contains `Admin.All`.

  After upgrading, **existing sessions keep their old role until the user logs
  out and back in** — the role is captured at session creation time.

## [1.2.1] - 2026-04-08

### Fixed
- `/top/port` returned HTTP 500 (`Unknown identifier 't.direction'`) whenever the
  frontend passed a `direction` filter. `TopPorts` and `TopProtocols` were calling
  the shared `buildDirectionFilter` / `buildLinkFilter` helpers, which emit
  `t.direction` and `t.link_tag`, but their `FROM traffic_by_port` clause did not
  define an alias. Added the missing `t` alias and qualified the columns
  consistently in both queries.
- `migrations/000008_hot_aggregates.up.sql`: replaced infix bitwise `&` with
  `bitAnd()` (ClickHouse SQL has no `&` operator) and qualified the column in the
  `sumIf(packets * sampling_rate, ...)` argument as `flows_raw.packets` so that
  ClickHouse no longer interprets it as a reference to the `AS packets` alias of
  the surrounding `sum()` (which produced `ILLEGAL_AGGREGATION`). Existing
  installations applied via the docker-entrypoint init mechanism are unaffected;
  fresh deploys (or sites that apply migration 000008 manually post v1.2.0) need
  this fix to create `traffic_by_dst_1min_mv`.

## [1.2.0] - 2026-04-08

### Added — Optional features (off by default, behind feature flags)
- **`FEATURE_FLOW_SEARCH`** — Forensic flow log: keeps full per-tuple flow records (src/dst IP+port, protocol, TCP flags) in `flows_log` for 180 days. Includes:
  - `/flows/search` API with filters for src/dst IP (single or CIDR), AS, protocol, port, link, min bytes, IP version, time range
  - CSV export with hard cap at 100k rows
  - `/flows/timeseries` drill-down endpoint
  - "Flow Search" page in the UI with comprehensive filter form
  - "View flows" button on IP Detail and AS Detail pages (cross-page drill-down)
  - Bloom-filter skip indexes on `src_ip`/`dst_ip` for fast forensic queries
- **`FEATURE_PORT_STATS`** — Aggregated port-level statistics:
  - `/top/protocol` and `/top/port` endpoints
  - "Top Protocols" and "Top Ports" UI pages with direction toggle, protocol filter, service name resolution
  - `traffic_by_port` table (5-min buckets, 1-year retention)
- **`FEATURE_ALERTS`** — DDoS detection engine + Alerts dashboard:
  - Background goroutine in the collector evaluating configurable rules every 30s
  - Built-in rule types: `volume_in`, `volume_out`, `syn_flood`, `amplification`, `port_scan`
  - Hot pre-aggregated tables (`traffic_by_dst_1min`, `traffic_by_src_1min`) with HyperLogLog sketches for unique source/port counting
  - Default rules seeded on first startup (high inbound, critical inbound, SYN flood, amplification, port scan, high outbound)
  - LOCAL_AS prefix filter — only alerts on IPs in announced prefixes
  - Alert lifecycle: active → acknowledged → resolved with auto-resolve for stale alerts
  - In-memory cooldown tracker to avoid re-alert spam
  - "Alerts" UI page with severity summary cards, status tabs, per-alert actions
  - Alert badge in the header (auto-refresh every 30s, pulses red on critical)
  - Webhooks for Slack, Microsoft Teams, Discord, and generic JSON
  - Per-webhook minimum severity filter
  - BGP blackhole stub (`internal/bgp/`) — `NoopBlocker` ships in phase 1, ExaBGP/GoBGP backends planned for phase 2
  - Audit log (`audit_log` table) of all sensitive actions with user, IP, action, params, result
- **Admin UI** — unified `/admin` page with tabs for Links / Alert Rules / Webhooks / Audit Log, all gated by feature flags and admin role

### Added
- Flow collector: NetFlow v5, v9, IPFIX, sFlow v5 parsing
- ClickHouse storage with materialized views for traffic aggregation
- REST API with endpoints for top AS/IP/prefix, time series, search
- React frontend with dark-first NOC-inspired theme
- OIDC authentication with PKCE and RBAC (admin/viewer)
- CSRF protection (double-submit cookie)
- IP x AS cross-reference queries
- Docker Compose setup for dev and production
- Multi-arch Docker images (amd64 + arm64) published to GHCR
- CI pipeline (Go lint/test/build, frontend lint/typecheck/build, Docker)
- Release workflow with auto changelog and binary artifacts
- Dependabot for Go, npm, Docker, and GitHub Actions
- Security hardening: rate limiting, input validation, security headers
