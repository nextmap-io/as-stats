import { Link, useParams } from "react-router-dom"
import { useASDetail, useASPeers, useASTopIPs } from "@/hooks/useApi"
import { useFilters } from "@/hooks/useFilters"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { TrafficChart } from "@/components/charts/TrafficChart"
import { formatBytes, formatNumber } from "@/lib/utils"

export function ASDetail() {
  const { asn } = useParams<{ asn: string }>()
  const asnNum = Number(asn) || 0
  const { filters } = useFilters()

  const { data, isLoading, error } = useASDetail(asnNum, filters)
  const { data: peersData } = useASPeers(asnNum, { ...filters, limit: 20 })
  const { data: topIPsData } = useASTopIPs(asnNum, { ...filters, limit: 20 })

  if (isLoading) return <p className="text-muted-foreground">Loading...</p>
  if (error) return <p className="text-destructive">{error.message}</p>

  const detail = data?.data
  if (!detail) return null

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">
          AS{detail.as_number}
          {detail.as_name && (
            <span className="ml-3 text-lg font-normal text-muted-foreground">{detail.as_name}</span>
          )}
        </h1>
      </div>

      {/* Traffic chart */}
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base">Traffic</CardTitle>
        </CardHeader>
        <CardContent>
          {detail.time_series?.length > 0 ? (
            <TrafficChart data={detail.time_series} height={350} />
          ) : (
            <p className="text-sm text-muted-foreground">No traffic data for this period</p>
          )}
        </CardContent>
      </Card>

      <div className="grid gap-6 lg:grid-cols-2">
        {/* Top internal IPs for this AS */}
        {topIPsData?.data && topIPsData.data.length > 0 && (
          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="text-base">Top Internal IPs</CardTitle>
            </CardHeader>
            <CardContent>
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-border">
                    <th className="pb-2 text-left font-medium text-muted-foreground">IP</th>
                    <th className="pb-2 text-right font-medium text-muted-foreground">Traffic</th>
                    <th className="pb-2 text-right font-medium text-muted-foreground">Flows</th>
                  </tr>
                </thead>
                <tbody>
                  {topIPsData.data.map(ip => (
                    <tr key={ip.ip} className="border-b border-border/50 last:border-0 hover:bg-muted/50">
                      <td className="py-1.5">
                        <Link to={`/ip/${ip.ip}`} className="text-primary hover:underline font-mono text-xs">
                          {ip.ip}
                        </Link>
                      </td>
                      <td className="py-1.5 text-right font-mono">{formatBytes(ip.bytes)}</td>
                      <td className="py-1.5 text-right font-mono text-muted-foreground">{formatNumber(ip.flows)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </CardContent>
          </Card>
        )}

        {/* Peers */}
        {peersData?.data && peersData.data.length > 0 && (
          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="text-base">Peers</CardTitle>
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
                  {peersData.data.map(peer => (
                    <tr key={peer.as_number} className="border-b border-border/50 last:border-0 hover:bg-muted/50">
                      <td className="py-1.5">
                        <Link to={`/as/${peer.as_number}`} className="text-primary hover:underline font-mono">
                          {peer.as_number}
                        </Link>
                      </td>
                      <td className="py-1.5 truncate max-w-48">{peer.as_name || "-"}</td>
                      <td className="py-1.5 text-right font-mono">{formatBytes(peer.bytes)}</td>
                      <td className="py-1.5 text-right font-mono text-muted-foreground">{formatNumber(peer.flows)}</td>
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
