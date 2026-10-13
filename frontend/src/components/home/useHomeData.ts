import { useMemo } from "react"
import { useLinksCapacity, useLinksTraffic } from "@/hooks/useApi"
import { useFilters } from "@/hooks/useFilters"
import { buildLinkRows, seriesByLink, sumSeries } from "@/lib/home"
import { stepSecondsOf } from "@/lib/rate"

/**
 * useHomeData fetches what the home page shows, once: per-link series for
 * IPv4 and IPv6 (they feed the total chart, the IP-version switch, link
 * volumes and the small link charts) and /links/capacity. Switching between
 * Both / IPv4 / IPv6 only re-sums cached data; it never refetches.
 */
export function useHomeData() {
  const { filters, periodSeconds, bounds } = useFilters()
  const v4 = useLinksTraffic(4, filters)
  const v6 = useLinksTraffic(6, filters)
  const capacity = useLinksCapacity(filters)

  const step = stepSecondsOf(v4.data?.meta ?? v6.data?.meta, periodSeconds)

  const derived = useMemo(() => {
    const v4Series = v4.data?.data ?? []
    const v6Series = v6.data?.data ?? []
    const all = [...v4Series, ...v6Series]
    return {
      total: { both: sumSeries(all), v4: sumSeries(v4Series), v6: sumSeries(v6Series) },
      links: buildLinkRows(capacity.data?.data ?? [], seriesByLink(all), step, bounds.to, periodSeconds),
    }
  }, [v4.data, v6.data, capacity.data, step, bounds.to, periodSeconds])

  return {
    step,
    total: derived.total,
    links: derived.links,
    seriesLoading: v4.isLoading || v6.isLoading,
    seriesError: v4.error ?? v6.error,
    refetchSeries: () => {
      void v4.refetch()
      void v6.refetch()
    },
    capacityLoading: capacity.isLoading,
    capacityError: capacity.error,
  }
}
