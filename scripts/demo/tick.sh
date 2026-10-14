#!/bin/sh
# Runs inside the ClickHouse container: inserts 30 s of traffic every 30 s so
# the routers stay "reporting" and the ongoing events continue.
C="clickhouse-client -u asstats --password asstats"
while true; do
  sed -e "s/{T0}/now()/g" -e "s/{N}/1/g" -e "s/{SPAN}/30/g" -e "s/{K}/30/g" /seed/flows.sql | $C --multiquery
  sed -e "s/{T0}/now()/g" -e "s/{N}/1/g" -e "s/{SPAN}/30/g" /seed/attacks.sql | $C --multiquery
  sleep 30
done
