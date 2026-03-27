import { Link } from "react-router-dom"
import { useOverview, useLinksTraffic, useASDetail } from "@/hooks/useApi"
import { useFilters } from "@/hooks/useFilters"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { ErrorDisplay, EmptyState } from "@/components/ui/error"
import { PageSkeleton } from "@/components/ui/skeleton"
import { TrafficChart } from "@/components/charts/TrafficChart"
import { LinkTrafficChart } from "@/components/charts/LinkTrafficChart"
import { formatNumber } from "@/lib/utils"
import { useUnit } from "@/hooks/useUnit"
import { ArrowDownLeft, ArrowUpRight, Layers, Network, BarChart3 } from "lucide-react"
import type { ASTraffic, IPTraffic, LinkTraffic } from "@/lib/types"

export function Dashboard() {
  const { filters, filterSearch } = useFilters()
  const { data, isLoading, error, refetch } = useOverview(filters)
  const { data: ipv4Traffic } = useLinksTraffic(4, filters)
  const { data: ipv6Traffic } = useLinksTraffic(6, filters)
  const { formatTraffic } = useUnit()

  if (isLoading) return <PageSkeleton />
  if (error) return <ErrorDisplay error={error} onRetry={() => refetch()} />

  const overview = data?.data
  if (!overview) return null

  // Top 4 ASes for mini charts
  const topASNs = (overview.top_as || []).slice(0, 4)

  return (
    <div className="space-y-4 animate-fade-in">
      {/* Stat bar — compact single row */}
      <div className="flex items-center gap-4 text-xs">
        <span className="font-semibold text-sm tracking-tight mr-auto">Dashboard</span>
        <StatPill label="In" value={formatTraffic(overview.total_bytes_in)} accent="in" />
        <StatPill label="Out" value={formatTraffic(overview.total_bytes_out)} accent="out" />
        <StatPill label="ASes" value={formatNumber(overview.active_as_count)} />
        <StatPill label="Flows" value={formatNumber(overview.total_flows)} />
      </div>

      {/* Traffic charts: IPv4 / IPv6 by link */}
      {((ipv4Traffic?.data && ipv4Traffic.data.length > 0) || (ipv6Traffic?.data && ipv6Traffic.data.length > 0)) && (
        <div className="grid gap-4 lg:grid-cols-2">
          <Card>
            <CardContent className="pt-5">
              {ipv4Traffic?.data && ipv4Traffic.data.length > 0 ? (
                <LinkTrafficChart series={ipv4Traffic.data} title="IPv4 Traffic by Link" />
              ) : (
                <EmptyState message="No IPv4 link traffic" icon={<BarChart3 className="h-8 w-8" />} />
              )}
            </CardContent>
          </Card>
          <Card>
            <CardContent className="pt-5">
              {ipv6Traffic?.data && ipv6Traffic.data.length > 0 ? (
                <LinkTrafficChart series={ipv6Traffic.data} title="IPv6 Traffic by Link" />
              ) : (
                <EmptyState message="No IPv6 link traffic" icon={<BarChart3 className="h-8 w-8" />} />
              )}
            </CardContent>
          </Card>
        </div>
      )}

      {/* Top AS traffic charts */}
      {topASNs.length > 0 && (
        <div className="grid gap-4 lg:grid-cols-2">
          {topASNs.map((as) => (
            <ASMiniChart key={as.as_number} asn={as.as_number} name={as.as_name} filterSearch={filterSearch} />
          ))}
        </div>
      )}

      {/* Tables grid */}
      <div className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader className="pb-2">
            <div className="flex items-center justify-between">
              <CardTitle>Top AS</CardTitle>
              <Link to={`/top/as${filterSearch}`} className="text-[10px] text-primary hover:underline uppercase tracking-wider">
                View all
              </Link>
            </div>
          </CardHeader>
          <CardContent>
            {overview.top_as?.length > 0 ? (
              <div className="overflow-x-auto -mx-6 px-6">
                <table className="w-full text-xs" role="table">
                  <thead>
                    <tr className="border-b border-border">
                      <th scope="col" className="pb-2 text-left font-medium text-muted-foreground">ASN</th>
                      <th scope="col" className="pb-2 text-left font-medium text-muted-foreground">Name</th>
                      <th scope="col" className="pb-2 text-right font-medium text-muted-foreground">Traffic</th>
                      <th scope="col" className="pb-2 text-right font-medium text-muted-foreground hidden sm:table-cell">%</th>
                    </tr>
                  </thead>
                  <tbody>
                    {overview.top_as.map((as: ASTraffic, i: number) => (
                      <tr
                        key={as.as_number}
                        className="border-b border-border/40 last:border-0 hover:bg-accent/50 transition-colors"
                        style={{ animationDelay: `${i * 30}ms` }}
                      >
                        <td className="py-1.5">
                          <Link to={`/as/${as.as_number}${filterSearch}`} className="text-primary hover:underline">
                            {as.as_number}
                          </Link>
                        </td>
                        <td className="py-1.5 text-muted-foreground truncate max-w-36" title={as.as_name}>
                          {as.as_name || "-"}
                        </td>
                        <td className="py-1.5 text-right">{formatTraffic(as.bytes)}</td>
                        <td className="py-1.5 text-right text-muted-foreground hidden sm:table-cell">
                          {as.pct?.toFixed(1)}%
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <EmptyState message="No AS traffic data" icon={<BarChart3 className="h-8 w-8" />} />
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <div className="flex items-center justify-between">
              <CardTitle>Top IP</CardTitle>
              <Link to={`/top/ip${filterSearch}`} className="text-[10px] text-primary hover:underline uppercase tracking-wider">
                View all
              </Link>
            </div>
          </CardHeader>
          <CardContent>
            {overview.top_ip?.length > 0 ? (
              <div className="overflow-x-auto -mx-6 px-6">
                <table className="w-full text-xs" role="table">
                  <thead>
                    <tr className="border-b border-border">
                      <th scope="col" className="pb-2 text-left font-medium text-muted-foreground">IP</th>
                      <th scope="col" className="pb-2 text-left font-medium text-muted-foreground hidden sm:table-cell">AS</th>
                      <th scope="col" className="pb-2 text-right font-medium text-muted-foreground">Traffic</th>
                    </tr>
                  </thead>
                  <tbody>
                    {overview.top_ip.map((ip: IPTraffic, i: number) => (
                      <tr
                        key={ip.ip}
                        className="border-b border-border/40 last:border-0 hover:bg-accent/50 transition-colors"
                        style={{ animationDelay: `${i * 30}ms` }}
                      >
                        <td className="py-1.5">
                          <Link to={`/ip/${ip.ip}${filterSearch}`} className="text-primary hover:underline text-[11px]">
                            {ip.ip}
                          </Link>
                        </td>
                        <td className="py-1.5 text-muted-foreground hidden sm:table-cell">
                          {ip.as_number > 0 ? (
                            <Link to={`/as/${ip.as_number}${filterSearch}`} className="hover:text-foreground transition-colors">
                              AS{ip.as_number}
                            </Link>
                          ) : "-"}
                        </td>
                        <td className="py-1.5 text-right">{formatTraffic(ip.bytes)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <EmptyState message="No IP traffic data" icon={<BarChart3 className="h-8 w-8" />} />
            )}
          </CardContent>
        </Card>
      </div>

      {/* Links */}
      {overview.links?.length > 0 && (
        <Card>
          <CardHeader className="pb-2">
            <div className="flex items-center justify-between">
              <CardTitle>Links</CardTitle>
              <Link to={`/links${filterSearch}`} className="text-[10px] text-primary hover:underline uppercase tracking-wider">
                View all
              </Link>
            </div>
          </CardHeader>
          <CardContent>
            <div className="overflow-x-auto -mx-6 px-6">
              <table className="w-full text-xs" role="table">
                <thead>
                  <tr className="border-b border-border">
                    <th scope="col" className="pb-2 text-left font-medium text-muted-foreground">Link</th>
                    <th scope="col" className="pb-2 text-left font-medium text-muted-foreground hidden md:table-cell">Description</th>
                    <th scope="col" className="pb-2 text-right font-medium text-muted-foreground">
                      <span className="text-traffic-in">In</span>
                    </th>
                    <th scope="col" className="pb-2 text-right font-medium text-muted-foreground">
                      <span className="text-traffic-out">Out</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {overview.links.map((l: LinkTraffic) => (
                    <tr key={l.tag} className="border-b border-border/40 last:border-0 hover:bg-accent/50 transition-colors">
                      <td className="py-1.5">
                        <Link to={`/link/${l.tag}${filterSearch}`} className="text-primary hover:underline font-medium">
                          {l.tag}
                        </Link>
                      </td>
                      <td className="py-1.5 text-muted-foreground truncate max-w-48 hidden md:table-cell" title={l.description}>
                        {l.description || "-"}
                      </td>
                      <td className="py-1.5 text-right text-traffic-in">{formatTraffic(l.bytes_in)}</td>
                      <td className="py-1.5 text-right text-traffic-out">{formatTraffic(l.bytes_out)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  )
}

/** Compact stat pill for the top bar */
function StatPill({ label, value, accent }: {
  label: string
  value: string
  accent?: "in" | "out"
}) {
  const color = accent === "in"
    ? "text-traffic-in"
    : accent === "out"
      ? "text-traffic-out"
      : "text-foreground"

  return (
    <div className="flex items-baseline gap-1.5">
      <span className="text-muted-foreground uppercase tracking-widest text-[9px]">{label}</span>
      <span className={`font-bold tabular-nums ${color}`}>{value}</span>
    </div>
  )
}

/** Mini traffic chart for a single AS on the dashboard */
function ASMiniChart({ asn, name, filterSearch }: { asn: number; name: string; filterSearch: string }) {
  const { filters } = useFilters()
  const { data } = useASDetail(asn, filters)

  const ts = data?.data?.time_series
  if (!ts || ts.length === 0) return null

  return (
    <Card>
      <CardHeader className="pb-1 pt-4 px-4">
        <div className="flex items-baseline justify-between">
          <Link to={`/as/${asn}${filterSearch}`} className="text-xs font-medium text-primary hover:underline">
            AS{asn}
          </Link>
          <span className="text-[10px] text-muted-foreground truncate ml-2 max-w-48">{name}</span>
        </div>
      </CardHeader>
      <CardContent className="px-4 pb-3">
        <TrafficChart data={ts} height={160} showLegend={false} />
      </CardContent>
    </Card>
  )
}
