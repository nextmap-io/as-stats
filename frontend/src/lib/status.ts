import type { LiveRouter, LiveStatus, RouterState, StatusData } from "./types"

// Thresholds mirror the backend's /status/live semantics (contract §1).
const OK_SECONDS = 120
const STALE_SECONDS = 600

/** Strips the IPv4-mapped prefix so 10.0.0.1 reads as an IPv4 address. */
export function displayIP(ip: string): string {
  return ip.startsWith("::ffff:") && ip.includes(".") ? ip.slice(7) : ip
}

export function routerStateFromAge(lastSeen: string, now: number): RouterState {
  const t = Date.parse(lastSeen)
  if (!lastSeen || Number.isNaN(t)) return "missing"
  const age = (now - t) / 1000
  if (age < OK_SECONDS) return "ok"
  if (age < STALE_SECONDS) return "stale"
  return "missing"
}

/**
 * liveStatusFromLegacy approximates GET /status/live from the windowed
 * GET /status of servers older than 3.1, queried over the last hour. Router
 * states come from the age of each router's last flow; flows over 10 minutes
 * are estimated from the hourly count; disk usage is unknown there.
 */
export function liveStatusFromLegacy(status: StatusData, now: number): LiveStatus {
  const routers: LiveRouter[] = (status.routers ?? []).map((r) => ({
    router_ip: displayIP(r.router_ip),
    last_seen: r.last_seen,
    flows_10m: Math.round(r.flow_count / 6),
    state: routerStateFromAge(r.last_seen, now),
  }))
  return {
    healthy: routers.length > 0 && routers.every((r) => r.state === "ok"),
    routers,
    disk_used_pct: null,
  }
}

/** Seconds since an RFC 3339 instant, or null when it is empty / invalid. */
export function ageSeconds(iso: string, now: number): number | null {
  const t = Date.parse(iso)
  if (!iso || Number.isNaN(t)) return null
  return Math.max(0, Math.round((now - t) / 1000))
}

export function formatAge(seconds: number | null): string {
  if (seconds === null) return "never"
  if (seconds < 60) return `${seconds} s ago`
  if (seconds < 3600) return `${Math.round(seconds / 60)} min ago`
  if (seconds < 86400) return `${Math.round(seconds / 3600)} h ago`
  return `${Math.round(seconds / 86400)} d ago`
}

export const ROUTER_STATE_LABEL: Record<RouterState, string> = {
  ok: "reporting",
  stale: "late",
  missing: "silent",
}

/** One line per router, for the header tooltip. */
export function describeRouters(status: LiveStatus, now: number): string {
  if (!status.routers.length) return "No router has sent flows in the last 10 minutes"
  return status.routers
    .map((r) => `${displayIP(r.router_ip)}: ${ROUTER_STATE_LABEL[r.state]}, last flow ${formatAge(ageSeconds(r.last_seen, now))}`)
    .join("\n")
}

/** Total flow records per second over the 10-minute window. */
export function flowsPerSecond(status: LiveStatus): number {
  return status.routers.reduce((sum, r) => sum + (r.flows_10m || 0), 0) / 600
}
