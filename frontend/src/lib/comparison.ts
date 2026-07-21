import { useSearchParams } from "react-router-dom"
import type { LinkTimeSeries, QueryFilters, TrafficPoint } from "./types"

/** Reads the current `compare=prev` overlay state from the URL (Module D). */
export function useCompareEnabled(): boolean {
  const [searchParams] = useSearchParams()
  return searchParams.get("compare") === "prev"
}

/**
 * previousWindow derives the filters for the equal-length window immediately
 * preceding the active one (Module D comparison overlay).
 *
 * When the active filters carry explicit from/to, the window is `to - from`.
 * Otherwise it falls back to `periodSeconds` relative to now. The returned
 * `prevFilters` always use explicit from/to (RFC3339) and **drop `period`**,
 * because the backend's `period` preset overrides from/to when both are sent
 * (see parseQueryParams). `windowMs` is the window length, used to time-align
 * the previous series onto the current axis via `shiftSeries`.
 */
export function previousWindow(
  filters: QueryFilters,
  periodSeconds: number,
): { prevFilters: QueryFilters; windowMs: number } {
  let fromMs: number
  let toMs: number
  if (filters.from && filters.to) {
    fromMs = new Date(filters.from).getTime()
    toMs = new Date(filters.to).getTime()
  } else {
    toMs = Date.now()
    fromMs = toMs - periodSeconds * 1000
  }
  const windowMs = Math.max(toMs - fromMs, 0)
  // Strip period so the backend honours the explicit prior-window from/to.
  const { period: _period, compare: _compare, ...rest } = filters
  void _period
  void _compare
  const prevFilters: QueryFilters = {
    ...rest,
    from: new Date(fromMs - windowMs).toISOString(),
    to: new Date(fromMs).toISOString(),
  }
  return { prevFilters, windowMs }
}

/**
 * shiftSeries returns a copy of `points` with every timestamp advanced by
 * `windowMs` so a prior-window series aligns onto the current window's time
 * axis for overlay.
 */
export function shiftSeries(points: TrafficPoint[], windowMs: number): TrafficPoint[] {
  if (windowMs <= 0) return points
  return points.map((p) => ({
    ...p,
    t: new Date(new Date(p.t).getTime() + windowMs).toISOString(),
  }))
}

/**
 * sumLinkSeries collapses a set of per-link time series into a single total
 * in/out series, summing every metric bucket by timestamp. Used to build a
 * dashboard-wide total series for the comparison overlay from data already
 * fetched per link (no extra endpoint).
 */
export function sumLinkSeries(seriesList: LinkTimeSeries[]): TrafficPoint[] {
  const byTs = new Map<string, TrafficPoint>()
  for (const s of seriesList) {
    for (const p of s.points) {
      const cur = byTs.get(p.t)
      if (cur) {
        cur.bytes_in += p.bytes_in
        cur.bytes_out += p.bytes_out || 0
        cur.packets_in = (cur.packets_in || 0) + (p.packets_in || 0)
        cur.packets_out = (cur.packets_out || 0) + (p.packets_out || 0)
      } else {
        byTs.set(p.t, {
          t: p.t,
          bytes_in: p.bytes_in,
          bytes_out: p.bytes_out || 0,
          packets_in: p.packets_in || 0,
          packets_out: p.packets_out || 0,
        })
      }
    }
  }
  return Array.from(byTs.values()).sort((a, b) => (a.t < b.t ? -1 : a.t > b.t ? 1 : 0))
}
