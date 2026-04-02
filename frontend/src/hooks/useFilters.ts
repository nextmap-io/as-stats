import { useSearchParams } from "react-router-dom"
import { useMemo, useCallback } from "react"
import type { QueryFilters } from "@/lib/types"

export function useFilters() {
  const [searchParams, setSearchParams] = useSearchParams()

  const filters: QueryFilters = useMemo(() => ({
    period: searchParams.get("period") || "24h",
    from: searchParams.get("from") || undefined,
    to: searchParams.get("to") || undefined,
    link: searchParams.get("link") || undefined,
    direction: searchParams.get("direction") || undefined,
    limit: searchParams.get("limit") ? Number(searchParams.get("limit")) : undefined,
    offset: searchParams.get("offset") ? Number(searchParams.get("offset")) : undefined,
  }), [searchParams])

  const setFilter = useCallback((key: string, value: string | undefined) => {
    setSearchParams(prev => {
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
  }, [setSearchParams])

  const setFilters = useCallback((updates: Record<string, string | undefined>) => {
    setSearchParams(prev => {
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
  }, [setSearchParams])

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

  // Full period duration in seconds (for average bps calculations on totals)
  const periodSeconds = useMemo(() => {
    const p = searchParams.get("period") || "24h"
    const map: Record<string, number> = {
      "1h": 3600, "3h": 10800, "6h": 21600, "24h": 86400, "7d": 604800, "30d": 2592000,
    }
    return map[p] || 86400
  }, [searchParams])

  // Time bounds for the current period (for chart X domain)
  const timeBounds = useMemo(() => {
    const now = new Date()
    const to = now.getTime()
    const from = to - periodSeconds * 1000
    return { from, to }
  }, [periodSeconds])

  // Bucket interval in seconds (matches backend autoStep)
  const bucketSeconds = useMemo(() => {
    if (periodSeconds <= 10800) return 60    // <= 3h: 1 min
    if (periodSeconds <= 21600) return 120   // <= 6h: 2 min
    if (periodSeconds <= 129600) return 300  // <= 36h: 5 min
    if (periodSeconds <= 604800) return 3600 // <= 7d: 1h
    return 86400                              // > 7d: 24h
  }, [periodSeconds])

  return { filters, setFilter, setFilters, filterSearch, periodSeconds, bucketSeconds, timeBounds }
}
