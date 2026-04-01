# Release Process

## Branch Strategy

- **`main`** — protected, requires PR with review. Always deployable.
- **`feature/*`** — feature branches, branched from main.
- **`fix/*`** — bug fix branches.
- **`release/vX.Y.Z`** — release preparation branches (optional).

### Rules

- **Never commit directly to `main`** — always use a PR.
- PRs must pass CI (Go lint/test/build + Frontend lint/typecheck/build).
- Squash merge for clean history.

## Creating a Release

### 1. Prepare

```bash
# Ensure main is up to date
git checkout main
git pull origin main

# Check CI status
gh run list --limit 1
```

### 2. Tag

```bash
# Semantic versioning: vMAJOR.MINOR.PATCH
git tag -a v1.0.0 -m "Release v1.0.0: initial production release"
git push origin v1.0.0
```

### 3. GitHub Release

The `release.yml` workflow triggers automatically on tag push and:
- Builds Go binaries for 4 platforms (linux/darwin × amd64/arm64)
- Builds Docker images (multi-arch: amd64 + arm64)
- Pushes to GHCR with version tags
- Creates a GitHub Release with changelog

Alternatively, create manually:

```bash
gh release create v1.0.0 \
  --title "v1.0.0" \
  --generate-notes
```

### 4. Deploy

On the production server:

```bash
cd /opt/as-stats
git pull
docker compose -f docker-compose.ghcr.yml pull
docker compose -f docker-compose.ghcr.yml up -d
```

Or with local build:

```bash
cd /opt/as-stats
git pull
docker compose -f docker-compose.yml -f docker-compose.prod.yml -f docker-compose.deploy.yml build --parallel
docker compose -f docker-compose.yml -f docker-compose.prod.yml -f docker-compose.deploy.yml up -d
```

### 5. Verify

```bash
curl -s https://as-stats.example.com/api/v1/status | python3 -m json.tool
```

Check: routers active, flows arriving, UI loads.

## Version Numbering

- **MAJOR**: breaking API changes, schema migrations that require manual steps
- **MINOR**: new features, non-breaking API additions
- **PATCH**: bug fixes, performance improvements, dependency updates

## Rollback

```bash
# Revert to previous version
docker compose -f docker-compose.ghcr.yml pull  # pulls :latest
# Or pin a specific version:
IMAGE_TAG=v0.9.0 docker compose -f docker-compose.ghcr.yml up -d
```

For database schema changes, apply the corresponding `migrations/NNNNNN_*.down.sql`.
