import { useSearchParams } from "react-router-dom"
import type { QueryFilters, TrafficPoint } from "./types"

/** Reads the current `compare=prev` overlay state from the URL (Module D). */
export function useCompareEnabled(): boolean {
  const [searchParams] = useSearchParams()
  return searchParams.get("compare") === "prev"
}

/**
 * previousWindow derives the filters for the equal-length window immediately
 * preceding the active one (Module D comparison overlay).
 *
 * `bounds` is the *already resolved* active window from `useFilters()`, which is
 * memoized and snapped to the minute. Taking it as an argument — rather than
 * reading the clock here — is load-bearing: this function feeds a TanStack Query
 * key, and a `Date.now()` fallback produced millisecond-precision ISO strings
 * that differed on every render, so the key never stabilised and each render
 * mounted a fresh query (an endless refetch loop against /links/traffic).
 *
 * The returned `prevFilters` always use explicit from/to (RFC3339) and **drop
 * `period`**, because the backend's `period` preset overrides from/to when both
 * are sent (see parseQueryParams). `windowMs` is the window length, used to
 * time-align the previous series onto the current axis via `shiftSeries`.
 */
export function previousWindow(
  filters: QueryFilters,
  bounds: { from: number; to: number },
): { prevFilters: QueryFilters; windowMs: number } {
  const fromMs = bounds.from
  const toMs = bounds.to
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

