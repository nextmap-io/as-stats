# BGP Blackhole

## Overview

BGP blackholing (RFC 7999) is a technique where a network operator advertises a /32 (IPv4) or /128 (IPv6) route with a well-known BLACKHOLE community (65535:666) to upstream providers. The upstream providers then drop all traffic destined for that prefix at their edge, preventing DDoS traffic from ever reaching your network.

as-stats implements BGP blackholing through the `ScriptBlocker`, which executes shell commands to interact with a local BGP daemon (GoBGP, BIRD, FRRouting, or any other daemon with a CLI). The design is intentionally daemon-agnostic: as-stats does not link against any BGP library. Instead, it shells out to whatever command you configure.

Key properties:

- **State persisted in ClickHouse** -- active blocks are stored in the `bgp_blocks` table so they survive process restarts.
- **Re-announced on startup** -- when the API server starts, `ScriptBlocker` loads all active (non-expired) blocks from the database and re-announces them to the BGP daemon.
- **Idempotent** -- announcing the same IP twice is a no-op.
- **Auto-expiry** -- each block can have a duration; a background goroutine automatically withdraws the route when the timer fires.

## Quick start

This guide uses GoBGP as the BGP daemon. See the section on BIRD/FRRouting below if you use a different daemon.

### 1. Install GoBGP on the API server

```bash
# Debian/Ubuntu
apt install gobgpd

# Or download the binary
curl -LO https://github.com/osrg/gobgp/releases/latest/download/gobgp_<version>_linux_amd64.tar.gz
tar xzf gobgp_*.tar.gz
mv gobgpd gobgp /usr/local/bin/
```

### 2. Configure GoBGP

Create `/etc/gobgpd.conf`:

```toml
[global.config]
  as = 64500
  router-id = "192.0.2.1"

[[neighbors]]
  [neighbors.config]
    neighbor-address = "192.0.2.2"
    peer-as = 64500
  [neighbors.afi-safis]
    [[neighbors.afi-safis.afi-safi-configs]]
      afi-safi-name = "ipv4-unicast"
```

Start the daemon:

```bash
gobgpd -f /etc/gobgpd.conf &
```

### 3. Set environment variables for the API server

```bash
export BGP_ENABLED=true
export BGP_ROUTER_ID=192.0.2.1
export BGP_LOCAL_AS=64500
export BGP_PEER_ADDRESS=192.0.2.2
export BGP_PEER_AS=64500
export BGP_COMMUNITY=65535:666
export BGP_NEXT_HOP=192.0.2.1
```

### 4. Restart the API server

The API server will create a `ScriptBlocker` using the default GoBGP command templates.

### 5. Test

Block an IP manually via the BGP page in the web UI (requires admin role), then verify:

```bash
gobgp global rib
# Should show a /32 route with community 65535:666
```

## Configuration reference

All variables are read by the API server at startup.

| Variable | Required | Default | Description |
|---|---|---|---|
| `BGP_ENABLED` | No | `false` | Set to `true` to enable the BGP blackhole feature. |
| `BGP_ROUTER_ID` | When enabled | -- | Router ID for GoBGP (e.g. `192.0.2.1`). |
| `BGP_LOCAL_AS` | When enabled | -- | Local AS number (e.g. `64500`). |
| `BGP_PEER_ADDRESS` | When enabled | -- | BGP peer address (e.g. `192.0.2.2`). |
| `BGP_PEER_AS` | When enabled | -- | Peer AS number (e.g. `64500`). |
| `BGP_COMMUNITY` | No | `65535:666` | BGP community attached to blackhole routes. RFC 7999 defines `65535:666` as the well-known BLACKHOLE community. |
| `BGP_NEXT_HOP` | When enabled | -- | Next-hop IP for announced routes. Typically the router-id or a dedicated discard next-hop. |
| `BGP_ANNOUNCE_CMD` | No | `gobgp global rib add {ip}/{prefix_len} community {community} nexthop {next_hop} -a ipv4` | Shell command template for announcing a route. |
| `BGP_WITHDRAW_CMD` | No | `gobgp global rib del {ip}/{prefix_len} -a ipv4` | Shell command template for withdrawing a route. |
| `BGP_STATUS_CMD` | No | `gobgp neighbor {peer_address} --json` | Shell command template for querying BGP session status. |
| `BGP_API_URL` | No | -- | Base URL of the API server (e.g. `http://localhost:8080`). Used by the **collector** to proxy auto-block requests to the API via `RemoteBlocker`. |

### Template placeholders

The command templates support these placeholders, which are substituted at execution time:

| Placeholder | Value |
|---|---|
| `{ip}` | Target IP address (e.g. `198.51.100.1`) |
| `{prefix_len}` | Prefix length (`32` for IPv4, `128` for IPv6) |
| `{community}` | BGP community string (e.g. `65535:666`) |
| `{next_hop}` | Next-hop IP address |
| `{peer_address}` | BGP peer address |

## Router-side configuration

Your upstream routers must be configured to accept the blackhole community and route matching prefixes to null/discard.

### Junos

```
policy-options {
    community BLACKHOLE members 65535:666;
    policy-statement ACCEPT-BLACKHOLE {
        term blackhole {
            from community BLACKHOLE;
            then {
                next-hop discard;
                accept;
            }
        }
    }
}

protocols {
    bgp {
        group BLACKHOLE-PEER {
            type internal;
            local-address 192.0.2.2;
            neighbor 192.0.2.1 {
                import ACCEPT-BLACKHOLE;
                family inet {
                    unicast;
                }
            }
        }
    }
}

routing-options {
    static {
        route 192.0.2.1/32 discard;
    }
}
```

### Cisco IOS-XE

