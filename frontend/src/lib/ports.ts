/**
 * Port statistics semantics (server 3.5+, migration 000018): every flow is
 * counted once, under the direction of the link it was seen on — "in" =
 * entered our network, "out" = left it — and under its SERVICE port: the lower
 * of its two ports (client ports are ephemeral, i.e. high). ICMP has no port
 * (0).
 */
export const PORT_STATS_HELP =
  "Each flow counts once: inbound = entering the network on a configured link, outbound = leaving it, " +
  "filed under its service port (the lower of its two ports; client ports are ephemeral)."

/** portLabel renders a service port; ICMP / ICMPv6 rows carry port 0. */
export function portLabel(port: number): string {
  return port > 0 ? String(port) : "—"
}

/**
 * flowSearchPath is the Flow Search drill-down of a Top Ports row: the service
 * port on either side (the server port is the destination of requests and the
 * source of responses), keeping the page's filters.
 */
export function flowSearchPath(filterSearch: string, protocol: number, port: number): string {
  const q = new URLSearchParams(filterSearch.startsWith("?") ? filterSearch.slice(1) : filterSearch)
  q.set("protocol", String(protocol))
  if (port > 0) q.set("port", String(port))
  return `/flows?${q.toString()}`
}

/**
 * portHistoryNote returns the sentence shown when the selected window reaches
 * back before the current port history (`meta.port_history_since`), or null.
 * `comparison` is for Changes, whose movers compare with the prior window.
 */
export function portHistoryNote(since: string | undefined, comparison = false): string | null {
  if (!since) return null
  const t = Date.parse(since)
  if (Number.isNaN(t)) return null
  const when = new Date(t).toISOString().slice(0, 16).replace("T", " ") + " UTC"
  return comparison
    ? `Port statistics restarted at the 3.5 upgrade (${when}): the prior window is incomplete, so most ports show as gainers until it has passed.`
    : `Port statistics restarted at the 3.5 upgrade (${when}): this window only holds the traffic since then. The earlier figures counted every flow twice and are not shown.`
}
