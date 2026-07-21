import { Link, useParams } from "react-router-dom"
import { useLinkDetail, useLinkLoadCurve } from "@/hooks/useApi"
import { useQuery } from "@tanstack/react-query"
import { api } from "@/lib/api"
import { useFilters } from "@/hooks/useFilters"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { TrafficChart } from "@/components/charts/TrafficChart"
import { ASTrafficChart } from "@/components/charts/ASTrafficChart"
import { LoadDurationChart } from "@/components/charts/LoadDurationChart"

const AS_COLORS = ["#e74c3c", "#3498db", "#2ecc71", "#f39c12", "#9b59b6", "#1abc9c", "#e67e22", "#2980b9", "#e91e63", "#00bcd4"]
import { ExpandableChart } from "@/components/ExpandableChart"
import { QueryBoundary } from "@/components/QueryBoundary"
import { ComparisonToggle } from "@/components/ComparisonToggle"
import { previousWindow, shiftSeries, useCompareEnabled } from "@/lib/comparison"
import { formatNumber, formatPercent } from "@/lib/utils"
import { useUnit } from "@/hooks/useUnit"

/** One percentile row (p50/p95/p99) showing in + out throughput. Renders
 *  nothing when neither direction is present (older backend). */
function PercentileItem({
  label,
  inVal,
  outVal,
  bucketSeconds,
}: {
  label: string
  inVal?: number
  outVal?: number
  bucketSeconds: number
}) {
  const { formatTraffic } = useUnit()
  if (inVal == null && outVal == null) return null
  return (
    <div>
      <span className="text-muted-foreground uppercase tracking-wide text-[10px]">{label}</span>{" "}
      <span className="font-semibold text-traffic-in">{formatTraffic(inVal ?? 0, bucketSeconds)}</span>
      <span className="text-muted-foreground"> / </span>
      <span className="font-semibold text-traffic-out">{formatTraffic(outVal ?? 0, bucketSeconds)}</span>
    </div>
  )
}

