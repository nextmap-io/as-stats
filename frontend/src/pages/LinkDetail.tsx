import { Link, useParams } from "react-router-dom"
import { useLinkDetail } from "@/hooks/useApi"
import { useQuery } from "@tanstack/react-query"
import { api } from "@/lib/api"
import { useFilters } from "@/hooks/useFilters"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { TrafficChart } from "@/components/charts/TrafficChart"
import { ASTrafficChart } from "@/components/charts/ASTrafficChart"

const AS_COLORS = ["#e74c3c", "#3498db", "#2ecc71", "#f39c12", "#9b59b6", "#1abc9c", "#e67e22", "#2980b9", "#e91e63", "#00bcd4"]
import { ExpandableChart } from "@/components/ExpandableChart"
import { formatNumber } from "@/lib/utils"
import { useUnit } from "@/hooks/useUnit"

export function LinkDetail() {
  const { tag } = useParams<{ tag: string }>()
  const { filters, filterSearch, periodSeconds, bucketSeconds, timeBounds } = useFilters()
  const { formatTraffic } = useUnit()
  const { data, isLoading, error } = useLinkDetail(tag || "", filters)

  const { data: linksData } = useQuery({
    queryKey: ["admin-links"],
    queryFn: () => api.adminLinks(),
    staleTime: 30_000,
  })
  const linkConfig = linksData?.data?.find(l => l.tag === tag)

  if (isLoading) return <p className="text-muted-foreground">Loading...</p>
  if (error) return <p className="text-destructive">{error.message}</p>

  const detail = data?.data
  if (!detail) return null

  const capacityBps = (linkConfig?.capacity_mbps || 0) * 1_000_000
  const p95InBps = detail.p95_in ? (detail.p95_in * 8) / bucketSeconds : 0
  const p95OutBps = detail.p95_out ? (detail.p95_out * 8) / bucketSeconds : 0
  const p95MaxBps = Math.max(p95InBps, p95OutBps)

  // Colors for AS in the chart and table
  const asColors: Record<number, string> = {}
  if (detail.as_series) {
    detail.as_series.forEach((d, i) => { asColors[d.as_number] = AS_COLORS[i % AS_COLORS.length] })
  }

  return (
    <div className="space-y-5">
      <div>
        <h1 className="text-lg font-bold tracking-tight">Link: {detail.tag}</h1>
        {linkConfig?.description && (
          <p className="text-xs text-muted-foreground mt-0.5">{linkConfig.description}</p>
        )}
      </div>

      {/* P95 + Capacity */}
      <div className="flex flex-wrap gap-x-6 gap-y-1 text-xs">
        {detail.p95_in != null && (
          <div><span className="text-muted-foreground">P95 in:</span> <span className="font-semibold text-traffic-in">{formatTraffic(detail.p95_in, bucketSeconds)}</span></div>
        )}
        {detail.p95_out != null && (
          <div><span className="text-muted-foreground">P95 out:</span> <span className="font-semibold text-traffic-out">{formatTraffic(detail.p95_out, bucketSeconds)}</span></div>
        )}
        {capacityBps > 0 && (
          <>
            <div><span className="text-muted-foreground">Capacity:</span> <span className="font-semibold">{linkConfig?.capacity_mbps} Mbps</span></div>
            <div><span className="text-muted-foreground">Usage:</span> <span className={`font-semibold ${p95MaxBps / capacityBps > 0.8 ? "text-destructive" : p95MaxBps / capacityBps > 0.5 ? "text-warning" : "text-success"}`}>{((p95MaxBps / capacityBps) * 100).toFixed(1)}%</span></div>
          </>
        )}
      </div>

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
                        className="inline-block w-3 h-3 rounded-sm"
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
