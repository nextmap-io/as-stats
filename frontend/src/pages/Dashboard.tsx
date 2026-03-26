import { Link } from "react-router-dom"
import { useOverview } from "@/hooks/useApi"
import { useFilters } from "@/hooks/useFilters"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { ErrorDisplay, EmptyState } from "@/components/ui/error"
import { PageSkeleton } from "@/components/ui/skeleton"
import { formatBytes, formatNumber } from "@/lib/utils"
import { ArrowDownLeft, ArrowUpRight, Layers, Network, BarChart3 } from "lucide-react"
import type { ASTraffic, IPTraffic, LinkTraffic } from "@/lib/types"

export function Dashboard() {
  const { filters } = useFilters()
  const { data, isLoading, error, refetch } = useOverview(filters)

  if (isLoading) return <PageSkeleton />
  if (error) return <ErrorDisplay error={error} onRetry={() => refetch()} />

  const overview = data?.data
  if (!overview) return null

  return (
    <div className="space-y-5 animate-fade-in">
      <div className="flex items-baseline justify-between">
        <h1 className="text-lg font-semibold tracking-tight">Dashboard</h1>
        <span className="text-[10px] text-muted-foreground uppercase tracking-widest">Overview</span>
      </div>

      {/* Stat cards */}
      <div className="grid gap-3 grid-cols-2 lg:grid-cols-4">
        <StatCard
          title="Inbound"
          value={formatBytes(overview.total_bytes_in)}
          icon={<ArrowDownLeft className="h-4 w-4" />}
          accent="in"
          delay={0}
        />
        <StatCard
          title="Outbound"
          value={formatBytes(overview.total_bytes_out)}
          icon={<ArrowUpRight className="h-4 w-4" />}
          accent="out"
          delay={1}
        />
        <StatCard
          title="Active ASes"
          value={formatNumber(overview.active_as_count)}
          icon={<Network className="h-4 w-4" />}
          delay={2}
        />
        <StatCard
          title="Total Flows"
          value={formatNumber(overview.total_flows)}
          icon={<Layers className="h-4 w-4" />}
          delay={3}
        />
      </div>

      {/* Tables grid */}
      <div className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader className="pb-2">
            <div className="flex items-center justify-between">
              <CardTitle>Top AS</CardTitle>
              <Link to="/top/as" className="text-[10px] text-primary hover:underline uppercase tracking-wider">
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
                          <Link to={`/as/${as.as_number}`} className="text-primary hover:underline">
                            {as.as_number}
                          </Link>
                        </td>
                        <td className="py-1.5 text-muted-foreground truncate max-w-36" title={as.as_name}>
                          {as.as_name || "-"}
                        </td>
                        <td className="py-1.5 text-right">{formatBytes(as.bytes)}</td>
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
              <Link to="/top/ip" className="text-[10px] text-primary hover:underline uppercase tracking-wider">
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
                          <Link to={`/ip/${ip.ip}`} className="text-primary hover:underline text-[11px]">
                            {ip.ip}
                          </Link>
                        </td>
                        <td className="py-1.5 text-muted-foreground hidden sm:table-cell">
                          {ip.as_number > 0 ? (
                            <Link to={`/as/${ip.as_number}`} className="hover:text-foreground transition-colors">
                              AS{ip.as_number}
                            </Link>
                          ) : "-"}
                        </td>
                        <td className="py-1.5 text-right">{formatBytes(ip.bytes)}</td>
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
              <Link to="/links" className="text-[10px] text-primary hover:underline uppercase tracking-wider">
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
                        <Link to={`/link/${l.tag}`} className="text-primary hover:underline font-medium">
                          {l.tag}
                        </Link>
                      </td>
                      <td className="py-1.5 text-muted-foreground truncate max-w-48 hidden md:table-cell" title={l.description}>
                        {l.description || "-"}
                      </td>
                      <td className="py-1.5 text-right text-traffic-in">{formatBytes(l.bytes_in)}</td>
                      <td className="py-1.5 text-right text-traffic-out">{formatBytes(l.bytes_out)}</td>
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

function StatCard({ title, value, icon, accent, delay = 0 }: {
  title: string
  value: string
  icon: React.ReactNode
  accent?: "in" | "out"
  delay?: number
}) {
  const accentClass = accent === "in"
    ? "text-traffic-in border-l-2 border-l-traffic-in"
    : accent === "out"
      ? "text-traffic-out border-l-2 border-l-traffic-out"
      : "text-muted-foreground"

  return (
    <Card
      className="animate-fade-in"
      style={{ animationDelay: `${delay * 60}ms` }}
    >
      <CardContent className="p-4">
        <div className="flex items-center justify-between mb-1">
          <p className="text-[10px] font-medium text-muted-foreground uppercase tracking-widest">{title}</p>
          <span className={accentClass}>{icon}</span>
        </div>
        <p className={`text-xl font-bold tabular-nums ${accent ? accentClass.split(" ")[0] : ""}`}>
          {value}
        </p>
      </CardContent>
    </Card>
  )
}