export function LinkDetail() {
  const { tag } = useParams<{ tag: string }>()
  const { filters, filterSearch, periodSeconds, bucketSeconds, timeBounds } = useFilters()
  const { formatTraffic } = useUnit()
  const { data, isLoading, error } = useLinkDetail(tag || "", filters)
  const loadCurveQuery = useLinkLoadCurve(tag || "", filters)

  // Comparison overlay (Module D). When off, the prev query reuses the active
  // filters so it dedupes with the main query — no extra request.
  const compare = useCompareEnabled()
  const { prevFilters, windowMs } = previousWindow(filters, periodSeconds)
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

  if (isLoading) return <p className="text-muted-foreground">Loading…</p>
  if (error) return <p className="text-destructive">{error.message}</p>

  const detail = data?.data
  if (!detail) return null

  // Capacity + utilization come from the API (detail); fall back to the link
  // config only for capacity if the detail response predates the field.
  const capacityMbps = detail.capacity_mbps || linkConfig?.capacity_mbps || 0
  const capacityBps = capacityMbps * 1_000_000
  const p95InBps = detail.p95_in ? (detail.p95_in * 8) / bucketSeconds : 0
  const p95OutBps = detail.p95_out ? (detail.p95_out * 8) / bucketSeconds : 0
  const p95MaxBps = Math.max(p95InBps, p95OutBps)
  // utilization_pct is p95(in+out) / capacity; nil when capacity unset.
  const utilizationPct =
    detail.utilization_pct != null
      ? detail.utilization_pct
      : capacityBps > 0
        ? (p95MaxBps / capacityBps) * 100
        : null

  // Colors for AS in the chart and table
  const asColors: Record<number, string> = {}
  if (detail.as_series) {
    detail.as_series.forEach((d, i) => { asColors[d.as_number] = AS_COLORS[i % AS_COLORS.length] })
  }

  return (
    <div className="space-y-5">
      <div className="flex items-start justify-between gap-3">
        <div>
          <h1 className="text-lg font-semibold tracking-tight">Link: {detail.tag}</h1>
          {linkConfig?.description && (
            <p className="text-xs text-muted-foreground mt-0.5">{linkConfig.description}</p>
          )}
        </div>
        <ComparisonToggle className="shrink-0" />
      </div>

      {/* Percentiles (p50 / p95 / p99, in & out) + Capacity */}
      <div className="flex flex-wrap gap-x-6 gap-y-1 text-xs">
        <PercentileItem label="p50" inVal={detail.p50_in} outVal={detail.p50_out} bucketSeconds={bucketSeconds} />
        <PercentileItem label="p95" inVal={detail.p95_in} outVal={detail.p95_out} bucketSeconds={bucketSeconds} />
        <PercentileItem label="p99" inVal={detail.p99_in} outVal={detail.p99_out} bucketSeconds={bucketSeconds} />
        {capacityMbps > 0 && (
          <>
            <div><span className="text-muted-foreground">Capacity:</span> <span className="font-semibold">{capacityMbps.toLocaleString()} Mbps</span></div>
            {utilizationPct != null && (
              <div><span className="text-muted-foreground">Utilization:</span> <span className={`font-semibold ${utilizationPct >= 90 ? "text-destructive" : utilizationPct >= 70 ? "text-warning" : "text-success"}`}>{formatPercent(utilizationPct)}</span></div>
            )}
          </>
        )}
      </div>

      {/* Total traffic vs previous period (comparison overlay, opt-in) */}
      {compare && detail.time_series?.length > 0 && (
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm">Total traffic vs previous period</CardTitle>
          </CardHeader>
          <CardContent>
            <TrafficChart
              data={detail.time_series}
              previous={prevSeries}
              height={300}
              p95In={detail.p95_in}
              p95Out={detail.p95_out}
              timeBounds={timeBounds}
            />
          </CardContent>
        </Card>
      )}

      {/* Top AS stacked chart */}
      {detail.as_series && detail.as_series.length > 0 && (
        <Card>
          <CardContent className="px-4 pt-5 pb-4">
            <ExpandableChart title={`Top AS on ${detail.tag}`} fetchType="link-detail" fetchParams={{ tag: tag || "" }} currentPeriod={filters.period}>
              <ASTrafficChart data={detail.as_series} title="Traffic by AS" height={350} timeBounds={timeBounds} />
            </ExpandableChart>
          </CardContent>
        </Card>
      )}

      {/* Fallback: simple traffic chart if no AS series */}
      {(!detail.as_series || detail.as_series.length === 0) && detail.time_series?.length > 0 && (
        <Card>
          <CardHeader className="pb-2"><CardTitle className="text-sm">Traffic</CardTitle></CardHeader>
          <CardContent>
            <ExpandableChart title="Link Traffic" fetchType="link-detail" fetchParams={{ tag: tag || "" }} currentPeriod={filters.period}>
              <TrafficChart data={detail.time_series} height={350} p95In={detail.p95_in} p95Out={detail.p95_out} timeBounds={timeBounds} />
            </ExpandableChart>
          </CardContent>
        </Card>
      )}

      {/* Load-duration curve */}
      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm">Load-Duration Curve</CardTitle>
        </CardHeader>
        <CardContent>
          <QueryBoundary
            query={loadCurveQuery}
            isEmpty={(d) => d.data.points.length === 0}
            skeleton={<div className="h-[300px] animate-pulse rounded bg-muted/40" />}
          >
            {(d) => (
              <ExpandableChart title={`Load-Duration Curve — ${detail.tag}`}>
                <LoadDurationChart curve={d.data} capacityBps={capacityBps} height={300} />
              </ExpandableChart>
            )}
          </QueryBoundary>
          <p className="mt-2 text-[10px] text-muted-foreground">
            Throughput sorted descending against the fraction of the window it was met or exceeded.
            {capacityBps > 0 && " The dashed line marks the configured capacity."}
          </p>
        </CardContent>
      </Card>

      {/* Top AS table with color dots */}
      {detail.top_as?.length > 0 && (
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm">Top AS on this link</CardTitle>
          </CardHeader>
          <CardContent>
            <table className="w-full text-xs">
              <thead>
                <tr className="border-b border-border">
                  <th className="pb-1.5 w-4"></th>
                  <th className="pb-1.5 text-left font-medium text-muted-foreground">ASN</th>
                  <th className="pb-1.5 text-left font-medium text-muted-foreground">Name</th>
                  <th className="pb-1.5 text-right font-medium text-muted-foreground">Traffic</th>
                  <th className="pb-1.5 text-right font-medium text-muted-foreground hidden sm:table-cell">Flows</th>
                </tr>
              </thead>
              <tbody>
                {detail.top_as.map((as) => (
                  <tr key={as.as_number} className="border-b border-border/40 last:border-0 hover:bg-muted/50">
                    <td className="py-1">
                      <span
                        className="inline-block size-3 rounded-sm"
                        style={{ backgroundColor: asColors[as.as_number] || "#888" }}
                      />
                    </td>
                    <td className="py-1">
                      <Link to={`/as/${as.as_number}${filterSearch}`} className="text-primary hover:underline font-mono">
                        {as.as_number}
                      </Link>
                    </td>
                    <td className="py-1 text-muted-foreground truncate max-w-48">{as.as_name || "-"}</td>
                    <td className="py-1 text-right font-mono">{formatTraffic(as.bytes, periodSeconds)}</td>
                    <td className="py-1 text-right font-mono text-muted-foreground hidden sm:table-cell">{formatNumber(as.flows)}</td>
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
