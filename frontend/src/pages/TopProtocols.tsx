import { useQuery } from "@tanstack/react-query"
import { Link, useSearchParams } from "react-router-dom"
import { api } from "@/lib/api"
import { useFilters } from "@/hooks/useFilters"
import { useUnit } from "@/hooks/useUnit"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { ErrorDisplay, EmptyState } from "@/components/ui/error"
import { TableSkeleton } from "@/components/ui/skeleton"
import { ExportButton, type ExportColumn } from "@/components/ExportButton"
import { ViewToggle } from "@/components/ViewToggle"
import { asView } from "@/lib/view"
import { TopNViz, type VizDatum } from "@/components/charts/TopNViz"
import { BarChart3 } from "lucide-react"
import type { ProtocolTraffic } from "@/lib/types"

export function TopProtocols() {
  const { filters, periodSeconds, filterSearch } = useFilters()
  const { formatTraffic } = useUnit()
  const [searchParams] = useSearchParams()
  const view = asView(searchParams.get("view"))
  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ["top-protocol", filters],
    queryFn: () => api.topProtocol(filters),
  })

  if (error) return <ErrorDisplay error={error as Error} onRetry={() => refetch()} />

  // Group by direction for separate in/out tables
  const inbound: ProtocolTraffic[] = []
  const outbound: ProtocolTraffic[] = []
  for (const p of data?.data || []) {
    if (p.direction === "in") inbound.push(p)
    else if (p.direction === "out") outbound.push(p)
  }
  inbound.sort((a, b) => b.bytes - a.bytes)
  outbound.sort((a, b) => b.bytes - a.bytes)

  const exportColumns: ExportColumn<ProtocolTraffic>[] = [
    { key: "protocol", header: "Protocol", value: (p) => p.protocol_name || String(p.protocol) },
    { key: "direction", header: "Direction", value: (p) => p.direction },
    { key: "bytes", header: "Bytes", value: (p) => p.bytes },
    { key: "packets", header: "Packets", value: (p) => p.packets },
    { key: "flows", header: "Flows", value: (p) => p.flows },
    { key: "pct", header: "Percent", value: (p) => p.pct },
  ]

  return (
    <div className="space-y-4 animate-fade-in">
      <div className="flex items-baseline justify-between">
        <h1 className="text-lg font-semibold tracking-tight">Top Protocols</h1>
        <div className="flex items-center gap-3">
          <span className="text-[10px] text-muted-foreground uppercase tracking-widest">
            5-min aggregation
          </span>
          <ViewToggle />
          <ExportButton rows={[...inbound, ...outbound]} columns={exportColumns} filename="top-protocols" />
        </div>
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        {view !== "table" ? (
          <>
            <ProtocolViz
              title="Inbound"
              rows={inbound}
              view={view}
              accent="text-traffic-in"
              formatTraffic={formatTraffic}
              periodSeconds={periodSeconds}
              filterSearch={filterSearch}
              direction="in"
            />
            <ProtocolViz
              title="Outbound"
              rows={outbound}
              view={view}
              accent="text-traffic-out"
              formatTraffic={formatTraffic}
              periodSeconds={periodSeconds}
              filterSearch={filterSearch}
              direction="out"
            />
          </>
        ) : (
          <>
            <ProtocolTable
              title="Inbound"
              rows={inbound}
              loading={isLoading}
              accent="text-traffic-in"
              formatTraffic={formatTraffic}
              periodSeconds={periodSeconds}
              filterSearch={filterSearch}
              direction="in"
            />
            <ProtocolTable
              title="Outbound"
              rows={outbound}
              loading={isLoading}
              accent="text-traffic-out"
              formatTraffic={formatTraffic}
              periodSeconds={periodSeconds}
              filterSearch={filterSearch}
              direction="out"
            />
          </>
        )}
      </div>
    </div>
  )
}

interface ProtocolVizProps {
  title: string
  rows: ProtocolTraffic[]
  view: "treemap" | "donut"
  accent: string
  formatTraffic: (bytes: number, periodSeconds: number) => string
  periodSeconds: number
  filterSearch: string
  direction: "in" | "out"
}

function ProtocolViz({ title, rows, view, accent, formatTraffic, periodSeconds, filterSearch, direction }: ProtocolVizProps) {
  const sep = filterSearch ? "&" : "?"
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className={accent}>{title}</CardTitle>
      </CardHeader>
      <CardContent>
        {rows.length === 0 ? (
          <EmptyState message="No data" icon={<BarChart3 className="size-8" />} />
        ) : (
          <TopNViz
            view={view}
            height={320}
            data={rows.map<VizDatum>((p) => ({
              name: p.protocol_name || `proto:${p.protocol}`,
              value: p.bytes,
              path: `/top/port${filterSearch}${sep}protocol=${p.protocol}&direction=${direction}`,
            }))}
            formatValue={(v) => formatTraffic(v, periodSeconds)}
            label={`${title} protocol composition`}
          />
        )}
      </CardContent>
    </Card>
  )
}

interface ProtocolTableProps {
  title: string
  rows: ProtocolTraffic[]
  loading: boolean
  accent: string
  formatTraffic: (bytes: number, periodSeconds: number) => string
  periodSeconds: number
  filterSearch: string
  direction: "in" | "out"
}

function ProtocolTable({ title, rows, loading, accent, formatTraffic, periodSeconds, filterSearch, direction }: ProtocolTableProps) {
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className={accent}>{title}</CardTitle>
      </CardHeader>
      <CardContent>
        {loading ? (
          <TableSkeleton rows={6} cols={4} />
        ) : rows.length === 0 ? (
          <EmptyState message="No data" icon={<BarChart3 className="size-8" />} />
        ) : (
          <table className="w-full text-xs">
            <thead>
              <tr className="border-b border-border">
                <th scope="col" className="pb-1.5 text-left font-medium text-muted-foreground">Protocol</th>
                <th scope="col" className="pb-1.5 text-right font-medium text-muted-foreground">Traffic</th>
                <th scope="col" className="pb-1.5 text-right font-medium text-muted-foreground hidden sm:table-cell">Flows</th>
                <th scope="col" className="pb-1.5 text-right font-medium text-muted-foreground">%</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((p) => (
                <tr key={`${p.protocol}-${p.direction}`} className="border-b border-border/40 last:border-0 hover:bg-accent/50 transition-colors">
                  <td className="py-1.5 font-mono">
                    <Link
                      to={`/top/port${filterSearch}${filterSearch ? "&" : "?"}protocol=${p.protocol}&direction=${direction}`}
                      className="text-primary hover:underline"
                    >
                      {p.protocol_name || `proto:${p.protocol}`}
                    </Link>
                  </td>
                  <td className="py-1.5 text-right font-mono">{formatTraffic(p.bytes, periodSeconds)}</td>
                  <td className="py-1.5 text-right font-mono text-muted-foreground hidden sm:table-cell">
                    {p.flows.toLocaleString()}
                  </td>
                  <td className="py-1.5 text-right">
                    <div className="flex items-center justify-end gap-1.5">
                      <div className="w-16 bg-muted rounded-full h-1">
                        <div
                          className={accent === "text-traffic-in" ? "bg-traffic-in h-1 rounded-full" : "bg-traffic-out h-1 rounded-full"}
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
  )
}
