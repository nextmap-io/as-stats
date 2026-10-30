# Demo stack

A throwaway AS-Stats with two weeks of invented traffic, used for the
screenshots in the main README. Everything runs in Docker; nothing is written
to the host disk except the screenshots.

```bash
scripts/demo/up.sh 3.2.0          # published images of that tag (default: latest)
# UI on http://127.0.0.1:8090/ (DEMO_PORT to change)
scripts/demo/shoot.sh docs/screenshots scripts/demo/screenshots.json
scripts/demo/down.sh              # removes the containers and the network
```

Needs the images `clickhouse/clickhouse-server:24.3-alpine` (or the one in
`CLICKHOUSE_IMAGE`, e.g. `clickhouse/clickhouse-server:26.8.36.3-alpine`),
`ghcr.io/nextmap-io/as-stats-{migrate,api,frontend}`, `golang:1.27` (to build
the stub below), `alpine` and, for screenshots,
`zenika/alpine-chrome:with-puppeteer`. Seeding takes a few minutes and about
3 GB of RAM (ClickHouse runs on a tmpfs).

## The fictional network

- Operator: **AS64500 "Example Networks"** (documentation ASN, RFC 5398).
- Its address space: `192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24`,
  `2001:db8::/32` (documentation prefixes). Routers are 192.0.2.252 / .253.
- Remote addresses: `198.18.0.0/15` (benchmarking, RFC 2544) and
  `3fff::/20` (IPv6 documentation, RFC 9637), so no address belongs to anyone.
- Remote ASNs carry well-known network names with invented volumes; about
  30 % of the traffic comes from private ASNs (grouped as "Private /
  Internal").
- Five links: Transit A 10G, Transit B 1G (the busy one, growing ~16 % over
  two weeks), IX Paris 10G, PNI CDN 10G, IX Amsterdam 1G.

## What is seeded

| File | Content |
|---|---|
| `config.sql` | Links, AS names, the default alert rules, two active and five past alerts |
| `flows.sql` | Background traffic into `flows_raw`: evening peak, weekends lower, a few movers (Hetzner up, Arelion down, Scaleway new today, Deutsche Telekom gone). The materialised views build every rollup from it |
| `attacks.sql` | The last half hour: a SYN flood on 203.0.113.10, an NTP reflection on 198.51.100.25, an outbound port scan from 198.51.100.77, a backup burst to 192.0.2.40 |
| `seed.sh` | Runs the above inside the ClickHouse container (15 days, then a denser last 3 hours) |
| `tick.sh` | Adds 30 s of traffic every 30 s so routers keep "reporting" and the events go on |

No collector runs: rows are inserted straight into `flows_raw`, so alerts are
seeded rather than raised by the engine.

## Local prefixes without RIPEstat

The API (and the collector) learn "your" prefixes by asking RIPEstat for the
prefixes `LOCAL_AS` announces; Live Threats, the alert scope and the
internal / external IP views depend on them. AS64500 announces nothing, so
`ripestub/` stands in for that one call: a small Go server that answers
`/data/announced-prefixes/data.json` with the documentation prefixes, under
the network alias `stat.ripe.net` on the demo network, with a throwaway
self-signed certificate that the API trusts through `SSL_CERT_FILE`. The real
RIPEstat is never queried.
