import { useMemo } from "react"
import { useParams } from "react-router-dom"
import { useLinkDetail, useLinkLoadCurve } from "@/hooks/useApi"
import { useQuery } from "@tanstack/react-query"
import { api } from "@/lib/api"
import { useFilters } from "@/hooks/useFilters"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { TrafficChart } from "@/components/charts/TrafficChart"
import { ASTrafficChart } from "@/components/charts/ASTrafficChart"
import { LoadDurationChart } from "@/components/charts/LoadDurationChart"

import { ExpandableChart } from "@/components/ExpandableChart"
import { QueryBoundary } from "@/components/QueryBoundary"
import { ComparisonToggle } from "@/components/ComparisonToggle"
import { previousWindow, shiftSeries, useCompareEnabled } from "@/lib/comparison"
import { formatNumber, formatPercent } from "@/lib/utils"
import { ASRef } from "@/components/ASRef"
import { asDisplayName } from "@/lib/asn"
import { useUnit } from "@/hooks/useUnit"
import { asSeriesColor, useChartColors } from "@/hooks/useChartColors"
import { bytesToBps, stepSecondsOf } from "@/lib/rate"
import { ErrorDisplay } from "@/components/ui/error"
import { PageSkeleton } from "@/components/ui/skeleton"
import { formatCapacity } from "@/lib/format"
import { levelTextClass, utilLevel } from "@/lib/home"
import { StateBadge } from "@/components/ui/state-badge"

/** One percentile row (p50/p95/p99) showing in + out throughput. Renders
 *  nothing when neither direction is present (older backend). */
function PercentileItem({
  label,
  inVal,
  outVal,
  stepSeconds,
}: {
  label: string
  inVal?: number
  outVal?: number
  stepSeconds: number
}) {
  const { formatTraffic } = useUnit()
  if (inVal == null && outVal == null) return null
  return (
    <div>
      <span className="text-muted-foreground">{label}</span>{" "}
      <span className="font-semibold text-traffic-in">{formatTraffic(inVal ?? 0, stepSeconds)}</span>
      <span className="text-muted-foreground"> / </span>
      <span className="font-semibold text-traffic-out">{formatTraffic(outVal ?? 0, stepSeconds)}</span>
    </div>
  )
}

