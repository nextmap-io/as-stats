#!/bin/sh
# as-stats RTBH helper for BIRD 2.
#
# BIRD has no runtime "route add": blackholes live in two include files read
# by static protocols (see docs/bgp-blackhole.md › BIRD), and this script
# edits them and asks BIRD to reconfigure. Wire it as:
#
#   BGP_ANNOUNCE_CMD="/usr/local/bin/bird-rtbh.sh announce {prefix}"
#   BGP_WITHDRAW_CMD="/usr/local/bin/bird-rtbh.sh withdraw {prefix}"
#   BGP_ROUTES_CMD="/usr/local/bin/bird-rtbh.sh routes"
#   BGP_STATUS_CMD="/usr/local/bin/bird-rtbh.sh status rtbh_upstream"
#
# Options (before the action): --dir=/etc/bird (where rtbh4.conf / rtbh6.conf
# live), --birdc=birdc (path, or e.g. "birdc -s /run/bird/bird.ctl" is not
# supported: use a wrapper). as-stats runs it without a shell and with a
# minimal environment, so everything is passed as arguments.
#
# Exit status: 0 on success (withdrawing an absent route succeeds), 1 on a
# BIRD error, 2 on a usage error.
set -eu

dir=/etc/bird
birdc=birdc
while [ $# -gt 0 ]; do
  case "$1" in
    --dir=*) dir=${1#--dir=}; shift ;;
    --birdc=*) birdc=${1#--birdc=}; shift ;;
    *) break ;;
  esac
done
action=${1:-}
arg=${2:-}

usage() { echo "usage: $0 [--dir=DIR] [--birdc=BIRDC] announce|withdraw PREFIX | routes | status PROTOCOL" >&2; exit 2; }

check_prefix() {
  # Only address/length characters: the value is written into a BIRD config.
  case "$1" in
    */*) ;;
    *) usage ;;
  esac
  if printf '%s' "$1" | grep -q '[^0-9A-Fa-f:./]'; then usage; fi
}

file_for() {
  case "$1" in
    *:*) echo "$dir/rtbh6.conf" ;;
    *) echo "$dir/rtbh4.conf" ;;
  esac
}

reconfigure() {
  out=$("$birdc" configure 2>&1) || { echo "$out" >&2; return 1; }
  case "$out" in
    *"Reconfigured"*|*"Reconfiguration in progress"*|*"Reconfiguration already in progress"*) return 0 ;;
    *) echo "$out" >&2; return 1 ;;
  esac
}

# Serialise edits (as-stats already does; this protects manual runs too).
lock() {
  exec 9>"$dir/.rtbh.lock"
  if command -v flock >/dev/null 2>&1; then flock -w 10 9; fi
}

case "$action" in
  announce)
    check_prefix "$arg"; lock
    f=$(file_for "$arg"); touch "$f"
    line="route $arg blackhole;"
    if ! grep -qxF "$line" "$f"; then
      tmp="$f.tmp.$$"; cp "$f" "$tmp"; echo "$line" >> "$tmp"; mv "$tmp" "$f"
    fi
    reconfigure ;;
  withdraw)
    check_prefix "$arg"; lock
    f=$(file_for "$arg"); touch "$f"
    line="route $arg blackhole;"
    if grep -qxF "$line" "$f"; then
      tmp="$f.tmp.$$"; grep -vxF "$line" "$f" > "$tmp" || true; mv "$tmp" "$f"
      reconfigure
    fi ;;
  routes)
    # What BIRD actually carries, not what the files say.
    "$birdc" show route protocol rtbh4 2>/dev/null || true
    "$birdc" show route protocol rtbh6 2>/dev/null || true ;;
  status)
    [ -n "$arg" ] || usage
    case "$arg" in *[!A-Za-z0-9_]*) usage ;; esac
    "$birdc" show protocols all "$arg" ;;
  *) usage ;;
esac
