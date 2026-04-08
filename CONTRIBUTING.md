# Contributing to AS-Stats

Thanks for your interest in contributing! This guide will help you get
started. For a quick architecture overview, see the [README](README.md);
for the full internal map, see [`CLAUDE.md`](CLAUDE.md).

## Getting Started

### Prerequisites

- Go 1.24+
- Node.js 22+
- Docker and Docker Compose v2
- (Optional but very helpful) `clickhouse-client` to validate SQL changes
  against a real ClickHouse before committing

### Development Setup

```bash
# Clone the repository
git clone https://github.com/nextmap-io/as-stats.git
cd as-stats

# Start ClickHouse (the only infrastructure dependency)
make docker-up

# Install frontend dependencies
cd frontend && npm ci && cd ..

# Run the three processes (each in its own terminal)
make run-collector   # Terminal 1
make run-api         # Terminal 2
make frontend-dev    # Terminal 3
```

The UI is at http://localhost:5173, the API at http://localhost:8080.

### Running Tests

```bash
# Go tests
make test

# Frontend lint + typecheck
make frontend-lint
make frontend-typecheck

# Run all CI checks locally
make ci
```

## How to Contribute

### Reporting Bugs

Open an issue using the **Bug Report** template. Include:

- Steps to reproduce
- Expected vs actual behavior
- Flow protocol (NetFlow v5/v9, IPFIX, sFlow)
- Router vendor/model if relevant

### Suggesting Features

Open an issue using the **Feature Request** template. Describe:

- The use case (what problem does it solve?)
- Proposed behavior
- Alternatives you've considered

### Submitting Code

1. **Fork** the repository
2. **Create a branch** from `main`: `git checkout -b feat/my-feature`
3. **Make your changes** following the guidelines below
4. **Test**: `make ci` must pass
5. **Commit** with a clear message (see Commit Messages below)
6. **Push** and open a **Pull Request** against `main`

### Branch Naming

| Type | Pattern | Example |
|------|---------|---------|
| Feature | `feat/short-description` | `feat/ipv6-prefix-aggregation` |
| Bug fix | `fix/short-description` | `fix/sflow-sampling-rate` |
| Docs | `docs/short-description` | `docs/deployment-guide` |
| Refactor | `refactor/short-description` | `refactor/query-builder` |

### Commit Messages

Use clear, imperative-mood messages:

```
Add sFlow counter sample support

Parse counter samples from sFlow v5 datagrams to extract
interface statistics (octets, packets, errors, discards).
```

- First line: concise summary (50 chars max, imperative mood)
- Blank line, then optional body with context
- Reference issues: `Fixes #42` or `Closes #42`

## Code Guidelines

### Go

- Follow standard Go conventions (`gofmt`, `go vet`).
- All exported functions must have doc comments.
- Handle all errors — `golangci-lint` enforces this. `_ =` only for
  intentional discards (with a comment explaining why).
- Add tests for new parsers, handlers, and store queries.
- Use parameterised queries for ClickHouse (`clickhouse.Named()`),
  **never** string concatenation for user input.
- In any query that aggregates a column it also filters on, alias the
  table (`FROM flows_log f`) and qualify column references in the
  `WHERE` / `GROUP BY` (`f.ts`) — ClickHouse alias scoping otherwise
  shadows the source column with the aggregate alias and throws
  `Aggregate function ... is found in WHERE`.

### TypeScript / React

- ESLint must pass (`npm run lint`).
- TypeScript strict mode — no `any` types.
- Use TanStack Query for data fetching (no raw `useEffect` for API
  calls).
- Components in `components/`, pages in `pages/`, hooks in `hooks/`.
- Cards: always use `<CardHeader> + <CardTitle> + <CardContent>` from
  `components/ui/card.tsx` for consistent vertical baseline alignment.
- Filters belong in URL search params, not component state — use
  `useFilters()`.

### SQL (ClickHouse)

- New tables go in their own numbered migration file
  (`migrations/NNNNNN_topic.up.sql` + `.down.sql`). Don't append to old
  migrations.
- Use `SummingMergeTree` for aggregation tables, `AggregatingMergeTree`
  if you need state functions like HyperLogLog.
- Always include a `TTL` for data retention.
- IPv4 is stored as IPv4-mapped IPv6 (`::ffff:1.2.3.4`). Always normalise
  IPv4 CIDRs to their mapped form before passing them to
  `isIPAddressInRange` — see `normalizeCIDRForIPv6Column()` in
  `internal/store/alert_queries.go`.
- ClickHouse SQL has **no infix bitwise operator** — use `bitAnd(a, b)`
  rather than `a & b`.
- Test queries with `clickhouse-client` against a real ClickHouse
  instance before committing.

### Docker

- Multi-stage builds, Alpine base images
- Use `TARGETARCH` for cross-compilation

## Architecture Overview

```
Routers --UDP--> Collector (Go) --batch--> ClickHouse
                                                |
                                       API Server (Go) --JSON--> Frontend (React)
                                                |
                              optional: alert engine reads hot 1-min tables
```

| Directory | Purpose |
|---|---|
| `cmd/collector/` | Flow collector entry point — wires the pipeline and starts the alert engine if `FEATURE_ALERTS=true` |
| `cmd/api/` | API server entry point |
| `internal/collector/` | Flow parsers (NetFlow v5/v9, IPFIX, sFlow), enricher, batch writer |
| `internal/api/` | HTTP handlers, middleware (auth, CSRF, cache, audit), router |
| `internal/store/` | ClickHouse read/write layer — `reader.go`, `flow_log.go`, `threats.go`, `alerts.go`, `alert_queries.go` |
| `internal/alerts/` | Alert engine (rule loop, cooldown, top-source enrichment, default-rule seeding) |
| `internal/bgp/` | BGP `Blocker` interface — Noop ships, real impl is phase 2 |
| `internal/ripestat/` | Local-prefix discovery via the RIPEstat API |
| `internal/services/` | IANA protocol + well-known port name resolution |
| `internal/model/` | Shared data types — TS mirror lives in `frontend/src/lib/types.ts` |
| `internal/config/` | Environment-based configuration |
| `frontend/src/` | React application — see [`frontend/README.md`](frontend/README.md) |
| `migrations/` | ClickHouse DDL, numbered 000001–000009 (last three are feature-gated) |

## Review Process

- All PRs require at least 1 approving review
- CI must be green (lint, test, build, Docker)
- Squash merge into `main`
- Branch is auto-deleted after merge

## Need Help?

- Open a **Discussion** for questions
- Check existing issues before creating new ones
- Tag your issue with appropriate labels

## License

By contributing, you agree that your contributions will be licensed under the [MIT License](LICENSE).
