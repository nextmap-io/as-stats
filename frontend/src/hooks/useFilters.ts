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

  return { filters, setFilter, setFilters }
}
