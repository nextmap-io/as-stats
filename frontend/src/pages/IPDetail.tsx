import { Link, useParams } from "react-router-dom"
import { useIPDetail } from "@/hooks/useApi"
import { useFilters } from "@/hooks/useFilters"
import { useFeatureFlags } from "@/hooks/useFeatures"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { TrafficChart } from "@/components/charts/TrafficChart"
import { stepSecondsOf } from "@/lib/rate"
import { ExpandableChart } from "@/components/ExpandableChart"
import { formatNumber } from "@/lib/utils"
import { ASRef } from "@/components/ASRef"
import { asDisplayName } from "@/lib/asn"
import { useUnit } from "@/hooks/useUnit"
import { IPWithPTR } from "@/components/PTR"
import { Search } from "lucide-react"
import { ErrorDisplay } from "@/components/ui/error"
import { PageSkeleton } from "@/components/ui/skeleton"
import { HostAlerts } from "@/components/HostAlerts"


export function IPDetail() {
  const { ip } = useParams<{ ip: string }>()
  const { filters, filterSearch, periodSeconds, timeBounds } = useFilters()
  const { formatTraffic } = useUnit()
  const features = useFeatureFlags()
  const { data, isLoading, error, refetch } = useIPDetail(ip || "", filters)

  // Same loading / error vocabulary as every other page: a flat skeleton, and
  // an error that names what failed and offers a retry.
  if (isLoading) return <PageSkeleton />
  if (error) return <ErrorDisplay error={error} title={`Could not load ${ip}`} onRetry={() => void refetch()} />

  const detail = data?.data
  if (!detail) return null

  return (
    <div className="space-y-5">
      {/* Header with IP info */}
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold font-ip">{detail.ip}</h1>
          <div className="flex flex-wrap gap-x-4 gap-y-0.5 mt-1 text-xs text-muted-foreground">
            {detail.ptr && <span>{detail.ptr}</span>}
            {detail.as_number ? (
              <Link to={`/as/${detail.as_number}${filterSearch}`} className="hover:text-primary transition-colors">
                AS{detail.as_number}{detail.as_name ? ` ${detail.as_name}` : ""}
              </Link>
            ) : null}
            {detail.prefix && <span className="font-ip">{detail.prefix}</span>}
          </div>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          {/* Quick period buttons — change current period filter without losing context */}
          {features.flow_search && (
            <Link
              to={`/flows?period=${filters.period || "1h"}&dst_ip=${encodeURIComponent(detail.ip)}`}
              className="inline-flex items-center gap-1.5 px-2.5 py-1 text-[13px] font-medium rounded border border-input bg-background hover:bg-accent transition-colors shrink-0"
              title="Search flows involving this IP"
            >
              <Search className="size-3" />
              View flows
            </Link>
          )}
        </div>
      </div>

      <HostAlerts ip={detail.ip} enabled={features.alerts} />

      <Card>
        <CardHeader>
          <CardTitle>Traffic</CardTitle>
        </CardHeader>
        <CardContent>
          {detail.time_series?.length > 0 ? (
            <ExpandableChart title="IP Traffic">
              <TrafficChart data={detail.time_series} height={350} timeBounds={timeBounds} stepSeconds={stepSecondsOf(data?.meta, periodSeconds)} />
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
            <CardHeader>
              <CardTitle>Top communicating IPs</CardTitle>
            </CardHeader>
            <CardContent>
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-border">
                    <th className="pb-1.5 text-left font-medium text-muted-foreground">IP</th>
                    <th className="pb-1.5 text-right font-medium text-muted-foreground">Traffic</th>
                    <th className="pb-1.5 text-right font-medium text-muted-foreground hidden sm:table-cell">Flows</th>
                  </tr>
                </thead>
                <tbody>
                  {detail.peer_ips.map(peer => (
                    <tr key={peer.ip} className="border-b border-border-soft last:border-0 hover:bg-accent/60">
                      <td className="py-1 max-w-[280px]">
                        <Link to={`/ip/${peer.ip}${filterSearch}`} className="text-primary hover:underline font-ip text-[13px] block truncate">
                          <IPWithPTR ip={peer.ip} />
                        </Link>
                      </td>
                      <td className="py-1 text-right">{formatTraffic(peer.bytes, periodSeconds, peer.packets)}</td>
                      <td className="py-1 text-right text-muted-foreground hidden sm:table-cell">{formatNumber(peer.flows)}</td>
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
            <CardHeader>
              <CardTitle>Traffic by AS</CardTitle>
            </CardHeader>
            <CardContent>
              <table className="w-full text-sm">
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
                    <tr key={as.as_number} className="border-b border-border-soft last:border-0 hover:bg-accent/60">
                      <td className="py-1">
                        <ASRef
                          asn={as.as_number}
                          search={filterSearch}
                          prefix={false}
                          className="text-primary hover:underline"
                        />
                      </td>
                      <td className="py-1 text-muted-foreground truncate max-w-40">{asDisplayName(as.as_number, as.as_name)}</td>
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
    </div>
  )
}
