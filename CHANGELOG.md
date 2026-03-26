# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
