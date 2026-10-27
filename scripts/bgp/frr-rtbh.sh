#!/bin/sh
# as-stats RTBH helper for FRRouting.
#
# A blackhole is a static route to the blackhole interface plus a BGP
# "network" statement with the RTBH route-map (which sets 65535:666 and
# NO_EXPORT, see docs/bgp-blackhole.md › FRR). IPv4 and IPv6 need different
# commands, which a single as-stats template cannot express. Wire it as:
#
#   BGP_ANNOUNCE_CMD="/usr/local/bin/frr-rtbh.sh announce {prefix} {local_as}"
#   BGP_WITHDRAW_CMD="/usr/local/bin/frr-rtbh.sh withdraw {prefix} {local_as}"
#   BGP_ROUTES_CMD="/usr/local/bin/frr-rtbh.sh routes"
#   BGP_STATUS_CMD="vtysh -c 'show bgp neighbor {peer_address} json'"
#
# Option (before the action): --vtysh=vtysh. as-stats runs it without a shell
# and with a minimal environment.
#
# Exit status: 0 on success (withdrawing an absent route succeeds), 1 on a
# vtysh error, 2 on a usage error.
set -eu

vtysh=vtysh
while [ $# -gt 0 ]; do
  case "$1" in
    --vtysh=*) vtysh=${1#--vtysh=}; shift ;;
    *) break ;;
  esac
done
action=${1:-}
prefix=${2:-}
asn=${3:-}

usage() { echo "usage: $0 [--vtysh=VTYSH] announce|withdraw PREFIX LOCAL_AS | routes" >&2; exit 2; }

check() {
  case "$prefix" in */*) ;; *) usage ;; esac
  if printf '%s' "$prefix" | grep -q '[^0-9A-Fa-f:./]'; then usage; fi
  case "$asn" in ''|*[!0-9]*) usage ;; esac
  case "$prefix" in
    *:*) af="ipv6 unicast"; static="ipv6 route" ;;
    *) af="ipv4 unicast"; static="ip route" ;;
  esac
}

case "$action" in
  announce)
    check
    "$vtysh" -c "configure terminal" \
      -c "$static $prefix blackhole" \
      -c "router bgp $asn" \
      -c "address-family $af" \
      -c "network $prefix route-map RTBH" ;;
  withdraw)
    check
    if "$vtysh" -c "show running-config" | grep -qF " network $prefix route-map RTBH"; then
      "$vtysh" -c "configure terminal" \
        -c "router bgp $asn" \
        -c "address-family $af" \
        -c "no network $prefix route-map RTBH"
    fi
    if "$vtysh" -c "show running-config" | grep -qxF "$static $prefix blackhole"; then
      "$vtysh" -c "configure terminal" -c "no $static $prefix blackhole"
    fi ;;
  routes)
    # Prefixes announced through the RTBH route-map.
    "$vtysh" -c "show running-config" | sed -n 's/^ *network \([^ ]*\) route-map RTBH$/\1/p' ;;
  *) usage ;;
esac
