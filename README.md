<h1 align="center">AS-Stats</h1>

<p align="center">
  A modern, production-grade NetFlow / sFlow / IPFIX collector and traffic analytics platform.<br>
  <em>Per-AS, per-IP, per-prefix statistics with a NOC-grade web UI, optional DDoS detection,
  and a forensic flow log — all in three containers.</em>
</p>

<p align="center">
  <a href="https://github.com/nextmap-io/as-stats/releases"><img src="https://img.shields.io/github/v/release/nextmap-io/as-stats?label=release&color=blue" alt="release"></a>
  <a href="https://github.com/nextmap-io/as-stats/actions/workflows/ci.yml"><img src="https://github.com/nextmap-io/as-stats/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/nextmap-io/as-stats/blob/main/LICENSE"><img src="https://img.shields.io/github/license/nextmap-io/as-stats?color=green" alt="license"></a>
  <a href="https://github.com/nextmap-io/as-stats/pkgs/container/as-stats-collector"><img src="https://img.shields.io/badge/container-ghcr.io-blue?logo=docker" alt="container"></a>
</p>

---

AS-Stats is the spiritual successor to the venerable Perl
[AS-Stats](https://github.com/manuelkasper/AS-Stats). Same idea — point your
routers at it and answer "where is my traffic going?" — but rebuilt around a
modern stack so it scales from a homelab to a multi-Tbps transit network
without falling over.

## Highlights

| | |
|---|---|
| **Flow protocols** | NetFlow v5 / v9, IPFIX, sFlow v5 — all parsed in pure Go, no external decoders |
| **Storage** | ClickHouse with materialised views: 5-min → hourly → daily rollups, automatic TTL |
| **Throughput** | 100k+ flows/sec on a 4-vCPU box with batched writes and IRQ-balanced pipelines |
| **IPv4 / IPv6** | First-class IPv6 — every chart and table can be split by address family |
| **Per-link** | Map router SNMP interfaces to named "links" with custom colors and capacity, get true per-uplink graphs |
| **Search** | Type an IP, an ASN, an AS name or a prefix — auto-routing to the right view |
| **Reverse DNS** | PTR records resolved on the fly and shown inline in IP tables |
| **Auth** | Optional OIDC (PKCE) with admin / viewer RBAC — works with Azure AD, Authentik, Keycloak, Google, etc. |
| **Deployment** | Three Docker images, one Compose file, one `.env` |

### Optional features (off by default)

Three independent feature flags add capability without bloating the base
install. Each one creates its own ClickHouse tables only when enabled.

- **`FEATURE_FLOW_SEARCH`** — forensic per-tuple flow log with bloom-filter
  indexes on src/dst IP. Search by IP, CIDR, AS, port, protocol, flags;
  CSV export capped at 100k rows. 180-day retention by default.

- **`FEATURE_PORT_STATS`** — Top Protocols and Top Ports views, IANA service
  name resolution, direction toggle.

- **`FEATURE_ALERTS`** — DDoS detection engine + Alerts dashboard +
  Live Threats real-time view + webhooks (Slack / Teams / Discord / generic)
  + audit log + admin UI for rule and webhook management.
  Ships with **10 default rules** covering volumetric, SYN flood,
  reflection / amplification, ICMP / UDP flood, connection-rate flood, port
  scan from internal hosts, and slow outbound exfiltration.

## Architecture

```
┌──────────┐                                          ┌────────────┐
│ Routers  │ ── UDP 2055 / 6343 ─────────►            │ Collector  │
└──────────┘   NetFlow / IPFIX / sFlow                │   (Go)     │
                                                      └─────┬──────┘
                                                            │ batched
                                                            │ INSERT
                                                            ▼
                                                      ┌────────────┐
                              ┌────────── reads ────► │ ClickHouse │
                              │                       └─────┬──────┘
                              │                             │
                              │  optional: alert engine     │
                              │  reads hot 1-min tables ◄───┘
                              │
                        ┌─────┴────┐
                        │ API (Go) │ ◄─── REST JSON
                        └─────┬────┘
                              │
                        ┌─────┴────┐
                        │ Frontend │ ◄─── HTTPS via your reverse proxy
                        │  (React) │
                        └──────────┘
```

The collector is a single-binary Go service with a backpressure-aware
channel pipeline (UDP listener → decoder workers → enricher → batch writer).
The API server is another Go binary on `chi` with an in-memory response
cache. The frontend is a React SPA built with Vite, served by nginx in
production.

## Quick Start

> Pre-built multi-arch (amd64 + arm64) images are published to GHCR on every
> tagged release. No build step required.

### 1. Prerequisites

- Linux host with Docker 24+ and Compose v2
- UDP/2055 (NetFlow / IPFIX) and UDP/6343 (sFlow) reachable from your routers
- A reverse proxy that terminates TLS (Caddy, nginx, Traefik — all fine)

### 2. Deploy

```bash
git clone https://github.com/nextmap-io/as-stats.git
cd as-stats

cp .env.example .env
$EDITOR .env   # set CLICKHOUSE_PASSWORD, LOCAL_AS, API_CORS_ORIGINS

docker compose -f docker-compose.ghcr.yml up -d
```

That's it. Three containers come up: ClickHouse, the collector, and the
API+frontend pair. The frontend listens on `127.0.0.1:8081` by default —
put your reverse proxy in front of it.

### 3. Reverse proxy

Caddyfile example:

```caddy
as-stats.example.com {
    reverse_proxy 127.0.0.1:8081
    encode gzip
}
```

### 4. Point a router at the collector

**Junos (NetFlow v9, inline-jflow)**:

```
set forwarding-options sampling instance AS-STATS input rate 1000
set forwarding-options sampling instance AS-STATS family inet output flow-server <COLLECTOR_IP> port 2055
set forwarding-options sampling instance AS-STATS family inet output flow-server <COLLECTOR_IP> autonomous-system-type origin
set forwarding-options sampling instance AS-STATS family inet output inline-jflow source-address <LOOPBACK>
```

**Cisco IOS-XE (Flexible NetFlow v9)**:

```
flow record AS-STATS-RECORD
 match ipv4 source address
 match ipv4 destination address
 match interface input
 match interface output
 match flow direction
 collect counter bytes long
 collect counter packets long
 collect routing source as 4-octet
 collect routing destination as 4-octet

flow exporter AS-STATS-EXPORT
 destination <COLLECTOR_IP>
 transport udp 2055
 source Loopback0

flow monitor AS-STATS-MONITOR
 exporter AS-STATS-EXPORT
 record AS-STATS-RECORD

interface <UPLINK>
 ip flow monitor AS-STATS-MONITOR input
 ip flow monitor AS-STATS-MONITOR output
```

**MikroTik RouterOS 7 (Traffic Flow / IPFIX)**:

```
/ip traffic-flow set enabled=yes interfaces=<UPLINK>
/ip traffic-flow target add dst-address=<COLLECTOR_IP> port=2055 version=ipfix
```

### 5. Map your interfaces

Open the UI → **Links** → **Add link** to bind a router IP + SNMP ifindex
to a human-friendly tag. The collector reloads link config every 60 seconds,
no restart needed.

You can find the SNMP ifindex with `show snmp mib ifmib ifindex <interface>`
on Cisco, `show snmp mib walk` on Junos, or `/interface print detail` on
MikroTik.

## Configuration

Everything is environment-variable driven. The full list lives in
[`.env.example`](.env.example); the highlights are:

### Core

| Variable | Default | Description |
|---|---|---|
| `CLICKHOUSE_PASSWORD` | *required* | Strong password for the `asstats` user |
| `COLLECTOR_LISTEN_NETFLOW` | `:2055` | NetFlow / IPFIX UDP listen address |
| `COLLECTOR_LISTEN_SFLOW` | `:6343` | sFlow UDP listen address |
| `COLLECTOR_BATCH_SIZE` | `10000` | Flows per batch INSERT |
| `COLLECTOR_FLUSH_INTERVAL` | `5s` | Force-flush timer (whichever fires first) |
| `COLLECTOR_WORKERS` | `4` | Decoder goroutines (raise on multi-Gbps) |
| `LOCAL_AS` | *(none)* | Your AS number — auto-fetches your prefixes from RIPEstat at startup so the engine knows which IPs are "yours" |
| `API_CORS_ORIGINS` | `http://localhost:5173` | Comma-separated allowed origins |

### Auth (optional)

| Variable | Default | Description |
|---|---|---|
| `AUTH_ENABLED` | `false` | Master switch for OIDC |
| `OIDC_ISSUER_URL` | | e.g. `https://login.microsoftonline.com/<tenant>/v2.0` |
| `OIDC_CLIENT_ID` | | App ID from your IdP |
| `OIDC_CLIENT_SECRET` | | Client secret |
| `OIDC_REDIRECT_URL` | | `https://your-domain/auth/callback` |

The OIDC callback grants the **admin** role to any user whose `roles` (or
`groups`) claim contains `Admin.All`. Anything else maps to **viewer**.

### Optional features

| Variable | Default | What it enables |
|---|---|---|
| `FEATURE_FLOW_SEARCH` | `false` | Forensic flow log + Search UI + CSV export |
| `FLOW_LOG_RETENTION_DAYS` | `180` | Applied via `ALTER TABLE` on collector startup, idempotent |
| `FEATURE_PORT_STATS` | `false` | Top Protocols + Top Ports views |
| `FEATURE_ALERTS` | `false` | DDoS detection engine + Alerts + Live Threats + Admin UI |
| `ALERT_EVAL_INTERVAL` | `30s` | How often the alert engine evaluates rules |
| `ALERT_STALE_THRESHOLD` | `5m` | Active alerts auto-resolve after this gap of silence |

> **Note**: enabling a feature flag for the first time on an existing
> installation requires running the corresponding numbered migration in
> `migrations/` once. Fresh deploys pick them up automatically via the
> ClickHouse `docker-entrypoint-initdb.d` mechanism.

## Alert Engine

When `FEATURE_ALERTS=true`, the collector spawns a goroutine that evaluates
configurable rules every 30 seconds against pre-aggregated 1-minute hot
tables. **Detection scales independently of `flows_raw`** — even on
high-volume networks, every rule resolves in milliseconds.

### Rule types

| Type | What it detects |
|---|---|
| `volume_in` | bps / pps received by a single destination |
| `volume_out` | bps / pps sent from a single source (compromised host) |
| `syn_flood` | TCP SYN-only packet rate to a destination — TCP state-table abuse |
| `amplification` | Many unique source IPs hitting one destination, with optional sustained-bps floor to filter scanners |
| `port_scan` | An internal source touching many distinct destination ports |
| `icmp_flood` | ICMP packet rate to a destination — almost never legitimate at high rates |
| `udp_flood` | UDP packet rate to a destination — DNS / NTP query flood signature |
| `connection_flood` | High distinct flow count per destination — Slowloris / half-open scan |

Every rule is bound to a **target filter** — by default the local AS
prefixes loaded from RIPEstat — so alerts only fire on IPs you actually
operate. Each triggered alert is enriched with the **top 5 source IPs**
hammering the target, pulled from `flows_raw` at trigger time so the
on-call engineer doesn't need to dig further.

### Live Threats view

The Alerts dashboard shows what already fired. The **Live Threats** page
(`/live`) shows what's *building*: a real-time, auto-refreshing snapshot of
the top destinations from the alert engine's hot tables, evaluated against
every active rule and color-coded by how close each row is to triggering.

```
 Status   Destination           bps        pps      SYN/s    Unique src   Worst rule
 64%      192.0.2.10            3.6 Mbps   450 pps  —        6,405        Reflection/amplification
 OK       192.0.2.225           13.6 Mbps  1.2 Kpps —        737          Reflection/amplification
 OK       192.0.2.8             93.4 Mbps  15 Kpps  —        25           High inbound volume
```

Sortable columns, configurable window (1m / 5m / 15m / 1h), 10-second
auto-refresh.

### BGP blackhole hook

The `/alerts/{id}/block` endpoint calls a `Blocker` interface defined in
`internal/bgp/`. Phase 1 ships a `NoopBlocker` that just logs — wire in your
own implementation (ExaBGP, GoBGP, vendor-specific REST) to push RFC 7999
blackhole communities to your edge.

## Hardware sizing

| Traffic level | Flows / sec | CPU | RAM | Disk (1 year, base install) |
|---|---|---|---|---|
| Small (< 1 Gbps) | < 5k | 2 vCPU | 4 GB | 50 GB |
| Medium (1–10 Gbps) | 5k–50k | 4 vCPU | 8 GB | 200 GB |
| Large (10–100 Gbps) | 50k–500k | 8 vCPU | 16 GB | 1 TB |
| Very large (100+ Gbps) | 500k+ | 16 vCPU | 32 GB | 2+ TB |

ClickHouse will happily eat most of the available RAM — set
`deploy.resources.limits.memory` in your compose override to cap it. SSDs
are strongly recommended for query performance even at small sizes.

`FEATURE_FLOW_SEARCH` adds significant storage: budget roughly **+250 GB
per 6 months per Gbps** at 1:1000 sampling. The other two flags are
lightweight (a few MB to a few GB per day).

### Default retention

| Table | Granularity | Retention |
|---|---|---|
| `flows_raw` | per flow | 7 days |
| `traffic_by_as` / `traffic_by_link` | 5 min | 90 days |
| `*_hourly` rollups | 1 hour | 2 years |
| `*_daily` rollups | 1 day | 5 years |
| `traffic_by_ip` | 5 min | 14 days |
| `traffic_by_prefix` | 5 min | 30 days |
| `flows_log` *(opt)* | per tuple | 180 days *(configurable)* |
| `traffic_by_dst_1min` / `traffic_by_src_1min` *(opt)* | 1 min | 7 days |

All TTLs are enforced by ClickHouse itself — no cron jobs to maintain.

## Networking notes

The collector runs in **`network_mode: host`** so it sees the real source
IP of routers. Without that, Docker NAT replaces every router IP with the
bridge gateway and link enrichment breaks.

Some Cisco IOS-XE images send NetFlow with **source port 0**, which Linux
conntrack drops by default. Add a `NOTRACK` rule:

```bash
ip6tables -t raw -I PREROUTING -s <ROUTER_IP> -p udp --dport 2055 -j NOTRACK
```

Persist it via `/etc/ufw/before6.rules` or `networkd-dispatcher` — see
[`docs/`](docs/) for distro-specific recipes.

## API

All endpoints under `/api/v1/`. Common query params: `period`
(`1h` / `3h` / `6h` / `24h` / `7d` / `30d`), `ip_version` (`4` / `6`),
`scope` (`internal` / `external`), `link`, `direction`, `limit`, `offset`.

### Always available

| Method | Path | Description |
|---|---|---|
| `GET` | `/overview` | Dashboard counters |
| `GET` | `/status` | Active routers, flow rate, DB size |
| `GET` | `/top/as` | Top ASes by volume |
| `GET` | `/top/ip` | Top IPs (filterable to internal / external) |
| `GET` | `/top/prefix` | Top prefixes (same scopes) |
| `GET` | `/as/{asn}` | AS detail with v4 / v6 charts and top peers |
| `GET` | `/ip/{ip}` | IP detail with sub-5-min resolution on short windows |
| `GET` | `/links` | Per-link traffic summary |
| `GET` | `/link/{tag}` | Link detail with top AS chart |
| `GET` | `/dns/ptr?ip=X` | Reverse DNS lookup |
| `GET` | `/search?q=X` | Search by AS / IP / prefix / name |
| `GET` | `/features` | Feature flag discovery (used by the frontend) |

### Feature-gated

Only registered when the corresponding `FEATURE_*` flag is set.

| Feature | Endpoints |
|---|---|
| `FLOW_SEARCH` | `GET /flows/search` (with `?format=csv` for export, max 100k rows) · `GET /flows/timeseries` |
| `PORT_STATS` | `GET /top/protocol` · `GET /top/port` |
| `ALERTS` | `GET /alerts` · `GET /alerts/summary` · `GET /threats/live` · `POST /alerts/{id}/{ack,resolve}` · `POST /alerts/{id}/block` · `GET/POST/PUT/DELETE /admin/{rules,webhooks}` · `GET /admin/audit` |

## Tech stack

| Layer | Choice | Why |
|---|---|---|
| Collector | Go, custom parsers | Pure Go, no CGo, predictable GC, easy cross-compile |
| API | Go, [chi](https://github.com/go-chi/chi) | Stdlib `http.Handler`, no magic, fast |
| Storage | [ClickHouse](https://clickhouse.com) | Columnar, materialised views, brutal compression |
| Cache | In-process, 30s TTL | No Redis to operate |
| Frontend | React 19, TypeScript, Vite, [TanStack Query](https://tanstack.com/query) | Modern, typed end-to-end |
| Charts | [Recharts](https://recharts.org) | Composable, themable, no D3 in app code |
| UI | [Tailwind](https://tailwindcss.com), JetBrains Mono | Dark-first NOC theme |
| Auth | OIDC + PKCE via [coreos/go-oidc](https://github.com/coreos/go-oidc) | Works with every modern IdP |
| Container | Multi-arch Docker (amd64 + arm64) on GHCR | One image, runs anywhere |
| CI/CD | GitHub Actions | Lint, test, build, push, release on tag |

## Development

```bash
make docker-up           # ClickHouse only
make run-collector       # Terminal 1
make run-api             # Terminal 2
cd frontend && npm run dev   # Terminal 3
```

UI on http://localhost:5173, API on http://localhost:8080.

```bash
make ci   # everything CI runs: Go lint + test + build, frontend lint + typecheck + build
```

See [`CONTRIBUTING.md`](CONTRIBUTING.md) for branch naming, commit
conventions and the full PR workflow, and [`CLAUDE.md`](CLAUDE.md) for the
internal architecture deep-dive used by automated agents.

## License

[MIT](LICENSE).
