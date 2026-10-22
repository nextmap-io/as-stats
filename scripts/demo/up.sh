#!/bin/sh
# Throwaway demo stack with invented data, for screenshots.
#
#   scripts/demo/up.sh [tag]      # default tag: latest
#   scripts/demo/down.sh
#
# Containers are prefixed "asdemo-" on a private network "asdemo". ClickHouse
# runs on a 4 GB tmpfs, so nothing is written to the host disk and the "disk"
# figure in the UI reflects the demo, not your machine. The UI is published on
# 127.0.0.1:${DEMO_PORT:-8090}.
set -eu
TAG=${1:-latest}
DIR=$(cd "$(dirname "$0")" && pwd)
STUB=$(mktemp -d)
chmod 755 "$STUB"

# 1. RIPEstat stand-in: the API and collector learn their local prefixes from
#    RIPEstat at startup. This stub answers that call with documentation
#    prefixes for AS64500, under the stat.ripe.net network alias.
docker run --rm -u "$(id -u):$(id -g)" -e CGO_ENABLED=0 -e GOCACHE=/tmp/gocache \
  -v "$DIR/ripestub":/src:ro -v "$STUB":/out -w /src golang:1.27 go build -o /out/ripestub main.go

docker network create asdemo >/dev/null
docker run -d --name asdemo-ripe --network asdemo --network-alias stat.ripe.net --label "asdemo.stub=$STUB" \
  -u "$(id -u):$(id -g)" --sysctl net.ipv4.ip_unprivileged_port_start=0 \
  -e CERT_OUT=/stub/ca.pem -v "$STUB":/stub alpine:latest /stub/ripestub >/dev/null

# 2. ClickHouse on tmpfs
docker run -d --name asdemo-ch --network asdemo --network-alias clickhouse \
  --tmpfs /var/lib/clickhouse:size=4g --ulimit nofile=262144:262144 \
  -e CLICKHOUSE_DB=asstats -e CLICKHOUSE_USER=asstats -e CLICKHOUSE_PASSWORD=asstats \
  -v "$DIR":/seed:ro "${CLICKHOUSE_IMAGE:-clickhouse/clickhouse-server:24.3-alpine}" >/dev/null
# The image's entrypoint first runs a loopback-only server for its init step;
# wait for the real one, reachable from the network.
until docker exec asdemo-ch sh -c 'clickhouse-client --host "$(hostname -i)" -u asstats --password asstats -q "SELECT 1"' >/dev/null 2>&1; do sleep 1; done

# 3. Schema
CH="-e CLICKHOUSE_ADDR=clickhouse:9000 -e CLICKHOUSE_DATABASE=asstats -e CLICKHOUSE_USER=asstats -e CLICKHOUSE_PASSWORD=asstats"
# shellcheck disable=SC2086
docker run --rm --network asdemo $CH "ghcr.io/nextmap-io/as-stats-migrate:$TAG"

# 4. Data (takes a few minutes)
docker exec asdemo-ch sh /seed/seed.sh

# 5. API + UI
# shellcheck disable=SC2086
docker run -d --name asdemo-api --network asdemo --network-alias api $CH \
  -e AUTH_ENABLED=false -e ALLOW_UNAUTHENTICATED=true -e LOCAL_AS=64500 \
  -e FEATURE_ALERTS=true -e FEATURE_FLOW_SEARCH=true -e FEATURE_PORT_STATS=true \
  -e FEATURE_REPORTS=true -e SMTP_HOST=smtp.example.net -e SMTP_FROM=as-stats@example.net \
  -e SSL_CERT_FILE=/stub/ca.pem -v "$STUB":/stub:ro \
  "ghcr.io/nextmap-io/as-stats-api:$TAG" >/dev/null
docker run -d --name asdemo-web --network asdemo -p "127.0.0.1:${DEMO_PORT:-8090}:80" \
  "ghcr.io/nextmap-io/as-stats-frontend:$TAG" >/dev/null

# 6. Keep the routers "reporting"
docker exec -d asdemo-ch sh /seed/tick.sh
echo "demo up: http://127.0.0.1:${DEMO_PORT:-8090}/  (stub dir: $STUB)"
