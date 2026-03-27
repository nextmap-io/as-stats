import { Link } from "react-router-dom"
import { useTopAS } from "@/hooks/useApi"
import { useFilters } from "@/hooks/useFilters"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { formatNumber, formatPercent } from "@/lib/utils"
import { useUnit } from "@/hooks/useUnit"

export function TopAS() {
  const { filters, setFilter, periodSeconds, filterSearch } = useFilters()
  const { formatTraffic } = useUnit()
  const { data, isLoading, error } = useTopAS({ ...filters, limit: 50 })

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold tracking-tight">Top AS</h1>

      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base">
            Autonomous Systems by traffic volume
          </CardTitle>
        </CardHeader>
        <CardContent>
          {isLoading && <p className="text-muted-foreground">Loading...</p>}
          {error && <p className="text-destructive">{error.message}</p>}
          {data?.data && (
            <>
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-border">
                    <th className="pb-2 text-left font-medium text-muted-foreground">#</th>
                    <th className="pb-2 text-left font-medium text-muted-foreground">ASN</th>
                    <th className="pb-2 text-left font-medium text-muted-foreground">Name</th>
                    <th className="pb-2 text-right font-medium text-muted-foreground">Traffic</th>
                    <th className="pb-2 text-right font-medium text-muted-foreground">Packets</th>
                    <th className="pb-2 text-right font-medium text-muted-foreground">Flows</th>
                    <th className="pb-2 text-right font-medium text-muted-foreground">%</th>
                  </tr>
                </thead>
                <tbody>
                  {data.data.map((as, i) => (
                    <tr key={as.as_number} className="border-b border-border/50 last:border-0 hover:bg-muted/50">
                      <td className="py-2 text-muted-foreground">{(filters.offset || 0) + i + 1}</td>
                      <td className="py-2">
                        <Link to={`/as/${as.as_number}${filterSearch}`} className="text-primary hover:underline font-mono">
                          {as.as_number}
                        </Link>
                      </td>
                      <td className="py-2 truncate max-w-64">{as.as_name || "-"}</td>
                      <td className="py-2 text-right font-mono">{formatTraffic(as.bytes, periodSeconds)}</td>
                      <td className="py-2 text-right font-mono text-muted-foreground">{formatNumber(as.packets)}</td>
                      <td className="py-2 text-right font-mono text-muted-foreground">{formatNumber(as.flows)}</td>
                      <td className="py-2 text-right font-mono">
                        <div className="flex items-center justify-end gap-2">
                          <div className="w-16 bg-muted rounded-full h-1.5">
                            <div
                              className="bg-primary h-1.5 rounded-full"
                              style={{ width: `${Math.min(as.pct || 0, 100)}%` }}
                            />
                          </div>
                          <span className="w-12 text-right">{formatPercent(as.pct || 0)}</span>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>

              {/* Pagination */}
              <div className="flex items-center justify-between mt-4 pt-4 border-t border-border">
                <button
                  disabled={!filters.offset || filters.offset === 0}
                  onClick={() => setFilter("offset", String(Math.max(0, (filters.offset || 0) - 50)))}
                  className="px-3 py-1.5 text-sm border border-input rounded-md hover:bg-accent disabled:opacity-50"
                >
                  Previous
                </button>
                <span className="text-sm text-muted-foreground">
                  Showing {(filters.offset || 0) + 1} - {(filters.offset || 0) + data.data.length}
                </span>
                <button
                  disabled={data.data.length < 50}
                  onClick={() => setFilter("offset", String((filters.offset || 0) + 50))}
                  className="px-3 py-1.5 text-sm border border-input rounded-md hover:bg-accent disabled:opacity-50"
                >
                  Next
                </button>
              </div>
            </>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
