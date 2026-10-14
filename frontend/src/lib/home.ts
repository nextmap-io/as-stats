// Pure helpers behind the home page. Kept out of the components so the
// arithmetic (rates, "now", peaks, utilisation, state) is unit-tested.
import type { Alert, AlertSeverity, LinkCapacity, LinkTimeSeries, LiveStatus, TrafficPoint } from "./types"
import { bytesToBps } from "./rate"

/** Utilisation at or above this is "watch" (amber). */
export const UTIL_WATCH_PCT = 70
/** Utilisation at or above this is an incident (red). */
export const UTIL_CRIT_PCT = 90
export const DISK_WATCH_PCT = 80
export const DISK_CRIT_PCT = 90

export type Level = "normal" | "watch" | "critical"

// ─── Series ────────────────────────────────────────────────────────────────

/** Sums per-link series into one in/out series per timestamp (bytes and
 *  packets per bucket), sorted by time. */
export function sumSeries(lists: LinkTimeSeries[]): TrafficPoint[] {
  const byTs = new Map<string, TrafficPoint>()
  for (const s of lists) {
    for (const p of s.points) {
      const cur = byTs.get(p.t)
      if (cur) {
        cur.bytes_in += p.bytes_in || 0
        cur.bytes_out += p.bytes_out || 0
        cur.packets_in = (cur.packets_in || 0) + (p.packets_in || 0)
        cur.packets_out = (cur.packets_out || 0) + (p.packets_out || 0)
      } else {
        byTs.set(p.t, {
          t: p.t,
          bytes_in: p.bytes_in || 0,
          bytes_out: p.bytes_out || 0,
          packets_in: p.packets_in || 0,
          packets_out: p.packets_out || 0,
        })
      }
    }
  }
  return Array.from(byTs.values()).sort((a, b) => Date.parse(a.t) - Date.parse(b.t))
}

/** Groups per-link series (from the IPv4 and IPv6 responses) by link tag and
 *  sums them, so each link gets one combined series. */
export function seriesByLink(lists: LinkTimeSeries[]): Map<string, TrafficPoint[]> {
  const grouped = new Map<string, LinkTimeSeries[]>()
  for (const s of lists) {
    const g = grouped.get(s.link_tag)
    if (g) g.push(s)
    else grouped.set(s.link_tag, [s])
  }
  const out = new Map<string, TrafficPoint[]>()
  for (const [tag, g] of grouped) out.set(tag, sumSeries(g))
  return out
}

/** Drops a trailing bucket that had not finished when the window closed: a
 *  partial bucket reads as a sudden drop in "now" figures. */
export function completeBuckets(points: TrafficPoint[], stepSeconds: number, windowEnd: number): TrafficPoint[] {
  if (!points.length) return points
  const last = points[points.length - 1]
  return Date.parse(last.t) + stepSeconds * 1000 > windowEnd ? points.slice(0, -1) : points
}

export interface DirectionStats {
  /** bps over the last complete bucket. */
  now: number
  /** Mean bps over the window (total bytes ÷ window length). */
  avg: number
  /** Highest bucket, bps, and when it started. */
  peak: number
  peakAt: number | null
  /** Bytes over the window. */
  volume: number
  /** Packet counterparts (per second / total), 0 when the series has none. */
  nowPackets: number
  volumePackets: number
}

export function directionStats(
  points: TrafficPoint[],
  dir: "in" | "out",
  stepSeconds: number,
  windowEnd: number,
  windowSeconds: number,
): DirectionStats {
  const bytes = (p: TrafficPoint) => (dir === "in" ? p.bytes_in : p.bytes_out) || 0
  const pkts = (p: TrafficPoint) => (dir === "in" ? p.packets_in : p.packets_out) || 0
  let volume = 0
  let volumePackets = 0
  let peakBytes = 0
  let peakAt: number | null = null
  for (const p of points) {
    volume += bytes(p)
    volumePackets += pkts(p)
  }
  const complete = completeBuckets(points, stepSeconds, windowEnd)
  for (const p of complete) {
    if (bytes(p) > peakBytes) {
      peakBytes = bytes(p)
      peakAt = Date.parse(p.t)
    }
  }
  const last = complete[complete.length - 1]
  return {
    now: last ? bytesToBps(bytes(last), stepSeconds) : 0,
    avg: windowSeconds > 0 ? (volume * 8) / windowSeconds : 0,
    peak: bytesToBps(peakBytes, stepSeconds),
    peakAt,
    volume,
    nowPackets: last && stepSeconds > 0 ? pkts(last) / stepSeconds : 0,
    volumePackets,
  }
}

// ─── Links ─────────────────────────────────────────────────────────────────

export interface LinkRowData {
  tag: string
  name: string
  capacityBps: number
  nowIn: number
  nowOut: number
  peakIn: number
  peakOut: number
  /** Busier direction now against capacity, %; null without a capacity. */
  nowPct: number | null
  /** Busier direction's window peak against capacity, %; null without one. */
  peakPct: number | null
  volumeIn: number
  volumeOut: number
  /** Combined series for the small chart. */
  points: TrafficPoint[]
}

/**
 * buildLinkRows joins /links/capacity (capacity, current and peak rates) with
 * the per-link series already fetched for the total chart (volumes, small
 * charts). When the server predates the per-direction capacity fields (3.1),
 * current and peak rates are derived from the series instead.
 */
