import { Link, useParams } from "react-router-dom"
import { useIPDetail } from "@/hooks/useApi"
import { useFilters } from "@/hooks/useFilters"
import { useFeatureFlags } from "@/hooks/useFeatures"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { TrafficChart } from "@/components/charts/TrafficChart"
import { ExpandableChart } from "@/components/ExpandableChart"
import { formatNumber } from "@/lib/utils"
import { useUnit } from "@/hooks/useUnit"
import { IPWithPTR } from "@/components/PTR"
import { Search } from "lucide-react"

const QUICK_PERIODS = ["1h", "6h", "24h", "7d"] as const

export function IPDetail() {
  const { ip } = useParams<{ ip: string }>()
  const { filters, filterSearch, periodSeconds, timeBounds, setFilter } = useFilters()
  const { formatTraffic } = useUnit()
  const features = useFeatureFlags()
  const { data, isLoading, error } = useIPDetail(ip || "", filters)

  if (isLoading) return <p className="text-muted-foreground">Loading...</p>
  if (error) return <p className="text-destructive">{error.message}</p>

  const detail = data?.data
  if (!detail) return null

  return (
    <div className="space-y-5">
      {/* Header with IP info */}
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-lg font-bold tracking-tight font-mono">{detail.ip}</h1>
          <div className="flex flex-wrap gap-x-4 gap-y-0.5 mt-1 text-xs text-muted-foreground">
            {detail.ptr && <span>{detail.ptr}</span>}
            {detail.as_number ? (
              <Link to={`/as/${detail.as_number}${filterSearch}`} className="hover:text-primary transition-colors">
                AS{detail.as_number}{detail.as_name ? ` ${detail.as_name}` : ""}
              </Link>
            ) : null}
            {detail.prefix && <span className="font-mono">{detail.prefix}</span>}
          </div>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          {/* Quick period buttons — change current period filter without losing context */}
          <div className="hidden md:flex items-center gap-0.5 px-1 py-0.5 rounded border border-input bg-muted/30">
            {QUICK_PERIODS.map(p => (
              <button
                key={p}
                onClick={() => setFilter("period", p)}
                className={`px-1.5 py-0.5 text-[10px] font-medium rounded transition-colors ${
                  filters.period === p
                    ? "bg-primary text-primary-foreground"
                    : "text-muted-foreground hover:text-foreground hover:bg-accent"
                }`}
                title={`Switch to ${p} window`}
              >
                {p}
              </button>
            ))}
          </div>
          {features.flow_search && (
            <Link
              to={`/flows?period=${filters.period || "1h"}&dst_ip=${encodeURIComponent(detail.ip)}`}
              className="inline-flex items-center gap-1.5 px-2.5 py-1 text-[11px] font-medium rounded border border-input bg-muted/50 hover:bg-accent transition-colors shrink-0"
              title="Search flows involving this IP"
            >
              <Search className="h-3 w-3" />
              View flows
            </Link>
          )}
        </div>
      </div>

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm">Traffic</CardTitle>
        </CardHeader>
        <CardContent>
          {detail.time_series?.length > 0 ? (
            <ExpandableChart title="IP Traffic">
              <TrafficChart data={detail.time_series} height={350} timeBounds={timeBounds} />
            </ExpandableChart>
          ) : (
            <p className="text-sm text-muted-foreground">No traffic data for this period</p>
          )}
        </CardContent>
      </Card>

      <div className="grid gap-5 lg:grid-cols-2">
        {/* Peer IPs */}
        {detail.peer_ips && detail.peer_ips.length > 0 && (
          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="text-sm">Top Communicating IPs</CardTitle>
            </CardHeader>
            <CardContent>
              <table className="w-full text-xs">
                <thead>
                  <tr className="border-b border-border">
                    <th className="pb-1.5 text-left font-medium text-muted-foreground">IP</th>
                    <th className="pb-1.5 text-right font-medium text-muted-foreground">Traffic</th>
                    <th className="pb-1.5 text-right font-medium text-muted-foreground hidden sm:table-cell">Flows</th>
                  </tr>
                </thead>
                <tbody>
                  {detail.peer_ips.map(peer => (
                    <tr key={peer.ip} className="border-b border-border/40 last:border-0 hover:bg-muted/50">
                      <td className="py-1">
                        <Link to={`/ip/${peer.ip}${filterSearch}`} className="text-primary hover:underline font-mono text-[11px]">
                          <IPWithPTR ip={peer.ip} />
                        </Link>
                      </td>
                      <td className="py-1 text-right font-mono">{formatTraffic(peer.bytes, periodSeconds)}</td>
                      <td className="py-1 text-right font-mono text-muted-foreground hidden sm:table-cell">{formatNumber(peer.flows)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </CardContent>
          </Card>
        )}

        {/* AS breakdown */}
        {detail.top_as?.length > 0 && (
          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="text-sm">Traffic by AS</CardTitle>
            </CardHeader>
            <CardContent>
              <table className="w-full text-xs">
                <thead>
                  <tr className="border-b border-border">
                    <th className="pb-1.5 text-left font-medium text-muted-foreground">ASN</th>
                    <th className="pb-1.5 text-left font-medium text-muted-foreground">Name</th>
                    <th className="pb-1.5 text-right font-medium text-muted-foreground">Traffic</th>
                    <th className="pb-1.5 text-right font-medium text-muted-foreground hidden sm:table-cell">Flows</th>
                  </tr>
                </thead>
                <tbody>
                  {detail.top_as.map(as => (
                    <tr key={as.as_number} className="border-b border-border/40 last:border-0 hover:bg-muted/50">
                      <td className="py-1">
                        <Link to={`/as/${as.as_number}${filterSearch}`} className="text-primary hover:underline font-mono">
                          {as.as_number}
                        </Link>
                      </td>
                      <td className="py-1 text-muted-foreground truncate max-w-40">{as.as_name || "-"}</td>
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
    </div>
  )
}