```
ip community-list standard BLACKHOLE permit 65535:666

route-map BLACKHOLE-IN permit 10
 match community BLACKHOLE
 set ip next-hop 192.0.2.1
 set local-preference 200

ip route 192.0.2.1 255.255.255.255 Null0

router bgp 64500
 neighbor 192.0.2.1 remote-as 64500
 neighbor 192.0.2.1 update-source Loopback0
 address-family ipv4
  neighbor 192.0.2.1 activate
  neighbor 192.0.2.1 route-map BLACKHOLE-IN in
```

### MikroTik RouterOS 7

```
/routing/bgp/connection
add name=blackhole-peer remote.address=192.0.2.1 remote.as=64500 \
    local.role=ibgp address-families=ip

/routing/filter/rule
add chain=bgp-in-blackhole rule="if (bgp-communities includes 65535:666) { \
    set blackhole yes; accept }"

/routing/bgp/connection
set blackhole-peer input.filter=bgp-in-blackhole
```

## Auto-block

The alert engine can automatically block IPs that trigger a rule configured with `action: "auto_block"`.

### How it works

1. In **Admin > Alert Rules**, set a rule's action to `auto_block`.
2. On the **collector**, set `BGP_API_URL` to the API server's base URL (e.g. `http://localhost:8080`). The collector uses a `RemoteBlocker` that sends HTTP requests to the API.
3. When the alert engine detects a violation:
   - It creates the alert as usual.
   - It calls `RemoteBlocker.Announce()`, which sends a POST to the API server.
   - The API server's `ScriptBlocker` executes the announce command (e.g. `gobgp global rib add ...`).
   - The block is persisted to the `bgp_blocks` table in ClickHouse.
4. The default block duration is **1 hour**. After that, the route is automatically withdrawn.
5. If the IP is already actively blocked, the duplicate block is skipped.

### Auto-generated description

The block record includes an auto-generated description with context from the triggering alert:

```
Auto-blocked by rule 'Critical inbound volume': 5000000000.00 bps exceeded threshold 2000000000.00. Top sources: 198.51.100.1, 198.51.100.2
```

This description appears in the BGP page and in the `bgp_blocks` table.

## Manual block/unblock

Administrators can manually block and unblock IPs from the **/bgp** page in the web UI. This page requires the **admin** role.

- **Block**: enter an IP address, optional duration, and a reason. The ScriptBlocker announces the route immediately.
- **Unblock**: click the withdraw button on an active block. The route is withdrawn and the block status is set to `withdrawn`.

The `/alerts/{id}/block` endpoint on the alerts page also allows blocking the target IP of a specific alert.

## What happens on restart

When the API server starts with `BGP_ENABLED=true`:

1. `NewScript()` is called with the configured command templates.
2. It queries the `bgp_blocks` table for all blocks with `status = 'active'` and `expires_at` in the future.
3. For each active block, it re-executes the announce command to re-inject the route into the BGP daemon.
4. If a block has a future expiry, a new auto-withdraw timer is scheduled for the remaining duration.

This means that blocks survive both API server restarts and BGP daemon restarts (assuming the BGP daemon is restarted first and the API server re-announces on startup).

If the store is unreachable at startup, the API server logs a warning and continues without re-announcing. Operators can manually re-block affected IPs via the UI.

## Using with BIRD or FRRouting instead of GoBGP

The `ScriptBlocker` is not tied to GoBGP. Override the command templates to use any BGP daemon:

### BIRD

```bash
export BGP_ANNOUNCE_CMD="birdc 'route add {ip}/{prefix_len} blackhole community (65535,666)'"
export BGP_WITHDRAW_CMD="birdc 'route delete {ip}/{prefix_len}'"
export BGP_STATUS_CMD="birdc 'show protocols all blackhole_peer'"
```

### FRRouting

```bash
export BGP_ANNOUNCE_CMD="vtysh -c 'configure terminal' -c 'ip route {ip}/{prefix_len} Null0' -c 'router bgp {peer_as}' -c 'network {ip}/{prefix_len} route-map BLACKHOLE'"
export BGP_WITHDRAW_CMD="vtysh -c 'configure terminal' -c 'no ip route {ip}/{prefix_len} Null0' -c 'router bgp {peer_as}' -c 'no network {ip}/{prefix_len}'"
export BGP_STATUS_CMD="vtysh -c 'show bgp neighbor {peer_address} json'"
```

### ExaBGP

```bash
export BGP_ANNOUNCE_CMD="exabgpcli announce route {ip}/{prefix_len} next-hop {next_hop} community {community}"
export BGP_WITHDRAW_CMD="exabgpcli withdraw route {ip}/{prefix_len} next-hop {next_hop}"
export BGP_STATUS_CMD=""
```

The status command output is parsed on a best-effort basis. GoBGP JSON output is parsed natively. For other daemons, the parser looks for keywords like `established`, `idle`, `active` in the plain-text output.

## Monitoring

### /bgp/status endpoint

The API exposes `GET /api/v1/bgp/status` which returns the current BGP session state:

```json
{
  "data": {
    "enabled": true,
    "peer_address": "192.0.2.2",
    "peer_as": 64500,
    "local_as": 64500,
    "state": "established",
    "uptime": 86400,
    "routes_announced": 3
  }
}
```

### BGP page

The **/bgp** page in the web UI shows:

- Session status (peer address, state, uptime)
- Number of active routes
- List of all active blocks with IP, reason, duration, and expiry
- Block/unblock controls (admin only)

### What to watch for

- **State != "established"**: the BGP session is down. Check the BGP daemon logs and peer connectivity.
- **routes_announced mismatch**: if the count does not match the number of active blocks in the database, routes may have been lost after a daemon restart. Restart the API server to trigger re-announcement.
