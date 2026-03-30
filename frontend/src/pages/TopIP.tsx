import { useState } from "react"
import { Link } from "react-router-dom"
import { useTopIP } from "@/hooks/useApi"
import { useFilters } from "@/hooks/useFilters"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { formatNumber } from "@/lib/utils"
import { useUnit } from "@/hooks/useUnit"
import { IPWithPTR } from "@/components/PTR"

type Scope = "all" | "internal" | "external"

export function TopIP() {
  const { filters, setFilter, periodSeconds, filterSearch } = useFilters()
  const { formatTraffic } = useUnit()
  const [scope, setScope] = useState<Scope>("all")

  const { data, isLoading, error } = useTopIP({ ...filters, limit: 50, scope: scope === "all" ? undefined : scope })

  return (
    <div className="space-y-6">
      <div className="flex items-baseline justify-between">
        <h1 className="text-lg font-semibold tracking-tight">Top IP</h1>
        <div className="flex gap-1 text-xs">
          {(["all", "internal", "external"] as Scope[]).map(s => (
            <button
              key={s}
              onClick={() => { setScope(s); setFilter("offset", undefined) }}
              className={`px-2.5 py-1 rounded transition-colors ${scope === s ? "bg-primary text-primary-foreground" : "text-muted-foreground hover:bg-accent"}`}
            >
              {s === "all" ? "All" : s === "internal" ? "Internal" : "External"}
            </button>
          ))}
        </div>
      </div>

      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-sm">
            {scope === "internal" ? "Internal" : scope === "external" ? "External" : "All"} IPs by traffic volume
          </CardTitle>
        </CardHeader>
        <CardContent>
          {isLoading && <p className="text-muted-foreground text-sm">Loading...</p>}
          {error && <p className="text-destructive text-sm">{error.message}</p>}
          {data?.data && (
            <>
              <table className="w-full text-xs">
                <thead>
                  <tr className="border-b border-border">
                    <th className="pb-2 text-left font-medium text-muted-foreground w-8">#</th>
                    <th className="pb-2 text-left font-medium text-muted-foreground">IP Address</th>
                    <th className="pb-2 text-left font-medium text-muted-foreground">AS</th>
                    <th className="pb-2 text-right font-medium text-muted-foreground">Traffic</th>
                    <th className="pb-2 text-right font-medium text-muted-foreground hidden sm:table-cell">Packets</th>
                    <th className="pb-2 text-right font-medium text-muted-foreground hidden sm:table-cell">Flows</th>
                  </tr>
                </thead>
                <tbody>
                  {data.data.map((ip, i) => (
                    <tr key={ip.ip} className="border-b border-border/40 last:border-0 hover:bg-muted/50">
                      <td className="py-1.5 text-muted-foreground">{(filters.offset || 0) + i + 1}</td>
                      <td className="py-1.5">
                        <Link to={`/ip/${ip.ip}${filterSearch}`} className="text-primary hover:underline font-mono text-[11px]">
                          <IPWithPTR ip={ip.ip} />
                        </Link>
                      </td>
                      <td className="py-1.5">
                        {ip.as_number > 0 ? (
                          <Link to={`/as/${ip.as_number}${filterSearch}`} className="hover:underline">
                            <span className="font-mono">AS{ip.as_number}</span>
                            {ip.as_name && <span className="ml-1 text-muted-foreground">{ip.as_name}</span>}
                          </Link>
                        ) : "-"}
                      </td>
                      <td className="py-1.5 text-right font-mono">{formatTraffic(ip.bytes, periodSeconds)}</td>
                      <td className="py-1.5 text-right font-mono text-muted-foreground hidden sm:table-cell">{formatNumber(ip.packets)}</td>
                      <td className="py-1.5 text-right font-mono text-muted-foreground hidden sm:table-cell">{formatNumber(ip.flows)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>

              <div className="flex items-center justify-between mt-3 pt-3 border-t border-border">
                <button
                  disabled={!filters.offset || filters.offset === 0}
                  onClick={() => setFilter("offset", String(Math.max(0, (filters.offset || 0) - 50)))}
                  className="px-3 py-1 text-xs border border-input rounded hover:bg-accent disabled:opacity-50"
                >
                  Previous
                </button>
                <button
                  disabled={data.data.length < 50}
                  onClick={() => setFilter("offset", String((filters.offset || 0) + 50))}
                  className="px-3 py-1 text-xs border border-input rounded hover:bg-accent disabled:opacity-50"
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
