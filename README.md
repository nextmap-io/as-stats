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
                                             │  - OIDC auth     │
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
- **ClickHouse Storage**: Pre-aggregated materialized views for fast queries
- **REST API**: Full-featured API with filtering, pagination, and search
- **Modern UI**: React + shadcn/ui with dark mode, real-time charts, and responsive design
- **OIDC Auth**: Optional OpenID Connect authentication with RBAC
- **Docker Ready**: Full Docker Compose setup for development and production

## Quick Start

### Prerequisites

- Go 1.24+
- Node.js 20+
- Docker and Docker Compose

### Development Setup

1. Start infrastructure:
   ```bash
   make docker-up
   ```

2. Apply database migrations:
   ```bash
   make migrate
   ```

3. Start the collector:
   ```bash
   make run-collector
   ```

4. Start the API server (in another terminal):
   ```bash
   make run-api
   ```

5. Start the frontend (in another terminal):
   ```bash
   make frontend-dev
   ```

The UI is available at http://localhost:5173, the API at http://localhost:8080.

### Configuration

All configuration is via environment variables. See [`.env.example`](.env.example) for the full list.

Key settings:

| Variable | Default | Description |
|----------|---------|-------------|
| `CLICKHOUSE_ADDR` | `localhost:9000` | ClickHouse address |
| `CLICKHOUSE_DATABASE` | `asstats` | Database name |
| `COLLECTOR_LISTEN_NETFLOW` | `:2055` | NetFlow/IPFIX UDP listen address |
| `COLLECTOR_LISTEN_SFLOW` | `:6343` | sFlow UDP listen address |
| `COLLECTOR_BATCH_SIZE` | `10000` | Flows per batch insert |
| `COLLECTOR_FLUSH_INTERVAL` | `5s` | Max time between batch writes |
| `API_LISTEN_ADDR` | `:8080` | API HTTP listen address |
| `AUTH_ENABLED` | `false` | Enable OIDC authentication |

### Production Deployment

```bash
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d
```

Ensure UDP ports 2055 and 6343 are open for flow reception.

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/overview` | Dashboard overview |
| GET | `/api/v1/top/as` | Top ASes by traffic |
| GET | `/api/v1/top/ip` | Top IPs by traffic |
| GET | `/api/v1/top/prefix` | Top prefixes by traffic |
| GET | `/api/v1/as/{asn}` | AS detail with time series |
| GET | `/api/v1/as/{asn}/peers` | AS peers |
| GET | `/api/v1/ip/{ip}` | IP detail with time series |
| GET | `/api/v1/links` | Known links with traffic |
| GET | `/api/v1/link/{tag}` | Link detail with top ASes |
| GET | `/api/v1/search?q=...` | Search AS, IP, prefix |

All endpoints support filters: `from`, `to`, `link`, `direction`, `limit`.

## Tech Stack

| Component | Technology |
|-----------|-----------|
| Collector | Go, custom UDP parsers |
| API | Go, chi router |
| Auth | OIDC (coreos/go-oidc) |
| Storage | ClickHouse |
| Cache | Redis (optional) |
| Frontend | React, TypeScript, Vite |
| UI Kit | shadcn/ui, Tailwind CSS |
| Charts | Recharts |
| Deploy | Docker Compose |

## License

MIT
