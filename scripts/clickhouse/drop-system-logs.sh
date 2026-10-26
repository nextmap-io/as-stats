#!/bin/sh
# Removes every system.*_log table (metadata and data) from a STOPPED ClickHouse
# data volume, so the server version started next re-creates them.
#
# Needed to roll a volume back from ClickHouse 26.8 to 24.3: 24.3 cannot attach
# the system log tables 26.8 created (new schemas and part formats) and refuses
# to start. Dropping them on a running 26.8 is useless — it re-creates them at
# the next flush. Only diagnostic history is lost. See
# docs/upgrades/clickhouse-26.8.md › Rollback.
#
#   docker run --rm -v <project>_clickhouse_data:/var/lib/clickhouse \
#     -v "$PWD/scripts/clickhouse":/s:ro alpine:latest sh /s/drop-system-logs.sh
set -eu
D=/var/lib/clickhouse
for f in $D/metadata/system/*_log.sql $D/metadata/system/*_log_[0-9]*.sql; do
  [ -e "$f" ] || continue
  u=$(grep -o "UUID '[^']*'" "$f" | head -1 | cut -d"'" -f2)
  if [ -n "$u" ]; then rm -rf "$D/store/$(echo "$u" | cut -c1-3)/$u"; fi
  rm -f "$f"
  echo "removed $(basename "$f" .sql) ${u:-}"
done