export function buildLinkRows(
  capacity: LinkCapacity[],
  series: Map<string, TrafficPoint[]>,
  stepSeconds: number,
  windowEnd: number,
  windowSeconds: number,
): LinkRowData[] {
  const tags = new Set<string>([...capacity.map((c) => c.tag), ...series.keys()])
  const capByTag = new Map(capacity.map((c) => [c.tag, c]))
  const rows: LinkRowData[] = []
  for (const tag of tags) {
    const cap = capByTag.get(tag)
    const points = series.get(tag) ?? []
    const sIn = directionStats(points, "in", stepSeconds, windowEnd, windowSeconds)
    const sOut = directionStats(points, "out", stepSeconds, windowEnd, windowSeconds)
    const capacityBps = (cap?.capacity_mbps ?? 0) * 1_000_000
    const nowIn = cap?.current_in_bps ?? sIn.now
    const nowOut = cap?.current_out_bps ?? sOut.now
    const peakIn = cap?.peak_in_bps ?? sIn.peak
    const peakOut = cap?.peak_out_bps ?? sOut.peak
    const pct = (bps: number) => (capacityBps > 0 ? (bps / capacityBps) * 100 : null)
    rows.push({
      tag,
      name: cap?.description || tag,
      capacityBps,
      nowIn,
      nowOut,
      peakIn,
      peakOut,
      nowPct: pct(Math.max(nowIn, nowOut)),
      peakPct: cap?.peak_utilization_pct !== undefined ? cap.peak_utilization_pct : pct(Math.max(peakIn, peakOut)),
      volumeIn: sIn.volume,
      volumeOut: sOut.volume,
      points,
    })
  }
  // Worst first: by peak utilisation, then links without a capacity by volume.
  return rows.sort((a, b) => {
    if (a.peakPct !== null && b.peakPct !== null) return b.peakPct - a.peakPct
    if (a.peakPct !== null) return -1
    if (b.peakPct !== null) return 1
    return b.volumeIn + b.volumeOut - (a.volumeIn + a.volumeOut)
  })
}

export function utilLevel(pct: number | null): Level {
  if (pct === null) return "normal"
  if (pct >= UTIL_CRIT_PCT) return "critical"
  if (pct >= UTIL_WATCH_PCT) return "watch"
  return "normal"
}

// ─── Status strip ──────────────────────────────────────────────────────────

const SEVERITY_RANK: Record<AlertSeverity, number> = { critical: 3, warning: 2, info: 1 }

/** The alert to show first: most severe, then most recently seen. */
export function topAlert(alerts: Alert[]): Alert | undefined {
  return [...alerts].sort(
    (a, b) =>
      SEVERITY_RANK[b.severity] - SEVERITY_RANK[a.severity] || Date.parse(b.last_seen_at) - Date.parse(a.last_seen_at),
  )[0]
}

/** Info alerts stay quiet; warning is "watch", critical an incident. */
export function alertsLevel(bySeverity: Record<string, number> | undefined): Level {
  if (!bySeverity) return "normal"
  if ((bySeverity.critical ?? 0) > 0) return "critical"
  if ((bySeverity.warning ?? 0) > 0) return "watch"
  return "normal"
}

export function ingestionLevel(status: LiveStatus | undefined): Level {
  if (!status) return "normal"
  if (!status.routers.length || status.routers.some((r) => r.state === "missing")) return "critical"
  if (!status.healthy) return "watch"
  return "normal"
}

export function diskLevel(pct: number | null | undefined): Level {
  if (pct === null || pct === undefined) return "normal"
  if (pct >= DISK_CRIT_PCT) return "critical"
  if (pct >= DISK_WATCH_PCT) return "watch"
  return "normal"
}

export function worst(...levels: Level[]): Level {
  if (levels.includes("critical")) return "critical"
  if (levels.includes("watch")) return "watch"
  return "normal"
}

/**
 * Alert target as a host IP, or null when the alert targets something else.
 * Link-scoped rules (link_capacity, anomaly) and disk_usage carry a synthetic
 * 0100::/64 discard-prefix address (RFC 6666) in target_ip and the real label
 * in details.extra.target; such an alert must not link to /ip/<synthetic>.
 */
export function hostTarget(alert: Pick<Alert, "target_ip">): string | null {
  const ip = alert.target_ip?.trim() ?? ""
  if (!ip || ip === "::" || ip === "0.0.0.0") return null
  const lower = ip.toLowerCase()
  if (lower.startsWith("100::") || lower.startsWith("100:0:0:0:")) return null
  return ip.startsWith("::ffff:") && ip.includes(".") ? ip.slice(7) : ip
}

/** The label to show for an alert's target: host IP, link tag, or rule scope. */
export function alertTargetLabel(alert: Pick<Alert, "target_ip" | "details">): string {
  const host = hostTarget(alert)
  if (host) return host
  try {
    const d = alert.details ? (JSON.parse(alert.details) as { extra?: { target?: unknown } }) : undefined
    const t = d?.extra?.target
    if (typeof t === "string" && t) return t
  } catch {
    /* details is free-form JSON; ignore parse errors */
  }
  return "no single host"
}
