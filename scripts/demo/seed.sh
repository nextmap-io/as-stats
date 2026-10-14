#!/bin/sh
# Runs inside the ClickHouse container (scripts/demo mounted at /seed).
# 15 days of background traffic, one dense recent window, then the events.
set -e
C="clickhouse-client -u asstats --password asstats --max_memory_usage=3000000000"
gen() { # gen <sql> <T0> <N> <SPAN> <K>
  sed -e "s/{T0}/$2/g" -e "s/{N}/$3/g" -e "s/{SPAN}/$4/g" -e "s/{K}/$5/g" "$1" | $C --multiquery
}

$C --multiquery < /seed/config.sql

# Day by day, oldest first: 15 days back up to 3 h ago, 4 rows per AS and slot.
d=15
while [ "$d" -ge 1 ]; do
  gen /seed/flows.sql "toStartOfMinute(now()) - toIntervalMinute(180 + ($d - 1) * 1440)" 1440 60 4
  echo "day -$d done"
  d=$((d - 1))
done
# Last 3 hours: same rates, split into more flows so flows/s looks like a live router.
gen /seed/flows.sql "toStartOfMinute(now())" 180 60 30
gen /seed/attacks.sql "toStartOfMinute(now())" 31 60 1

# flows_raw keeps 3 days in production (TTL); the older days were only needed
# to feed the rollups. Drop them so the tmpfs stays small.
for p in $($C --query "SELECT DISTINCT partition FROM system.parts WHERE database = 'asstats' AND table = 'flows_raw' AND active AND toDate(partition) < today() - 3"); do
  $C --query "ALTER TABLE asstats.flows_raw DROP PARTITION '$p'"
done

$C --query "SELECT count(), min(timestamp), max(timestamp) FROM asstats.flows_raw"
