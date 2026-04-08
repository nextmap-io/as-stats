# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