export function LinkDetail() {
  const { tag } = useParams<{ tag: string }>()
  const { filters, filterSearch, periodSeconds, timeBounds, bounds } = useFilters()
  const chartColors = useChartColors()
  const { formatTraffic } = useUnit()
  const { data, isLoading, error, refetch } = useLinkDetail(tag || "", filters)
  const loadCurveQuery = useLinkLoadCurve(tag || "", filters)

  // Comparison overlay (Module D). When off, the prev query reuses the active
  // filters so it dedupes with the main query — no extra request.
  const compare = useCompareEnabled()
  // Memoized on the minute-snapped bounds: prevFilters feeds a query key.
  const { prevFilters, windowMs } = useMemo(
    () => previousWindow(filters, bounds),
    [filters, bounds],
  )
  const prevDetail = useLinkDetail(tag || "", compare ? prevFilters : filters)
  const prevSeries =
    compare && prevDetail.data?.data?.time_series
      ? shiftSeries(prevDetail.data.data.time_series, windowMs)
      : undefined

  const { data: linksData } = useQuery({
    queryKey: ["admin-links"],
    queryFn: () => api.adminLinks(),
    staleTime: 30_000,
  })
  const linkConfig = linksData?.data?.find(l => l.tag === tag)

  // Same loading / error vocabulary as every other page: a flat skeleton, and
  // an error that names what failed and offers a retry.
  if (isLoading) return <PageSkeleton />
  if (error) return <ErrorDisplay error={error} title={`Could not load link ${tag}`} onRetry={() => void refetch()} />

  const detail = data?.data
  if (!detail) return null

  // Capacity + utilization come from the API (detail); fall back to the link
  // config only for capacity if the detail response predates the field.
  const capacityMbps = detail.capacity_mbps || linkConfig?.capacity_mbps || 0
  const capacityBps = capacityMbps * 1_000_000
  // Bucket width the server used for the series and percentiles (3.1+), or the
  // legacy guess on older servers.
  const step = stepSecondsOf(data?.meta, periodSeconds)
  const p95InBps = detail.p95_in ? bytesToBps(detail.p95_in, step) : 0
  const p95OutBps = detail.p95_out ? bytesToBps(detail.p95_out, step) : 0
  const p95MaxBps = Math.max(p95InBps, p95OutBps)
  // utilization_pct is the busier direction's p95 against capacity; null when
  // capacity is unset.
  const utilizationPct =
    detail.utilization_pct != null
      ? detail.utilization_pct
      : capacityBps > 0
        ? (p95MaxBps / capacityBps) * 100
        : null

  // Colors for AS in the chart and table
  const asColors: Record<number, string> = {}
  if (detail.as_series) {
    detail.as_series.forEach((d, i) => { asColors[d.as_number] = asSeriesColor(chartColors, i) })
  }

  return (
    <div className="space-y-5">
      <div className="flex items-start justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold">{linkConfig?.description || detail.tag}</h1>
          {linkConfig?.description && (
            <p className="mt-0.5 text-[13px] text-muted-foreground">Link tag {detail.tag}</p>
          )}
        </div>
        <ComparisonToggle className="shrink-0" />
      </div>

      {/* Percentiles (p50 / p95 / p99, in & out) + Capacity */}
      <div className="flex flex-wrap gap-x-6 gap-y-1 text-sm">
        <PercentileItem label="p50" inVal={detail.p50_in} outVal={detail.p50_out} stepSeconds={step} />
        <PercentileItem label="p95" inVal={detail.p95_in} outVal={detail.p95_out} stepSeconds={step} />
        <PercentileItem label="p99" inVal={detail.p99_in} outVal={detail.p99_out} stepSeconds={step} />
        {capacityMbps > 0 && (
          <>
            <div><span className="text-muted-foreground">Capacity</span> <span className="font-semibold">{formatCapacity(capacityBps)}</span></div>
            {utilizationPct != null && (
              <div>
                <span className="text-muted-foreground">p95 utilisation, busier direction</span>{" "}
                <span className={`font-semibold ${levelTextClass(utilLevel(utilizationPct))}`}>
                  {formatPercent(utilizationPct)}
                </span>
                {/* Colour never carries the state alone: the same words as the home strip. */}
                <StateBadge level={utilLevel(utilizationPct)} className="ml-1.5" />
              </div>
            )}
          </>
        )}
      </div>

      {/* Total traffic vs previous period (comparison overlay, opt-in) */}
      {compare && detail.time_series?.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>Total traffic against the previous period</CardTitle>
          </CardHeader>
          <CardContent>
            <TrafficChart
              data={detail.time_series}
              previous={prevSeries}
              height={300}
              p95In={detail.p95_in}
              p95Out={detail.p95_out}
              timeBounds={timeBounds}
              stepSeconds={step}
            />
          </CardContent>
        </Card>
      )}

      {/* Top AS stacked chart */}
      {detail.as_series && detail.as_series.length > 0 && (
        <Card>
          <CardContent className="pt-3">
            <ExpandableChart title={`Top AS on ${detail.tag}`} fetchType="link-detail" fetchParams={{ tag: tag || "" }} currentPeriod={filters.period}>
              <ASTrafficChart data={detail.as_series} title="Traffic by AS" height={350} timeBounds={timeBounds} stepSeconds={step} />
            </ExpandableChart>
          </CardContent>
        </Card>
      )}

      {/* Fallback: simple traffic chart if no AS series */}
      {(!detail.as_series || detail.as_series.length === 0) && detail.time_series?.length > 0 && (
        <Card>
          <CardHeader><CardTitle>Traffic</CardTitle></CardHeader>
          <CardContent>
            <ExpandableChart title="Link Traffic" fetchType="link-detail" fetchParams={{ tag: tag || "" }} currentPeriod={filters.period}>
              <TrafficChart data={detail.time_series} height={350} p95In={detail.p95_in} p95Out={detail.p95_out} timeBounds={timeBounds} stepSeconds={step} />
            </ExpandableChart>
          </CardContent>
        </Card>
      )}

      {/* Load-duration curve */}
      <Card>
        <CardHeader>
          <CardTitle>Load-duration curve</CardTitle>
        </CardHeader>
        <CardContent>
          <QueryBoundary
            query={loadCurveQuery}
            isEmpty={(d) => !d.data?.points?.length}
            skeleton={<div className="h-[300px] rounded bg-muted/40" />}
          >
            {(d) => (
              <ExpandableChart title={`Load-Duration Curve — ${detail.tag}`}>
                <LoadDurationChart curve={d.data} capacityBps={capacityBps} height={300} />
              </ExpandableChart>
            )}
          </QueryBoundary>
          <p className="mt-2 text-xs text-muted-foreground">
            Throughput sorted descending against the fraction of the window it was met or exceeded.
            {capacityBps > 0 && " The dashed line marks the configured capacity."}
          </p>
        </CardContent>
      </Card>

      {/* Top AS table with color dots */}
      {detail.top_as?.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>Top AS on this link</CardTitle>
          </CardHeader>
          <CardContent>
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-foreground">
                  <th className="pb-1.5 w-4"></th>
                  <th className="pb-1.5 text-left text-[13px] font-normal text-muted-foreground">ASN</th>
                  <th className="pb-1.5 text-left text-[13px] font-normal text-muted-foreground">Name</th>
                  <th className="pb-1.5 text-right text-[13px] font-normal text-muted-foreground">Traffic</th>
                  <th className="pb-1.5 text-right text-[13px] font-normal text-muted-foreground hidden sm:table-cell">Flows</th>
                </tr>
              </thead>
              <tbody>
                {detail.top_as.map((as) => (
                  <tr key={as.as_number} className="border-b border-border-soft last:border-0 hover:bg-accent/60">
                    <td className="py-1">
                      {/* The chart's colour key; networks it does not draw get none. */}
                      {asColors[as.as_number] && (
                        <span
                          aria-hidden
                          className="inline-block size-3 rounded-sm"
                          style={{ backgroundColor: asColors[as.as_number] }}
                        />
                      )}
                    </td>
                    <td className="py-1">
                      <ASRef
                        asn={as.as_number}
                        search={filterSearch}
                        prefix={false}
                        className="text-primary hover:underline"
                      />
                    </td>
                    <td className="py-1 text-muted-foreground truncate max-w-48">{asDisplayName(as.as_number, as.as_name)}</td>
                    <td className="py-1 text-right">{formatTraffic(as.bytes, periodSeconds, as.packets)}</td>
                    <td className="py-1 text-right text-muted-foreground hidden sm:table-cell">{formatNumber(as.flows)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </CardContent>
        </Card>
      )}
    </div>
  )
}
