import { useQuery } from "@tanstack/react-query"
import { Link, useSearchParams } from "react-router-dom"
import { api } from "@/lib/api"
import { useFilters } from "@/hooks/useFilters"
import { useUnit } from "@/hooks/useUnit"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { ErrorDisplay, EmptyState } from "@/components/ui/error"
import { TableSkeleton } from "@/components/ui/skeleton"
import { BarChart3 } from "lucide-react"
import { cn } from "@/lib/utils"
import type { PortTraffic } from "@/lib/types"

const PROTOCOL_OPTIONS = [
  { value: 0, label: "All" },
  { value: 6, label: "TCP" },
  { value: 17, label: "UDP" },
  { value: 1, label: "ICMP" },
]

export function TopPorts() {
  const { filters, periodSeconds, filterSearch } = useFilters()
  const { formatTraffic } = useUnit()
  const [searchParams, setSearchParams] = useSearchParams()

  const protocol = Number(searchParams.get("protocol") || "0")
  const direction = (searchParams.get("direction") as "in" | "out") || "in"

  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ["top-port", filters, protocol, direction],
    queryFn: () =>
      api.topPort({
        ...filters,
        direction,
        ...(protocol > 0 && { protocol }),
      }),
  })

  if (error) return <ErrorDisplay error={error as Error} onRetry={() => refetch()} />

  const rows: PortTraffic[] = (data?.data || []).filter((p) => p.direction === direction)

  const setProtocol = (p: number) => {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev)
      if (p > 0) next.set("protocol", String(p))
      else next.delete("protocol")
      return next
    })
  }

  const setDirection = (d: "in" | "out") => {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev)
      next.set("direction", d)
      return next
    })
  }

  return (
    <div className="space-y-4 animate-fade-in">
      <div className="flex items-baseline justify-between">
        <h1 className="text-lg font-semibold tracking-tight">Top Ports</h1>
        <span className="text-[10px] text-muted-foreground uppercase tracking-widest">
          5-min aggregation
        </span>
      </div>

      {/* Filter bar */}
      <div className="flex flex-wrap items-center gap-3">
        <div className="flex items-center gap-1.5">
          <span className="text-[10px] font-medium text-muted-foreground uppercase tracking-widest">Direction</span>
          <div className="flex gap-0.5">
            <button
              onClick={() => setDirection("in")}
              className={cn(
                "px-2 py-0.5 text-[11px] font-medium rounded transition-all",
                direction === "in" ? "bg-traffic-in text-background" : "text-muted-foreground hover:bg-accent"
              )}
            >
              Inbound
            </button>
            <button
              onClick={() => setDirection("out")}
              className={cn(
                "px-2 py-0.5 text-[11px] font-medium rounded transition-all",
                direction === "out" ? "bg-traffic-out text-background" : "text-muted-foreground hover:bg-accent"
              )}
            >
              Outbound
            </button>
          </div>
        </div>

        <div className="h-4 w-px bg-border" aria-hidden="true" />

        <div className="flex items-center gap-1.5">
          <span className="text-[10px] font-medium text-muted-foreground uppercase tracking-widest">Protocol</span>
          <div className="flex gap-0.5">
            {PROTOCOL_OPTIONS.map((p) => (
              <button
                key={p.value}
                onClick={() => setProtocol(p.value)}
                className={cn(
                  "px-2 py-0.5 text-[11px] font-medium rounded transition-all",
                  protocol === p.value ? "bg-primary text-primary-foreground" : "text-muted-foreground hover:bg-accent"
                )}
              >
                {p.label}
              </button>
            ))}
          </div>
        </div>
      </div>

      <Card>
        <CardHeader className="pb-2">
          <CardTitle>
            {direction === "in" ? "Destination ports (services accessed)" : "Source ports (services exposed)"}
          </CardTitle>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <TableSkeleton rows={10} cols={5} />
          ) : rows.length === 0 ? (
            <EmptyState message="No port data" icon={<BarChart3 className="h-8 w-8" />} />
          ) : (
            <table className="w-full text-xs" role="table">
              <thead>
                <tr className="border-b border-border">
                  <th scope="col" className="pb-1.5 text-left font-medium text-muted-foreground">#</th>
                  <th scope="col" className="pb-1.5 text-left font-medium text-muted-foreground">Port</th>
                  <th scope="col" className="pb-1.5 text-left font-medium text-muted-foreground">Service</th>
                  <th scope="col" className="pb-1.5 text-left font-medium text-muted-foreground">Proto</th>
                  <th scope="col" className="pb-1.5 text-right font-medium text-muted-foreground">Traffic</th>
                  <th scope="col" className="pb-1.5 text-right font-medium text-muted-foreground hidden sm:table-cell">Flows</th>
                  <th scope="col" className="pb-1.5 text-right font-medium text-muted-foreground">%</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((p, i) => (
                  <tr key={`${p.protocol}-${p.port}`} className="border-b border-border/40 last:border-0 hover:bg-accent/50 transition-colors">
                    <td className="py-1.5 text-muted-foreground tabular-nums">{i + 1}</td>
                    <td className="py-1.5 font-mono">
                      <Link
                        to={`/flows${filterSearch}${filterSearch ? "&" : "?"}protocol=${p.protocol}&${direction === "in" ? "dst_port" : "src_port"}=${p.port}`}
                        className="text-primary hover:underline"
                      >
                        {p.port}
                      </Link>
                    </td>
                    <td className="py-1.5 text-foreground/80">{p.service || "-"}</td>
                    <td className="py-1.5">
                      <span className="px-1.5 py-0.5 text-[9px] font-mono rounded bg-muted/50 text-foreground">
                        {p.protocol_name || `proto:${p.protocol}`}
                      </span>
                    </td>
                    <td className="py-1.5 text-right font-mono">{formatTraffic(p.bytes, periodSeconds)}</td>
                    <td className="py-1.5 text-right font-mono text-muted-foreground hidden sm:table-cell">
                      {p.flows.toLocaleString()}
                    </td>
                    <td className="py-1.5 text-right">
                      <div className="flex items-center justify-end gap-1.5">
                        <div className="w-16 bg-muted rounded-full h-1">
                          <div
                            className={direction === "in" ? "bg-traffic-in h-1 rounded-full" : "bg-traffic-out h-1 rounded-full"}
                            style={{ width: `${Math.min(p.pct || 0, 100)}%` }}
                          />
                        </div>
                        <span className="w-8 text-right text-muted-foreground tabular-nums">{(p.pct || 0).toFixed(1)}%</span>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
