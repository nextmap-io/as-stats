import { Link } from "react-router-dom"
import { useTopIP } from "@/hooks/useApi"
import { useFilters } from "@/hooks/useFilters"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { formatNumber } from "@/lib/utils"
import { useUnit } from "@/hooks/useUnit"

export function TopIP() {
  const { filters, setFilter, periodSeconds, filterSearch } = useFilters()
  const { formatTraffic } = useUnit()
  const { data, isLoading, error } = useTopIP({ ...filters, limit: 50 })

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold tracking-tight">Top IP</h1>

      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base">IP addresses by traffic volume</CardTitle>
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
                    <th className="pb-2 text-left font-medium text-muted-foreground">IP Address</th>
                    <th className="pb-2 text-left font-medium text-muted-foreground">AS</th>
                    <th className="pb-2 text-right font-medium text-muted-foreground">Traffic</th>
                    <th className="pb-2 text-right font-medium text-muted-foreground">Packets</th>
                    <th className="pb-2 text-right font-medium text-muted-foreground">Flows</th>
                  </tr>
                </thead>
                <tbody>
                  {data.data.map((ip, i) => (
                    <tr key={ip.ip} className="border-b border-border/50 last:border-0 hover:bg-muted/50">
                      <td className="py-2 text-muted-foreground">{(filters.offset || 0) + i + 1}</td>
                      <td className="py-2">
                        <Link to={`/ip/${ip.ip}${filterSearch}`} className="text-primary hover:underline font-mono text-xs">
                          {ip.ip}
                        </Link>
                      </td>
                      <td className="py-2">
                        {ip.as_number > 0 ? (
                          <Link to={`/as/${ip.as_number}${filterSearch}`} className="hover:underline">
                            <span className="font-mono text-xs">AS{ip.as_number}</span>
                            {ip.as_name && <span className="ml-1.5 text-muted-foreground">{ip.as_name}</span>}
                          </Link>
                        ) : "-"}
                      </td>
                      <td className="py-2 text-right font-mono">{formatTraffic(ip.bytes, periodSeconds)}</td>
                      <td className="py-2 text-right font-mono text-muted-foreground">{formatNumber(ip.packets)}</td>
                      <td className="py-2 text-right font-mono text-muted-foreground">{formatNumber(ip.flows)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>

              <div className="flex items-center justify-between mt-4 pt-4 border-t border-border">
                <button
                  disabled={!filters.offset || filters.offset === 0}
                  onClick={() => setFilter("offset", String(Math.max(0, (filters.offset || 0) - 50)))}
                  className="px-3 py-1.5 text-sm border border-input rounded-md hover:bg-accent disabled:opacity-50"
                >
                  Previous
                </button>
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
