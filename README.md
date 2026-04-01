# AS-Stats Modern

A modern, production-ready replacement for [AS-Stats](https://github.com/manuelkasper/AS-Stats), built with Go, ClickHouse, and React.

Collects NetFlow/sFlow from routers and generates per-AS, per-IP, and per-prefix traffic statistics with a modern web UI.

## Architecture

```
┌─────────────┐     UDP (NetFlow/sFlow)     ┌──────────────────┐
│   Routers   │ ──────────────────────────►  │  Collector (Go)  │
└─────────────┘                              │  - Flow parser   │
                                             │  - AS enrichment │
                                             │  - Batch writer  │
                                             └────────┬─────────┘
                                                      │ Batch INSERT
                                                      ▼
                                             ┌──────────────────┐
                                             │    ClickHouse    │
                                             │  - flows_raw     │
                                             │  - mat. views    │
                                             └────────┬─────────┘
                                                      │
                                             ┌────────┴─────────┐
                                             │  API Server (Go) │
                                             │  - REST endpoints│
                                             │  - In-memory cache│
                                             └────────┬─────────┘
                                                      │
                                             ┌────────┴─────────┐
                                             │ Frontend (React) │
                                             │  - Dashboard     │
                                             │  - Top AS/IP     │
                                             │  - Graphs        │
                                             └──────────────────┘
```

## Features

- **Flow Collection**: NetFlow v5, v9, IPFIX, and sFlow v5
- **High Performance**: Handles 100k+ flows/sec with batched writes
- **ClickHouse Storage**: Pre-aggregated materialized views (5min → hourly → daily)
- **IPv4/IPv6 Split**: Separate graphs and stats per IP version
- **Per-Link Breakdown**: Traffic split by transit/peering link with custom colors
- **95th Percentile**: P95 lines on all charts, capacity utilization on links
- **LOCAL_AS Enrichment**: Auto-fetch announced prefixes from RIPEstat, remap private ASes
- **Smart Search**: AS number, IP address, or name — auto-redirect
- **Reverse DNS**: PTR records displayed inline on IP tables
- **REST API**: Full-featured with filtering, pagination, search, and in-memory cache
- **Modern UI**: React with dark/light mode, expandable charts, responsive tables
- **OIDC Auth**: Optional OpenID Connect authentication with RBAC
- **Docker Ready**: Single-file deployment with pre-built images

## Quick Start (Production)

### 1. Prerequisites

- Linux server with Docker 24+ and Compose v2
- UDP ports 2055 (NetFlow) and 6343 (sFlow) open
- A reverse proxy for HTTPS (Caddy recommended)

### 2. Deploy

```bash
git clone https://github.com/nextmap-io/as-stats.git
cd as-stats

# Configure
cp .env.example .env
# Edit .env: set CLICKHOUSE_PASSWORD, API_CORS_ORIGINS, LOCAL_AS

# Start with pre-built images (no build required)
docker compose -f docker-compose.ghcr.yml up -d
```

### 3. Configure your reverse proxy

Example with Caddy (`/etc/caddy/Caddyfile`):

```
as-stats.example.com {
    reverse_proxy 127.0.0.1:8081
    encode gzip
}
```

### 4. Configure your routers

**Junos (NetFlow v9)**:
```
set forwarding-options sampling instance AS-STATS input rate 1
set forwarding-options sampling instance AS-STATS family inet output flow-server <COLLECTOR_IP> port 2055
set forwarding-options sampling instance AS-STATS family inet output flow-server <COLLECTOR_IP> autonomous-system-type origin
set forwarding-options sampling instance AS-STATS family inet output inline-jflow source-address <LOOPBACK_IP>
```

**Cisco IOS-XE (NetFlow v9)**:
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
 collect ipv4 source prefix
 collect ipv4 destination prefix

flow exporter AS-STATS-EXPORT
 destination <COLLECTOR_IP>
 source Loopback0
 transport udp 2055

flow monitor AS-STATS-MONITOR
 exporter AS-STATS-EXPORT
 record AS-STATS-RECORD

interface <UPLINK>
 ip flow monitor AS-STATS-MONITOR input
 ip flow monitor AS-STATS-MONITOR output
```

### 5. Configure links

Open the web UI → **Links** page → **Add link** to map router SNMP interfaces to named links.

You need: router IP, SNMP ifindex (from `show snmp mib ifmib ifindex <interface>`), and a tag name.

## Configuration

All settings via environment variables. Key options:

| Variable | Default | Description |
|----------|---------|-------------|
| `CLICKHOUSE_PASSWORD` | *required* | Strong password for ClickHouse |
| `COLLECTOR_LISTEN_NETFLOW` | `:2055` | NetFlow/IPFIX UDP listen |
| `COLLECTOR_LISTEN_SFLOW` | `:6343` | sFlow UDP listen |
| `COLLECTOR_BATCH_SIZE` | `10000` | Flows per batch write |
| `LOCAL_AS` | *(none)* | Your AS number — auto-fetches prefixes from RIPEstat to remap private ASes |
| `API_CORS_ORIGINS` | `http://localhost:5173` | Allowed CORS origins (set to your domain) |
| `AUTH_ENABLED` | `false` | Enable OIDC authentication |

See [`.env.example`](.env.example) for the full list.

## Hardware Sizing

| Traffic Level | Flows/sec | CPU | RAM | Disk (1 year) |
|---------------|-----------|-----|-----|---------------|
| Small (< 1 Gbps) | < 5k | 2 vCPU | 4 GB | 50 GB |
| Medium (1-10 Gbps) | 5k-50k | 4 vCPU | 8 GB | 200 GB |
| Large (10-100 Gbps) | 50k-500k | 8 vCPU | 16 GB | 1 TB |
| Very Large (100+ Gbps) | 500k+ | 16 vCPU | 32 GB | 2+ TB |

**Notes**:
- ClickHouse uses most of the RAM (configure `deploy.resources.limits.memory` in compose)
- Disk is mainly ClickHouse data — SSD recommended for query performance
- The collector is CPU-bound (flow parsing); the API is I/O-bound (ClickHouse queries)
- With `LOCAL_AS` set, the collector does additional prefix lookups per flow

### Data Retention (default)

| Table | Granularity | Retention |
|-------|-------------|-----------|
| `flows_raw` | Per flow | 3 days |
| `traffic_by_as` | 5 min | 90 days |
| `traffic_by_as_hourly` | 1 hour | 2 years |
| `traffic_by_as_daily` | 1 day | 5 years |
| `traffic_by_link` | 5 min | 90 days |
| `traffic_by_link_hourly` | 1 hour | 2 years |
| `traffic_by_link_daily` | 1 day | 5 years |
| `traffic_by_ip` | 5 min | 14 days |
| `traffic_by_prefix` | 5 min | 30 days |

## Networking Notes

### Collector and Docker NAT

The collector runs in **host networking mode** (`network_mode: host`) so it sees the real source IP of routers. Without this, Docker NAT replaces the router IP with a bridge IP, breaking link enrichment.

If your router sends NetFlow with **source port 0** (common on Cisco IOS-XE), Linux conntrack may drop these packets. Add a NOTRACK rule:

```bash
ip6tables -t raw -I PREROUTING -s <ROUTER_IP> -p udp --dport 2055 -j NOTRACK
```

Make it persistent by adding to `/etc/ufw/before6.rules` or a `networkd-dispatcher` script.

### Firewall

Required ports:
- **TCP 80/443** — HTTPS (reverse proxy)
- **UDP 2055** — NetFlow/IPFIX
- **UDP 6343** — sFlow
- **TCP 22** — SSH (management)

## Development Setup

```bash
# Start infrastructure
make docker-up

# Apply migrations
make migrate

# Terminal 1: collector
make run-collector

# Terminal 2: API
make run-api

# Terminal 3: frontend
cd frontend && npm run dev
```

UI: http://localhost:5173 | API: http://localhost:8080

Run all checks: `make ci`

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/overview` | Dashboard overview |
| GET | `/api/v1/status` | Collector status + DB info |
| GET | `/api/v1/top/as` | Top ASes by traffic |
| GET | `/api/v1/top/as/traffic` | Top ASes with time series |
| GET | `/api/v1/top/ip` | Top IPs (scope: all/internal/external) |
| GET | `/api/v1/top/prefix` | Top prefixes (scope: all/internal/external) |
| GET | `/api/v1/as/{asn}` | AS detail with IPv4/IPv6 charts |
| GET | `/api/v1/as/{asn}/ips` | Top IPs for an AS (scope: internal/external) |
| GET | `/api/v1/ip/{ip}` | IP detail with peers and AS info |
| GET | `/api/v1/links` | Links with traffic summary |
| GET | `/api/v1/links/traffic` | Link traffic time series (ip_version filter) |
| GET | `/api/v1/link/{tag}` | Link detail with top AS chart |
| GET | `/api/v1/dns/ptr?ip=X` | Reverse DNS lookup |
| GET | `/api/v1/search?q=X` | Search AS/IP/prefix |
| POST | `/api/v1/admin/links` | Create/update link config |
| DELETE | `/api/v1/admin/links/{tag}` | Delete link config |

Common query params: `period` (1h/3h/6h/24h/7d/30d), `ip_version` (4/6), `scope` (internal/external), `limit`, `offset`.

## Tech Stack

| Component | Technology |
|-----------|-----------|
| Collector | Go, custom NetFlow/sFlow/IPFIX parsers |
| API | Go, chi router, in-memory cache |
| Storage | ClickHouse (SummingMergeTree) |
| Frontend | React 19, TypeScript 6, Vite, Recharts |
| UI | Tailwind CSS, JetBrains Mono |
| Auth | OIDC (optional, coreos/go-oidc) |
| Deploy | Docker Compose, GHCR images |
| CI/CD | GitHub Actions |

## License

MIT
