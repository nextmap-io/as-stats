import { Link } from "react-router-dom"
import { useOverview, useLinksTraffic, useTopASTraffic, useLinkColors } from "@/hooks/useApi"
import { useFilters } from "@/hooks/useFilters"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { ErrorDisplay, EmptyState } from "@/components/ui/error"
import { PageSkeleton } from "@/components/ui/skeleton"
import { LinkTrafficChart } from "@/components/charts/LinkTrafficChart"
import { ExpandableChart } from "@/components/ExpandableChart"
import { formatNumber } from "@/lib/utils"
import { useUnit } from "@/hooks/useUnit"
import { useState } from "react"
import { BarChart3 } from "lucide-react"
import type { LinkTraffic, ASTrafficDetail } from "@/lib/types"

const DEFAULT_LINK_COLORS = ["#e74c3c", "#3498db", "#2ecc71", "#f39c12", "#9b59b6", "#1abc9c", "#e67e22", "#2980b9"]

export function Dashboard() {
  const { filters, filterSearch, periodSeconds, bucketSeconds, timeBounds } = useFilters()
  const { data, isLoading, error, refetch } = useOverview(filters)
  const { data: ipv4Traffic } = useLinksTraffic(4, filters)
  const { data: ipv6Traffic } = useLinksTraffic(6, filters)
  const { data: topASv4 } = useTopASTraffic(4, filters)
  const { data: topASv6 } = useTopASTraffic(6, filters)
  const { formatTraffic } = useUnit()
  const linkColors = useLinkColors()
  const [showAll, setShowAll] = useState(false)

  if (isLoading) return <PageSkeleton />
  if (error) return <ErrorDisplay error={error} onRetry={() => refetch()} />

  const overview = data?.data
  if (!overview) return null

  // Merge top AS from IPv4 and IPv6 into one ordered list (by combined bytes)
  const asMap = new Map<number, { v4?: ASTrafficDetail; v6?: ASTrafficDetail; total: number; name: string }>()
  for (const as of topASv4?.data || []) {
    const entry = asMap.get(as.as_number) || { total: 0, name: as.as_name }
    entry.v4 = as
    entry.total += as.bytes
    entry.name = as.as_name || entry.name
    asMap.set(as.as_number, entry)
  }
  for (const as of topASv6?.data || []) {
    const entry = asMap.get(as.as_number) || { total: 0, name: as.as_name }
    entry.v6 = as
    entry.total += as.bytes
    entry.name = as.as_name || entry.name
    asMap.set(as.as_number, entry)
  }
  const allAS = Array.from(asMap.entries())
    .sort(([, a], [, b]) => b.total - a.total)
    .slice(0, 50)

  const topASList = showAll ? allAS : allAS.slice(0, 10)

  // Extract unique link tags from all series for the sidebar legend
  const allLinkTags = Array.from(new Set([
    ...(ipv4Traffic?.data || []).map(s => s.link_tag),
    ...(ipv6Traffic?.data || []).map(s => s.link_tag),
  ]))

  return (
    <div className="space-y-4 animate-fade-in">
      {/* Stat bar */}
      <div className="flex items-center gap-4 text-xs flex-wrap">
        <span className="font-semibold text-sm tracking-tight mr-auto">Dashboard</span>
        <StatPill label="In" value={formatTraffic(overview.total_bytes_in, periodSeconds)} accent="in" />
        <StatPill label="Out" value={formatTraffic(overview.total_bytes_out, periodSeconds)} accent="out" />
        <StatPill label="ASes" value={formatNumber(overview.active_as_count)} />
        <StatPill label="Flows" value={formatNumber(overview.total_flows)} />
      </div>

      {/* Global traffic charts: IPv4 / IPv6 by link */}
      {((ipv4Traffic?.data && ipv4Traffic.data.length > 0) || (ipv6Traffic?.data && ipv6Traffic.data.length > 0)) && (
        <div className="grid gap-4 lg:grid-cols-2">
          <Card className="overflow-visible">
            <CardHeader className="pb-2">
              <CardTitle>IPv4 Traffic by Link</CardTitle>
            </CardHeader>
            <CardContent>
              {ipv4Traffic?.data && ipv4Traffic.data.length > 0 ? (
                <ExpandableChart title="IPv4 Traffic by Link" fetchType="link-traffic" fetchParams={{ ip_version: 4 }} linkColors={linkColors} currentPeriod={filters.period}>
                  <LinkTrafficChart series={ipv4Traffic.data} linkColors={linkColors} timeBounds={timeBounds} />
                </ExpandableChart>
              ) : (
                <EmptyState message="No IPv4 link traffic" icon={<BarChart3 className="h-6 w-6" />} />
              )}
            </CardContent>
          </Card>
          <Card className="overflow-visible">
            <CardHeader className="pb-2">
              <CardTitle>IPv6 Traffic by Link</CardTitle>
            </CardHeader>
            <CardContent>
              {ipv6Traffic?.data && ipv6Traffic.data.length > 0 ? (
                <ExpandableChart title="IPv6 Traffic by Link" fetchType="link-traffic" fetchParams={{ ip_version: 6 }} linkColors={linkColors} currentPeriod={filters.period}>
                  <LinkTrafficChart series={ipv6Traffic.data} linkColors={linkColors} timeBounds={timeBounds} />
                </ExpandableChart>
              ) : (
                <EmptyState message="No IPv6 link traffic" icon={<BarChart3 className="h-6 w-6" />} />
              )}
            </CardContent>
          </Card>
        </div>
      )}

      {/* Top AS with IPv4 + IPv6 graphs per AS */}
      {topASList.length > 0 && (
        <div className="flex gap-4">
          {/* Sticky legend sidebar */}
          <div className="hidden lg:block w-28 shrink-0">
            <div className="sticky top-14 space-y-1.5 pt-6">
              <h3 className="text-[9px] font-medium text-muted-foreground uppercase tracking-wider mb-2">Legend</h3>
              {allLinkTags.map((tag, i) => (
                <div key={tag} className="flex items-center gap-1.5 text-[9px] text-muted-foreground">
                  <span className="inline-block w-3 h-3 rounded-sm shrink-0" style={{ backgroundColor: linkColors[tag] || DEFAULT_LINK_COLORS[i % DEFAULT_LINK_COLORS.length] }} />
                  <span className="truncate">{tag}</span>
                </div>
              ))}
            </div>
          </div>

          {/* AS list */}
          <div className="flex-1 space-y-3">
          <h2 className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
            Top {topASList.length} AS — IPv4 + IPv6
          </h2>
          {topASList.map(([asn, entry]) => (
            <Card key={asn}>
              <CardHeader className="pb-1 pt-3 px-4">
                <div className="flex items-baseline justify-between gap-2">
                  <Link to={`/as/${asn}${filterSearch}`} className="text-xs font-semibold text-primary hover:underline">
                    AS{asn}
                  </Link>
                  <span className="text-[10px] text-muted-foreground truncate">{entry.name}</span>
                  <span className="text-[10px] text-muted-foreground ml-auto tabular-nums">
                    p95: {formatTraffic(
                      Math.max(entry.v4?.p95_in || 0, entry.v4?.p95_out || 0) + Math.max(entry.v6?.p95_in || 0, entry.v6?.p95_out || 0),
                      bucketSeconds
                    )}
                  </span>
                </div>
              </CardHeader>
              <CardContent className="px-4 pb-3">
                <div className="grid gap-4 lg:grid-cols-2">
                  <div>
                    {entry.v4 && entry.v4.series.length > 0 ? (
                      <ExpandableChart title={`AS${asn} — IPv4`} fetchType="as-detail-v4" fetchParams={{ asn }} linkColors={linkColors} currentPeriod={filters.period}>
                        <LinkTrafficChart
                          series={entry.v4.series}
                          title="IPv4"
                          height={140}
                          linkColors={linkColors}
                          hideLegend
                          timeBounds={timeBounds}
                          p95In={entry.v4.p95_in}
                          p95Out={entry.v4.p95_out}
                        />
                      </ExpandableChart>
                    ) : (
                      <div className="text-[10px] text-muted-foreground py-4 text-center">No IPv4 data</div>
                    )}
                  </div>
                  <div>
                    {entry.v6 && entry.v6.series.length > 0 ? (
                      <ExpandableChart title={`AS${asn} — IPv6`} fetchType="as-detail-v6" fetchParams={{ asn }} linkColors={linkColors} currentPeriod={filters.period}>
                        <LinkTrafficChart
                          series={entry.v6.series}
                          title="IPv6"
                          height={140}
                          linkColors={linkColors}
                          hideLegend
                          timeBounds={timeBounds}
                          p95In={entry.v6?.p95_in}
                          p95Out={entry.v6?.p95_out}
                        />
                      </ExpandableChart>
                    ) : (
                      <div className="text-[10px] text-muted-foreground py-4 text-center">No IPv6 data</div>
                    )}
                  </div>
                </div>
              </CardContent>
            </Card>
          ))}
          {!showAll && allAS.length > 10 && (
            <button
              onClick={() => setShowAll(true)}
              className="w-full py-2 text-xs text-primary hover:underline"
            >
              Show all {allAS.length} AS
            </button>
          )}
          </div>
        </div>
      )}

      {/* Links summary */}
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
                      <td className="py-1.5 text-right text-traffic-in">{formatTraffic(l.bytes_in, periodSeconds)}</td>
                      <td className="py-1.5 text-right text-traffic-out">{formatTraffic(l.bytes_out, periodSeconds)}</td>
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
