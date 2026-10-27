# BGP blackholing (RTBH)

as-stats can ask your upstream providers to drop all traffic to one of your
addresses under attack: it announces a host route (`/32`, `/128`) carrying the
RFC 7999 **BLACKHOLE** community `65535:666` (plus `NO_EXPORT`) through a BGP
daemon you run. Your providers then discard every packet to that address at
their edge — the attack **and every legitimate visitor**. The address becomes
unreachable from the Internet until the block expires or is withdrawn. Use it
to save the rest of the network, not the target.

Blocks come from three places, all checked by the same safety rules:

| Source | How | Recorded as |
|---|---|---|
| BGP page | an admin enters an address | `manual` |
| Block on an alert / on Live Threats | an admin confirms the dialog | `alert` (with the alert's rule and metrics) / `manual` |
| Alert engine | a rule with action `auto_block` fires | `auto_block` (by `engine`) |

```
 Browser ──(session, admin, CSRF)──┐
                                   ├─► API: bgp.Service ──exec, no shell──► gobgp / birdc / vtysh / script ──► BGP daemon ──► upstream
 Collector alert engine ──(BGP_API_TOKEN, /internal/bgp/blocks)──┘        │
                                                                   bgp_blocks (ClickHouse), audit_log
```

The **API process owns everything**: the `bgp_blocks` table, the daemon
commands, expiry and reconciliation. The collector only asks.

## Safety rails

Every block — manual, from an alert or automatic — passes these checks before
any command runs. Refusals are answered with a clear message (HTTP 422 / 429)
and written to the audit log.

| Rail | Default | Variable |
|---|---|---|
| **Allow-list**: the target must lie inside one of these prefixes | your `LOCAL_AS` prefixes (RIPEstat, fetched at API start) | `BGP_ALLOWED_PREFIXES` (comma-separated; replaces the LOCAL_AS list). With neither, **every block is refused**. |
| **Never blocked**: the BGP peer, the next hops, the router id, plus your list | automatic | `BGP_PROTECTED_PREFIXES` (resolvers, router loopbacks, the as-stats host…) |
| Reserved space (`0.0.0.0/8`, loopback, link-local, multicast, `100::/64`, `::`…) | always refused | — |
| **Prefix length**: shortest prefix accepted | `/32` and `/128` (host routes only) | `BGP_MIN_PREFIX_LEN_V4` (≥ 24), `BGP_MIN_PREFIX_LEN_V6` (≥ 48) |
| **Max active blocks** (all sources) | 50 | `BGP_MAX_ACTIVE_BLOCKS` |
| **Automatic blocks per hour** (alert engine; survives restarts) | 10 | `BGP_AUTO_BLOCK_MAX_PER_HOUR` (`0` = no automatic block) |
| Duration (every block expires) | 1 h default, 24 h max (hard cap 168 h) | `BGP_DEFAULT_DURATION`, `BGP_MAX_DURATION` |
| IPv6 | refused unless an IPv6 next hop is set | `BGP_NEXT_HOP_V6` |
| **Dry run**: check and record, announce nothing | off | `BGP_DRY_RUN=true` |

A target with host bits set (`192.0.2.1/30`) is refused rather than widened.
IPv4-mapped IPv6 (`::ffff:192.0.2.1`) is treated as the IPv4 address.

**Start with `BGP_DRY_RUN=true`.** Blocks then show on the BGP page with a
"dry run" mark, expire normally and never reach the daemon — not even after
you switch dry run off (they are recorded with `dry_run = 1`).

## Quick start with GoBGP

### 1. Run gobgpd

A dedicated GoBGP instance next to the API is the simplest setup: it only
originates blackholes, so its RIB is exactly the list of active blocks.

`/etc/gobgpd.toml` (eBGP session to an upstream or an RTBH route server; adapt
addresses and ASNs):

```toml
[global.config]
  as = 64501                 # your AS
  router-id = "192.0.2.10"   # this host
  port = -1                  # do not listen for incoming sessions

[[neighbors]]
  [neighbors.config]
    neighbor-address = "192.0.2.2"   # upstream / RTBH peer
    peer-as = 64500
  [neighbors.ebgp-multihop.config]
    enabled = true                   # only if the peer is not directly connected
    multihop-ttl = 4
  [[neighbors.afi-safis]]
    [neighbors.afi-safis.config]
      afi-safi-name = "ipv4-unicast"
  [[neighbors.afi-safis]]
    [neighbors.afi-safis.config]
      afi-safi-name = "ipv6-unicast"
```

```bash
gobgpd -f /etc/gobgpd.toml --api-hosts 127.0.0.1:50051
gobgp neighbor 192.0.2.2          # state must reach Established
```

The gobgp CLI talks to gobgpd over gRPC (`127.0.0.1:50051` by default). The
API container image ships without gobgp: either run the API where `gobgp`
is in `PATH`, or build a small derived image
(`FROM ghcr.io/nextmap-io/as-stats-api:<version>` + `COPY gobgp
/usr/local/bin/`) and point the CLI at the daemon with `-u <host>` in the
templates (e.g. `gobgp -u 192.0.2.10 global rib add …`).

### 2. Configure the API

```bash
BGP_ENABLED=true
BGP_DRY_RUN=true                        # first, rehearse
BGP_PEER_ADDRESS=192.0.2.2
BGP_PEER_AS=64500
BGP_LOCAL_AS=64501
BGP_NEXT_HOP=192.0.2.10                 # see "Next hop" below
BGP_NEXT_HOP_V6=2001:db8::10            # optional; without it IPv6 blocks are refused
BGP_ALLOWED_PREFIXES=198.51.100.0/24,2001:db8:100::/48   # recommended even with LOCAL_AS
BGP_PROTECTED_PREFIXES=198.51.100.53,198.51.100.1        # resolvers, gateways…
```

With the default (gobgp) commands, as-stats runs — without a shell:

```
gobgp global rib add 198.51.100.10/32 community 65535:666,65535:65281 nexthop 192.0.2.10 -a ipv4
gobgp global rib del 198.51.100.10/32 -a ipv4
gobgp neighbor 192.0.2.2 --json                  # session state, every minute
gobgp global rib -a ipv4   (and -a ipv6)         # reconciliation, every minute
```

### 3. Rehearse, then go live

1. Restart the API, open **BGP blocks**: the session should read *Established*
   ("Check the daemon now" re-runs the status command).
2. Block a test address of yours in dry run. Check the history, the audit log
   (Admin › Audit, `bgp_block`) and the API log (`bgp[dry-run]: would announce`).
3. Set `BGP_DRY_RUN=false`, restart, block the test address for 15 minutes and
   verify with `gobgp global rib` and on the provider's looking glass.

### Next hop

Most RTBH services ignore the next hop and set their own discard next hop when
they see `65535:666`; on an eBGP session it must still be valid for the
session (usually your side of the link, i.e. the gobgpd router-id or local
address). Inside your own network (iBGP to your edge routers) use an address
that every edge routes to a discard interface (`192.0.2.1/32 → Null0`), see
"Edge routers" below.

## Configuration reference (API)

| Variable | Default | Description |
|---|---|---|
| `BGP_ENABLED` | `false` | Enables the feature (API routes, BGP page). |
| `BGP_PEER_ADDRESS` | — (required) | BGP peer; used for `{peer_address}` and protected. |
| `BGP_PEER_AS`, `BGP_LOCAL_AS` | — (required) | ASNs, for `{peer_as}` / `{local_as}` and the status page. |
| `BGP_NEXT_HOP` | — (required) | IPv4 next hop of announced routes; protected. |
| `BGP_NEXT_HOP_V6` | empty | IPv6 next hop. Empty = IPv6 blocks refused. |
| `BGP_ROUTER_ID` | empty | Optional (no longer required); protected when set. |
| `BGP_COMMUNITY` | `65535:666` | BLACKHOLE community (`{community}`). Some providers use their own (e.g. `64500:666`). |
| `BGP_EXTRA_COMMUNITIES` | `65535:65281` (NO_EXPORT) | Added to `{communities}`. Set empty to drop NO_EXPORT. Accepts `ASN:VAL`, large `A:B:C`, `no-export`, `no-advertise`, `blackhole`. |
| `BGP_ANNOUNCE_CMD` | gobgp (above) | Announce template. Must contain `{prefix}` or `{ip}`. |
| `BGP_WITHDRAW_CMD` | gobgp (above) | Withdraw template. Should succeed when the route is already gone. |
| `BGP_STATUS_CMD` | gobgp when the announce command is the default, else none | Session state. Set empty to disable. |
| `BGP_ROUTES_CMD` | gobgp when the announce command is the default, else none | Lists the routes the daemon originates, for reconciliation. Set empty to disable. |
| `BGP_COMMAND_TIMEOUT` | `10s` | Per command (max `1m`); the process and its children are killed after it. |
| `BGP_RECONCILE_INTERVAL` | `1m` | Status + RIB check (min `10s`). Expiry is checked every 15 s. |
| `BGP_DRY_RUN` | `false` | See above. |
| `BGP_ALLOWED_PREFIXES`, `BGP_PROTECTED_PREFIXES`, `BGP_MIN_PREFIX_LEN_V4/_V6`, `BGP_MAX_ACTIVE_BLOCKS`, `BGP_AUTO_BLOCK_MAX_PER_HOUR`, `BGP_DEFAULT_DURATION`, `BGP_MAX_DURATION` | see "Safety rails" | |
| `BGP_API_TOKEN` | empty | Shared with the collector for auto-block (≥ 32 characters, e.g. `openssl rand -hex 32`). Without it the internal route does not exist. |

Every value is validated at startup: an invalid community, address, template
or duration stops the API with an explicit message rather than half-working.

### Command templates

Templates are split into arguments once, at startup, like a POSIX shell would
(`'single'` and `"double"` quotes, backslash escapes) and run with `exec` —
**never through a shell**. A placeholder value can only fill the argument it
sits in. Unquoted `|`, `;`, `&`, `>`, `<`, `` ` `` or `$` are refused at
startup: put pipelines in a script (or call `sh -c '…'` explicitly, at your
own risk).

| Placeholder | Value |
|---|---|
| `{prefix}` | `198.51.100.10/32` |
| `{ip}`, `{prefix_len}` | `198.51.100.10`, `32` |
| `{family}` | `ipv4` / `ipv6` |
| `{community}` | `BGP_COMMUNITY` |
| `{communities}` | community + extra, comma-separated (`65535:666,65535:65281`) |
| `{next_hop}` | the next hop for the prefix's family |
| `{peer_address}`, `{peer_as}`, `{local_as}` | from the configuration |
| `{action}` | `announce`, `withdraw`, `status`, `routes` |

An unknown placeholder is a startup error. Commands run with a minimal
environment — `PATH`, `LANG=C` and the same values as `ASSTATS_BGP_PREFIX`,
`ASSTATS_BGP_NEXT_HOP`, `ASSTATS_BGP_COMMUNITIES`, `ASSTATS_BGP_ACTION`, … —
never the API's own variables (ClickHouse password, OIDC secret, token).

A failing command (non-zero exit, timeout) means *not done*: the block is
recorded as `failed` and the BGP page says the daemon refused. Its output is
logged by the API, not returned to the browser.

## BIRD 2

BIRD has no runtime "add route" command. Blackholes live in two include files
read by static protocols; [`scripts/bgp/bird-rtbh.sh`](../scripts/bgp/bird-rtbh.sh)
edits them and runs `birdc configure`.

`bird.conf` (excerpt):

```
protocol static rtbh4 {
  ipv4;
  include "/etc/bird/rtbh4.conf";   # route 198.51.100.10/32 blackhole;
}
protocol static rtbh6 {
  ipv6;
  include "/etc/bird/rtbh6.conf";
}

filter rtbh_out {
  if proto = "rtbh4" || proto = "rtbh6" then {
    bgp_community.add((65535,666));
    bgp_community.add((65535,65281));   # NO_EXPORT
    accept;
  }
  reject;
}

protocol bgp rtbh_upstream {
  local 192.0.2.10 as 64501;
  neighbor 192.0.2.2 as 64500;
  ipv4 { import none; export filter rtbh_out; next hop self; };
}
protocol bgp rtbh_upstream6 {
  local 2001:db8::10 as 64501;
  neighbor 2001:db8::2 as 64500;
  ipv6 { import none; export filter rtbh_out; next hop self; };
}
```

```bash
install -m 755 scripts/bgp/bird-rtbh.sh /usr/local/bin/
touch /etc/bird/rtbh4.conf /etc/bird/rtbh6.conf    # writable by the API's user
```

```bash
BGP_ANNOUNCE_CMD="/usr/local/bin/bird-rtbh.sh announce {prefix}"
BGP_WITHDRAW_CMD="/usr/local/bin/bird-rtbh.sh withdraw {prefix}"
BGP_ROUTES_CMD="/usr/local/bin/bird-rtbh.sh routes"
BGP_STATUS_CMD="/usr/local/bin/bird-rtbh.sh status rtbh_upstream"
```

The communities are set by BIRD's export filter, so `BGP_COMMUNITY` only
documents them here. Options: `--dir=/etc/bird`, `--birdc=/usr/sbin/birdc`
(before the action). The API's user needs access to the BIRD control socket.

## FRRouting

A blackhole is a static route to `blackhole` plus a `network` statement with
the `RTBH` route-map; [`scripts/bgp/frr-rtbh.sh`](../scripts/bgp/frr-rtbh.sh)
issues the IPv4 or IPv6 variant.

`frr.conf` (excerpt):

```
bgp community-list standard RTBH permit 65535:666
route-map RTBH permit 10
 set community 65535:666 65535:65281
route-map RTBH-OUT permit 10
 match community RTBH
route-map DENY-ALL deny 10
!
router bgp 64501
 neighbor 192.0.2.2 remote-as 64500
 address-family ipv4 unicast
  neighbor 192.0.2.2 send-community
  neighbor 192.0.2.2 route-map DENY-ALL in
  neighbor 192.0.2.2 route-map RTBH-OUT out
 exit-address-family
 address-family ipv6 unicast
  neighbor 192.0.2.2 activate
  neighbor 192.0.2.2 send-community
  neighbor 192.0.2.2 route-map DENY-ALL in
  neighbor 192.0.2.2 route-map RTBH-OUT out
 exit-address-family
```

```bash
BGP_ANNOUNCE_CMD="/usr/local/bin/frr-rtbh.sh announce {prefix} {local_as}"
BGP_WITHDRAW_CMD="/usr/local/bin/frr-rtbh.sh withdraw {prefix} {local_as}"
BGP_ROUTES_CMD="/usr/local/bin/frr-rtbh.sh routes"
BGP_STATUS_CMD="vtysh -c 'show bgp neighbor {peer_address} json'"
```

The API's user must be in the `frrvty` group. Note: the old documentation's
FRR example used `router bgp {peer_as}` — it must be your own AS
(`{local_as}`), which the script enforces.

## ExaBGP

```bash
BGP_ANNOUNCE_CMD="exabgpcli announce route {prefix} next-hop {next_hop} community [65535:666 65535:65281]"
BGP_WITHDRAW_CMD="exabgpcli withdraw route {prefix} next-hop {next_hop}"
BGP_STATUS_CMD="exabgpcli show neighbor summary"
BGP_ROUTES_CMD="exabgpcli show adj-rib out"
```

## Upstream and edge routers

**Transit providers / RTBH services.** Most accept `65535:666` (or their own
community — set `BGP_COMMUNITY`) on host routes inside the prefixes you are
registered for, on your normal transit session or on a dedicated one. Ask
for: accepted prefix lengths, the community, whether `NO_EXPORT` is expected,
and the maximum number of blackholes (keep `BGP_MAX_ACTIVE_BLOCKS` below it).

**Your own edge routers** (iBGP from the blackhole speaker) — accept only
`65535:666` routes from it and send them to a discard next hop:

Junos:

```
policy-options {
    community BLACKHOLE members 65535:666;
    policy-statement ACCEPT-BLACKHOLE {
        term blackhole { from community BLACKHOLE; then { next-hop discard; accept; } }
        term rest { then reject; }
    }
}
protocols bgp group RTBH { type internal; neighbor 192.0.2.10 { import ACCEPT-BLACKHOLE; } }
```

Cisco IOS-XE:

```
ip community-list standard BLACKHOLE permit 65535:666
ip route 192.0.2.1 255.255.255.255 Null0
route-map BLACKHOLE-IN permit 10
 match community BLACKHOLE
 set ip next-hop 192.0.2.1
route-map BLACKHOLE-IN deny 20
router bgp 64501
 neighbor 192.0.2.10 remote-as 64501
 address-family ipv4
  neighbor 192.0.2.10 activate
  neighbor 192.0.2.10 route-map BLACKHOLE-IN in
```

MikroTik RouterOS 7:

```
/routing/filter/rule
add chain=rtbh-in rule="if (bgp-communities includes 65535:666) { set blackhole yes; accept }"
add chain=rtbh-in rule="reject"
/routing/bgp/connection
add name=rtbh remote.address=192.0.2.10 remote.as=64501 local.role=ibgp input.filter=rtbh-in
```

## Auto-block from alert rules

1. Generate a token: `openssl rand -hex 32`.
2. API: `BGP_API_TOKEN=<token>`. Collector: `BGP_API_URL=http://api:8080` and
   the same `BGP_API_TOKEN`.
3. In **Admin › Rules**, set the action of the rules you trust to `auto_block`.

When such a rule fires on a single host (never for link, anomaly, disk or
subnet rules), the collector posts the alert's target and context to
`POST /internal/bgp/blocks` with `Authorization: Bearer <token>`. That route
is outside `/api/v1` (no session, no CSRF cookie), compares the token in
constant time, is only registered when the token is set, and is not proxied
by the bundled nginx (it only forwards `/api/` and `/auth/`) — keep port 8080
of the API off the Internet. The API applies every rail: the allow-list, max
active blocks and **at most `BGP_AUTO_BLOCK_MAX_PER_HOUR` automatic blocks
per hour**, so a runaway rule cannot blackhole your network. Duplicates
(target already blocked) are answered 200 and change nothing. A refusal is
logged by the collector (`auto-block … NOT applied`) and counted in
`asstats_bgp_auto_block_requests_total{result="refused"}`.

Before this release the collector sent unauthenticated requests that the API's CSRF
check always rejected: auto-block never worked.

## Expiry, restarts and reconciliation

- **Row first, route second.** A block is written to `bgp_blocks` (status
  `active`) before the announce command runs; if the command fails the row
  becomes `failed`. A crash in between leaves an active row, never an unknown
  route.
- **Expiry** is checked every 15 s from the in-memory copy of the active rows.
  A failed withdraw is retried; the row stays `active` until the daemon
  confirms (or no longer lists the route).
- **Restart**: the API reloads every active row (retrying until ClickHouse
  answers — blocking is refused with 503 meanwhile), withdraws those that
  expired while it was down and re-announces the others with their original
  expiry. Routes are left in the daemon on shutdown on purpose.
- **Reconciliation** (with a routes command, every `BGP_RECONCILE_INTERVAL`):
  an active block missing from the daemon (daemon restarted) is re-announced;
  a route the daemon carries without a block is shown on the BGP page as an
  *unknown route* — never withdrawn automatically, but withdrawable there.
  Without a routes command, a session coming back to *Established* triggers a
  re-announce of every active block.
- **Withdraw** of an address with no active block answers 404 (unless it is
  an unknown route of the daemon). Blocking an address already blocked
  answers 200 with the existing block.

`bgp_blocks` is a plain MergeTree written by the API only; status changes are
synchronous `ALTER … UPDATE` keyed by id (a few rows a day). Times are UTC.
Rows are kept 365 days (Admin › Storage).

## Monitoring

`GET /api/v1/bgp/status` (`?refresh=1` re-runs the status command) returns the
session, the rails and the last reconciliation:

```json
{"data": {"enabled": true, "dry_run": false, "ready": true, "state": "established", "uptime": 86400,
  "peer_address": "192.0.2.2", "peer_as": 64500, "local_as": 64501, "communities": ["65535:666", "65535:65281"],
  "active_blocks": 2, "routes_announced": 2, "max_active_blocks": 50,
  "auto_blocks_last_hour": 1, "auto_blocks_per_hour": 10,
  "allowed_source": "BGP_ALLOWED_PREFIXES", "allowed_prefix_count": 2,
  "rib": {"supported": true, "routes": 2, "missing": [], "orphans": []}}}
```

Prometheus (API `/metrics`):

| Metric | Use |
|---|---|
| `asstats_bgp_blocks_active{mode}` | live / dry_run |
| `asstats_bgp_block_requests_total{source,result}` | created / existing / rejected / limited / error |
| `asstats_bgp_withdrawals_total{trigger,result}` | manual / expired / orphan |
| `asstats_bgp_commands_total{action,result}`, `asstats_bgp_command_duration_seconds` | daemon CLI health |
| `asstats_bgp_daemon_up` | 1 established, 0 down, -1 not checked |
| `asstats_bgp_rib_missing`, `asstats_bgp_rib_orphans`, `asstats_bgp_reannounced_total` | reconciliation |

Collector: `asstats_bgp_auto_block_requests_total{result}`.

Suggested alerts: `asstats_bgp_daemon_up == 0 and asstats_bgp_blocks_active{mode="live"} > 0`;
`increase(asstats_bgp_commands_total{result="error"}[10m]) > 0`;
`asstats_bgp_rib_missing > 0`; `increase(asstats_bgp_block_requests_total{source="auto_block",result="limited"}[1h]) > 0`.

Every block, withdrawal, refusal and expiry is in the audit log (actions
`bgp_block`, `bgp_unblock`, `bgp_expire`; user `engine` / `system` for the
automatic ones).

## Upgrading from 3.6

- Auto-block needs `BGP_API_TOKEN` on both API and collector (it never
  worked before).
- Templates no longer run through `sh -c`: one using pipes, `;` or `>` is
  refused at startup — move it to a script. Quoting works as before.
- Blocks outside the allow-list are refused: set `BGP_ALLOWED_PREFIXES` (or
  `LOCAL_AS`) before upgrading.
- IPv6 blocks need `BGP_NEXT_HOP_V6`.
- NO_EXPORT (`65535:65281`) is added by default to the gobgp announce
  (`BGP_EXTRA_COMMUNITIES=` restores the old behaviour).
- The default status command (gobgp) now actually runs; a routes command
  enables reconciliation.
- Migration 000019 adds `bgp_blocks.dry_run`.

## What is tested

The service, the policy, the command layer (argument splitting, environment,
timeouts), the BIRD / FRR scripts (against stub `birdc` / `vtysh`) and the
HTTP API end to end with a fake daemon on ClickHouse 24.3 and 26.8 are
covered by tests. The gobgp, BIRD and FRR command lines and daemon
configurations above follow those tools' documentation but have **not** been
run against live daemons in CI: rehearse with `BGP_DRY_RUN=true`, then with a
test address, before relying on it.
