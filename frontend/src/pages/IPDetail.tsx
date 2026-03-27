import { Link, useParams } from "react-router-dom"
import { useIPDetail } from "@/hooks/useApi"
import { useFilters } from "@/hooks/useFilters"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { TrafficChart } from "@/components/charts/TrafficChart"
import { ExpandableChart } from "@/components/ExpandableChart"
import { formatNumber } from "@/lib/utils"
import { useUnit } from "@/hooks/useUnit"

export function IPDetail() {
  const { ip } = useParams<{ ip: string }>()
  const { filters, periodSeconds, timeBounds } = useFilters()
  const { formatTraffic } = useUnit()
  const { data, isLoading, error } = useIPDetail(ip || "", filters)

  if (isLoading) return <p className="text-muted-foreground">Loading...</p>
  if (error) return <p className="text-destructive">{error.message}</p>

  const detail = data?.data
  if (!detail) return null

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold tracking-tight font-mono">{detail.ip}</h1>

      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base">Traffic</CardTitle>
        </CardHeader>
        <CardContent>
          {detail.time_series?.length > 0 ? (
            <ExpandableChart title="IP Traffic">
              <TrafficChart data={detail.time_series} height={350} />
            </ExpandableChart>
          ) : (
            <p className="text-sm text-muted-foreground">No traffic data for this period</p>
          )}
        </CardContent>
      </Card>

      {/* AS breakdown for this IP */}
      {detail.top_as?.length > 0 && (
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-base">Traffic by AS</CardTitle>
          </CardHeader>
          <CardContent>
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border">
                  <th className="pb-2 text-left font-medium text-muted-foreground">ASN</th>
                  <th className="pb-2 text-left font-medium text-muted-foreground">Name</th>
                  <th className="pb-2 text-right font-medium text-muted-foreground">Traffic</th>
                  <th className="pb-2 text-right font-medium text-muted-foreground">Flows</th>
                </tr>
              </thead>
              <tbody>
                {detail.top_as.map(as => (
                  <tr key={as.as_number} className="border-b border-border/50 last:border-0 hover:bg-muted/50">
                    <td className="py-1.5">
                      <Link to={`/as/${as.as_number}`} className="text-primary hover:underline font-mono">
                        {as.as_number}
                      </Link>
                    </td>
                    <td className="py-1.5 truncate max-w-48">{as.as_name || "-"}</td>
                    <td className="py-1.5 text-right font-mono">{formatTraffic(as.bytes, periodSeconds)}</td>
                    <td className="py-1.5 text-right font-mono text-muted-foreground">{formatNumber(as.flows)}</td>
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
