import { Link, useParams } from "react-router-dom"
import { useASDetail, useASTopIPs, useASRemoteIPs, useLinkColors } from "@/hooks/useApi"
import { useFilters } from "@/hooks/useFilters"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { LinkTrafficChart } from "@/components/charts/LinkTrafficChart"
import { ExpandableChart } from "@/components/ExpandableChart"
import { formatNumber, formatBytes } from "@/lib/utils"
import { useUnit } from "@/hooks/useUnit"
import { ExternalLink } from "lucide-react"

export function ASDetail() {
  const { asn } = useParams<{ asn: string }>()
  const asnNum = Number(asn) || 0
  const { filters, filterSearch, periodSeconds, bucketSeconds} = useFilters()
  const { formatTraffic } = useUnit()
  const linkColors = useLinkColors()

  const { data, isLoading, error } = useASDetail(asnNum, filters)
  const { data: topIPsData } = useASTopIPs(asnNum, { ...filters, limit: 20 })
  const { data: remoteIPsData } = useASRemoteIPs(asnNum, { ...filters, limit: 20 })

  if (isLoading) return <p className="text-muted-foreground">Loading...</p>
  if (error) return <p className="text-destructive">{error.message}</p>

  const detail = data?.data
  if (!detail) return null

  return (
    <div className="space-y-5">
      {/* Header */}
      <div className="flex items-baseline justify-between flex-wrap gap-2">
        <h1 className="text-lg font-bold tracking-tight">
          AS{detail.as_number}
          {detail.as_name && (
            <span className="ml-2 text-sm font-normal text-muted-foreground">{detail.as_name}</span>
          )}
        </h1>
        <div className="flex items-center gap-3 text-[10px]">
          <a href={`https://bgp.he.net/AS${detail.as_number}`} target="_blank" rel="noopener noreferrer"
            className="text-muted-foreground hover:text-primary transition-colors inline-flex items-center gap-1">
            HE.net <ExternalLink className="h-2.5 w-2.5" />
          </a>
          <a href={`https://www.peeringdb.com/asn/${detail.as_number}`} target="_blank" rel="noopener noreferrer"
            className="text-muted-foreground hover:text-primary transition-colors inline-flex items-center gap-1">
            PeeringDB <ExternalLink className="h-2.5 w-2.5" />
          </a>
          <a href={`https://bgp.tools/as/${detail.as_number}`} target="_blank" rel="noopener noreferrer"
            className="text-muted-foreground hover:text-primary transition-colors inline-flex items-center gap-1">
            bgp.tools <ExternalLink className="h-2.5 w-2.5" />
          </a>
        </div>
      </div>

      {/* Volume + P95 summary */}
      <div className="flex flex-wrap gap-x-8 gap-y-1 text-xs">
        <div>
          <span className="text-muted-foreground">IPv4:</span>{" "}
          <span className="text-traffic-in">{formatBytes(detail.v4_bytes_in || 0)} in</span>
          {" / "}
          <span className="text-traffic-out">{formatBytes(detail.v4_bytes_out || 0)} out</span>
          {(detail.p95_v4_in || detail.p95_v4_out) ? (
            <span className="ml-2 text-muted-foreground">
              p95: <span className="text-traffic-in">{formatTraffic(detail.p95_v4_in || 0, bucketSeconds)}</span>
              {" / "}<span className="text-traffic-out">{formatTraffic(detail.p95_v4_out || 0, bucketSeconds)}</span>
            </span>
          ) : null}
        </div>
        <div>
          <span className="text-muted-foreground">IPv6:</span>{" "}
          <span className="text-traffic-in">{formatBytes(detail.v6_bytes_in || 0)} in</span>
          {" / "}
          <span className="text-traffic-out">{formatBytes(detail.v6_bytes_out || 0)} out</span>
          {(detail.p95_v6_in || detail.p95_v6_out) ? (
            <span className="ml-2 text-muted-foreground">
              p95: <span className="text-traffic-in">{formatTraffic(detail.p95_v6_in || 0, bucketSeconds)}</span>
              {" / "}<span className="text-traffic-out">{formatTraffic(detail.p95_v6_out || 0, bucketSeconds)}</span>
            </span>
          ) : null}
        </div>
      </div>

      {/* IPv4 + IPv6 traffic charts side by side, split by link */}
      <div className="grid gap-4 lg:grid-cols-2">
        <Card className="overflow-visible">
          <CardContent className="p-4">
            {detail.v4_series && detail.v4_series.length > 0 ? (
              <ExpandableChart title={`AS${detail.as_number} — IPv4`}>
                <LinkTrafficChart series={detail.v4_series} title="IPv4 Traffic by Link" height={280} linkColors={linkColors} p95In={detail.p95_v4_in} p95Out={detail.p95_v4_out} />
              </ExpandableChart>
            ) : (
              <p className="text-xs text-muted-foreground py-8 text-center">No IPv4 data</p>
            )}
          </CardContent>
        </Card>
        <Card className="overflow-visible">
          <CardContent className="p-4">
            {detail.v6_series && detail.v6_series.length > 0 ? (
              <ExpandableChart title={`AS${detail.as_number} — IPv6`}>
                <LinkTrafficChart series={detail.v6_series} title="IPv6 Traffic by Link" height={280} linkColors={linkColors} p95In={detail.p95_v6_in} p95Out={detail.p95_v6_out} />
              </ExpandableChart>
            ) : (
              <p className="text-xs text-muted-foreground py-8 text-center">No IPv6 data</p>
            )}
          </CardContent>
        </Card>
      </div>

      {/* Tables: Top IPs + Peers */}
      <div className="grid gap-5 lg:grid-cols-2">
        {topIPsData?.data && topIPsData.data.length > 0 && (
          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="text-sm">Top Internal IPs</CardTitle>
            </CardHeader>
            <CardContent>
              <table className="w-full text-xs">
                <thead>
                  <tr className="border-b border-border">
                    <th className="pb-1.5 text-left font-medium text-muted-foreground">IP</th>
                    <th className="pb-1.5 text-right font-medium text-muted-foreground">Traffic</th>
                    <th className="pb-1.5 text-right font-medium text-muted-foreground">Flows</th>
                  </tr>
                </thead>
                <tbody>
                  {topIPsData.data.map(ip => (
                    <tr key={ip.ip} className="border-b border-border/40 last:border-0 hover:bg-muted/50">
                      <td className="py-1">
                        <Link to={`/ip/${ip.ip}${filterSearch}`} className="text-primary hover:underline font-mono text-[11px]">
                          {ip.ip}
                        </Link>
                      </td>
                      <td className="py-1 text-right font-mono">{formatTraffic(ip.bytes, periodSeconds)}</td>
                      <td className="py-1 text-right font-mono text-muted-foreground">{formatNumber(ip.flows)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </CardContent>
          </Card>
        )}

        {remoteIPsData?.data && remoteIPsData.data.length > 0 && (
          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="text-sm">Top Remote IPs</CardTitle>
            </CardHeader>
            <CardContent>
              <table className="w-full text-xs">
                <thead>
                  <tr className="border-b border-border">
                    <th className="pb-1.5 text-left font-medium text-muted-foreground">IP</th>
                    <th className="pb-1.5 text-right font-medium text-muted-foreground">Traffic</th>
                    <th className="pb-1.5 text-right font-medium text-muted-foreground">Flows</th>
                  </tr>
                </thead>
                <tbody>
                  {remoteIPsData.data.map(ip => (
                    <tr key={ip.ip} className="border-b border-border/40 last:border-0 hover:bg-muted/50">
                      <td className="py-1">
                        <Link to={`/ip/${ip.ip}${filterSearch}`} className="text-primary hover:underline font-mono text-[11px]">
                          {ip.ip}
                        </Link>
                      </td>
                      <td className="py-1 text-right font-mono">{formatTraffic(ip.bytes, periodSeconds)}</td>
                      <td className="py-1 text-right font-mono text-muted-foreground">{formatNumber(ip.flows)}</td>
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
