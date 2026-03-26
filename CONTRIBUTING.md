# Contributing to AS-Stats

Thanks for your interest in contributing! This guide will help you get started.

## Getting Started

### Prerequisites

- Go 1.24+
- Node.js 22+
- Docker and Docker Compose

### Development Setup

```bash
# Clone the repository
git clone https://github.com/nextmap-io/as-stats.git
cd as-stats

# Start infrastructure (ClickHouse + Redis)
make docker-up

# Install frontend dependencies
cd frontend && npm ci && cd ..

# Run everything
make run-collector  # Terminal 1
make run-api        # Terminal 2
make frontend-dev   # Terminal 3
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

- Follow standard Go conventions (`gofmt`, `go vet`)
- All exported functions must have doc comments
- Handle all errors — `golangci-lint` enforces this
- Add tests for new parsers and handlers
- Use parameterized queries for ClickHouse (never string concatenation)

### TypeScript / React

- ESLint must pass (`npm run lint`)
- TypeScript strict mode — no `any` types
- Use TanStack Query for data fetching (no raw `useEffect` for API calls)
- Components in `components/`, pages in `pages/`, hooks in `hooks/`

### SQL (ClickHouse)

- Add new tables and materialized views to `migrations/000001_init_schema.up.sql`
- Use `SummingMergeTree` for aggregation tables
- Always include a TTL for data retention
- Test queries with `clickhouse-client` before committing

### Docker

- Multi-stage builds, Alpine base images
- Use `TARGETARCH` for cross-compilation

## Architecture Overview

```
Routers --UDP--> Collector (Go) --batch--> ClickHouse
                                                |
                                          API Server (Go) --JSON--> Frontend (React)
```

| Directory | Purpose |
|-----------|---------|
| `cmd/collector/` | Flow collector entry point |
| `cmd/api/` | API server entry point |
| `internal/collector/` | Flow parsers (NetFlow, sFlow), enricher, batch writer |
| `internal/api/` | HTTP handlers, middleware, router |
| `internal/store/` | ClickHouse read/write layer |
| `internal/model/` | Shared data types |
| `internal/config/` | Environment-based configuration |
| `frontend/src/` | React application |
| `migrations/` | ClickHouse schema |

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
