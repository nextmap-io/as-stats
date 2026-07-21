import { useSearchParams } from "react-router-dom"
import { useMemo, useCallback } from "react"
import type { QueryFilters } from "@/lib/types"

// ─── Time-range presets (U5) ──────────────────────────────────────────────
// Relative presets → window length in seconds. The subset in BACKEND_PRESETS is
// resolved server-side (sent as ?period=); every other preset — plus the two
// windowed presets ("yesterday" / "week") and the absolute picker — is resolved
// to explicit from/to here before hitting the API (the backend already accepts
// from/to). This keeps the base install's well-tested period path unchanged
// while letting the UI offer richer ranges.
export const RELATIVE_SECONDS: Record<string, number> = {
  "15m": 900,
  "1h": 3600,
  "3h": 10800,
  "4h": 14400,
  "6h": 21600,
  "12h": 43200,
  "24h": 86400,
  "7d": 604800,
  "30d": 2592000,
}

// Presets the backend understands directly via ?period=. Everything else is
// converted to from/to. (Mirrors parseQueryParams in internal/api/handler.)
const BACKEND_PRESETS = new Set(["1h", "3h", "6h", "24h", "7d", "30d"])

export interface TimeBounds {
  from: number
  to: number
}

function startOfDay(ms: number): number {
  const d = new Date(ms)
  d.setHours(0, 0, 0, 0)
  return d.getTime()
}

function startOfWeek(ms: number): number {
  const start = startOfDay(ms)
  const d = new Date(start)
  const dow = (d.getDay() + 6) % 7 // shift so Monday = 0
  d.setDate(d.getDate() - dow)
  return d.getTime()
}

// resolveBounds turns a (period, from, to) triple into absolute ms bounds.
// `now` is snapped to the minute by the caller so relative windows produce a
// stable value within a minute (keeps TanStack Query keys from churning).
function resolveBounds(
  period: string,
  from: string | undefined,
  to: string | undefined,
  now: number,
): TimeBounds {
  if (from && to) {
    const f = Date.parse(from)
    const t = Date.parse(to)
    if (!Number.isNaN(f) && !Number.isNaN(t) && t > f) return { from: f, to: t }
  }
  if (period === "yesterday") {
    const today = startOfDay(now)
    return { from: today - 86_400_000, to: today }
  }
  if (period === "week") {
    return { from: startOfWeek(now), to: now }
  }
  const secs = RELATIVE_SECONDS[period] ?? 86_400
  return { from: now - secs * 1000, to: now }
}

function bucketFor(periodSeconds: number): number {
  if (periodSeconds <= 10800) return 60 // <= 3h: 1 min
  if (periodSeconds <= 21600) return 120 // <= 6h: 2 min
  if (periodSeconds <= 129600) return 300 // <= 36h: 5 min
  if (periodSeconds <= 604800) return 3600 // <= 7d: 1h
  return 86400 // > 7d: 24h
}

export function useFilters() {
  const [searchParams, setSearchParams] = useSearchParams()

  const rawPeriod = searchParams.get("period") || undefined
  const rawFrom = searchParams.get("from") || undefined
  const rawTo = searchParams.get("to") || undefined
  const hasAbsolute = !!(rawFrom && rawTo)

  // UI-facing period string: "custom" when an explicit from/to window is set,
  // otherwise the raw preset (defaulting to 24h). Always defined.
  const period = hasAbsolute ? "custom" : rawPeriod || "24h"

  // Resolve the active window. "now" is captured (snapped to the minute) inside
  // the memo so it stays stable across renders and only re-evaluates when the
  // period/from/to inputs change — keeping TanStack Query keys from churning.
  const bounds = useMemo<TimeBounds>(() => {
    const nowMinute = Math.floor(new Date().getTime() / 60000) * 60000
    return resolveBounds(period, rawFrom, rawTo, nowMinute)
  }, [period, rawFrom, rawTo])

  // Backend can resolve the window itself only for its known presets with no
  // explicit override. Everything else travels as from/to.
  const useBackendPreset = !hasAbsolute && BACKEND_PRESETS.has(rawPeriod ?? "24h")
  const backendPeriod = rawPeriod ?? "24h"

  const fromISO = useBackendPreset ? undefined : new Date(bounds.from).toISOString()
  const toISO = useBackendPreset ? undefined : new Date(bounds.to).toISOString()

  const link = searchParams.get("link") || undefined
  const direction = searchParams.get("direction") || undefined
  const metric = searchParams.get("metric") || undefined
  const limitStr = searchParams.get("limit")
  const offsetStr = searchParams.get("offset")

  const filters: QueryFilters = useMemo(
    () => ({
      ...(useBackendPreset ? { period: backendPeriod } : {}),
      from: fromISO,
      to: toISO,
      link,
      direction,
      metric,
      limit: limitStr ? Number(limitStr) : undefined,
      offset: offsetStr ? Number(offsetStr) : undefined,
    }),
    [useBackendPreset, backendPeriod, fromISO, toISO, link, direction, metric, limitStr, offsetStr],
  )

  const setFilter = useCallback(
    (key: string, value: string | undefined) => {
      setSearchParams((prev) => {
        const next = new URLSearchParams(prev)
        if (value === undefined || value === "") {
          next.delete(key)
        } else {
          next.set(key, value)
        }
        // Reset offset when changing filters
        if (key !== "offset") {
          next.delete("offset")
        }
        return next
      })
    },
    [setSearchParams],
  )

  const setFilters = useCallback(
    (updates: Record<string, string | undefined>) => {
      setSearchParams((prev) => {
        const next = new URLSearchParams(prev)
        Object.entries(updates).forEach(([key, value]) => {
          if (value === undefined || value === "") {
            next.delete(key)
          } else {
            next.set(key, value)
          }
        })
        return next
      })
    },
    [setSearchParams],
  )

  // Selecting a relative preset clears any lingering absolute from/to (and vice
  // versa) so the two controls never fight over the active window.
  const setPeriod = useCallback(
    (p: string) => {
      setFilters({ period: p, from: undefined, to: undefined, offset: undefined })
    },
    [setFilters],
  )

  const setAbsolute = useCallback(
    (from: string | undefined, to: string | undefined) => {
      setFilters({ from, to, period: undefined, offset: undefined })
    },
    [setFilters],
  )

  // Returns search string preserving period/time filters for cross-page navigation
  const filterSearch = useMemo(() => {
    const keep = new URLSearchParams()
    for (const key of ["period", "from", "to"]) {
      const val = searchParams.get(key)
      if (val) keep.set(key, val)
    }
    const s = keep.toString()
    return s ? `?${s}` : ""
  }, [searchParams])

  // Full period duration in seconds (for average bps calculations on totals).
  const periodSeconds = useMemo(
    () => Math.max(1, Math.round((bounds.to - bounds.from) / 1000)),
    [bounds],
  )

  // Time bounds for the current period (for chart X domain).
  const timeBounds = useMemo<TimeBounds>(() => ({ from: bounds.from, to: bounds.to }), [bounds])

  // Bucket interval in seconds (matches backend autoStep).
  const bucketSeconds = useMemo(() => bucketFor(periodSeconds), [periodSeconds])

  return {
    filters,
    setFilter,
    setFilters,
    setPeriod,
    setAbsolute,
    filterSearch,
    period,
    periodSeconds,
    bucketSeconds,
    timeBounds,
    bounds,
  }
}
