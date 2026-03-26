import { Link } from "react-router-dom"
import { useLinksGrouped, useLinksTimeSeries } from "@/hooks/useApi"
import { useFilters } from "@/hooks/useFilters"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { StackedLinkChart } from "@/components/charts/StackedLinkChart"
import { ErrorDisplay } from "@/components/ui/error"
import { TableSkeleton } from "@/components/ui/skeleton"
import { formatBytes } from "@/lib/utils"
import type { LinkGroup } from "@/lib/types"

export function Links() {
  const { filters } = useFilters()
  const { data, isLoading, error, refetch } = useLinksGrouped(filters)
  const { data: tsData } = useLinksTimeSeries(filters)

  if (error) return <ErrorDisplay error={error} onRetry={() => refetch()} />

  const groups: LinkGroup[] = data?.data || []
  const allLinks = groups.flatMap(g => g.links)

  // Build series for stacked chart
  const series = allLinks
    .filter(l => tsData?.data?.[l.tag])
    .map(l => ({
      tag: l.tag,
      color: l.color || undefined,
      data: tsData!.data[l.tag],
    }))

  return (
    <div className="space-y-5 animate-fade-in">
      <div className="flex items-baseline justify-between">
        <h1 className="text-lg font-semibold tracking-tight">Links</h1>
        <span className="text-[10px] text-muted-foreground uppercase tracking-widest">
          {allLinks.length} link{allLinks.length !== 1 ? "s" : ""}
        </span>
      </div>

      {/* Stacked traffic chart — one color per link */}
      {series.length > 0 && (
        <div className="grid gap-4 lg:grid-cols-2">
          <Card>
            <CardHeader className="pb-2">
              <CardTitle>Inbound by link</CardTitle>
            </CardHeader>
            <CardContent>
              <StackedLinkChart series={series} direction="in" height={260} />
            </CardContent>
          </Card>
          <Card>
            <CardHeader className="pb-2">
              <CardTitle>Outbound by link</CardTitle>
            </CardHeader>
            <CardContent>
              <StackedLinkChart series={series} direction="out" height={260} />
            </CardContent>
          </Card>
        </div>
      )}

      {/* Grouped link tables */}
      {isLoading ? (
        <TableSkeleton rows={6} cols={5} />
      ) : groups.length === 0 ? (
        <Card>
          <CardContent className="p-6 text-center text-muted-foreground text-sm">
            No links configured. Add links via the admin API.
          </CardContent>
        </Card>
      ) : (
        groups.map(group => (
          <Card key={group.name}>
            <CardHeader className="pb-2">
              <CardTitle>{group.name}</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="overflow-x-auto -mx-4 px-4 sm:-mx-5 sm:px-5">
                <table className="w-full text-xs" role="table">
                  <thead>
                    <tr className="border-b border-border">
                      <th scope="col" className="pb-2 text-left font-medium text-muted-foreground">Link</th>
                      <th scope="col" className="pb-2 text-left font-medium text-muted-foreground hidden md:table-cell">Description</th>
                      <th scope="col" className="pb-2 text-right font-medium text-muted-foreground hidden sm:table-cell">Capacity</th>
                      <th scope="col" className="pb-2 text-right font-medium text-muted-foreground">
                        <span className="text-traffic-in">In</span>
                      </th>
                      <th scope="col" className="pb-2 text-right font-medium text-muted-foreground">
                        <span className="text-traffic-out">Out</span>
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {group.links.map(l => (
                      <tr key={l.tag} className="border-b border-border/40 last:border-0 hover:bg-accent/50 transition-colors">
                        <td className="py-1.5">
                          <div className="flex items-center gap-2">
                            {l.color && (
                              <span
                                className="inline-block w-2 h-2 rounded-full shrink-0"
                                style={{ backgroundColor: l.color }}
                                aria-hidden="true"
                              />
                            )}
                            <Link to={`/link/${l.tag}`} className="text-primary hover:underline font-medium">
                              {l.tag}
                            </Link>
                          </div>
                        </td>
                        <td className="py-1.5 text-muted-foreground truncate max-w-48 hidden md:table-cell" title={l.description}>
                          {l.description || "-"}
                        </td>
                        <td className="py-1.5 text-right text-muted-foreground hidden sm:table-cell">
                          {l.capacity_mbps ? `${l.capacity_mbps} Mbps` : "-"}
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
        ))
      )}
    </div>
  )
}
