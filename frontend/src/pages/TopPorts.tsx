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
import type { PortTraffic } from "@/lib/types"
import { Segmented } from "@/components/ui/segmented"
import { PORT_STATS_HELP, flowSearchPath, portHistoryNote, portLabel } from "@/lib/ports"

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
  const view = asView(searchParams.get("view"))

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
  const historyNote = portHistoryNote(data?.meta?.port_history_since)

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

  const exportColumns: ExportColumn<PortTraffic>[] = [
    { key: "port", header: "Service port", value: (p) => p.port },
    { key: "service", header: "Service", value: (p) => p.service ?? "" },
    { key: "protocol", header: "Protocol", value: (p) => p.protocol_name || String(p.protocol) },
    { key: "direction", header: "Direction", value: (p) => p.direction },
    { key: "bytes", header: "Bytes", value: (p) => p.bytes },
    { key: "flows", header: "Flows", value: (p) => p.flows },
    { key: "pct", header: "Percent", value: (p) => p.pct },
  ]

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-2">
        <h1 className="text-2xl font-semibold">Top ports</h1>
        <span className="text-xs text-muted-foreground">
          5-min aggregation
        </span>
      </div>

      {/* Filter bar */}
      <div className="flex flex-wrap items-center gap-3">
        <div className="flex items-center gap-1.5">
          <span className="text-[13px] text-muted-foreground">Direction</span>
          <Segmented
            label="Direction"
            value={direction}
            onChange={setDirection}
            options={[
              { value: "in", label: "Inbound" },
              { value: "out", label: "Outbound" },
            ]}
          />
        </div>

        <div className="h-4 w-px bg-border" aria-hidden="true" />

        <div className="flex items-center gap-1.5">
          <span className="text-[13px] text-muted-foreground">Protocol</span>
          <Segmented
            label="Protocol"
            value={String(protocol)}
            onChange={(v) => setProtocol(Number(v))}
            options={PROTOCOL_OPTIONS.map((p) => ({ value: String(p.value), label: p.label }))}
          />
        </div>
      </div>

      <div className="space-y-1 text-[13px] text-muted-foreground">
        <p>{PORT_STATS_HELP}</p>
        {historyNote && <p>{historyNote}</p>}
      </div>

      <Card>
        <CardHeader>
          <div className="flex flex-wrap items-center justify-between gap-2">
            <CardTitle>
              {direction === "in" ? "Inbound traffic, by service port" : "Outbound traffic, by service port"}
            </CardTitle>
            <div className="flex flex-wrap items-center gap-2">
              <ViewToggle />
              <ExportButton rows={rows} columns={exportColumns} filename={`top-ports-${direction}`} />
            </div>
          </div>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <TableSkeleton rows={10} cols={5} />
          ) : rows.length === 0 ? (
            <EmptyState message="No port data" icon={<BarChart3 className="size-8" />} />
          ) : view !== "table" ? (
            <TopNViz
              view={view}
              data={rows.map<VizDatum>((p) => ({
                name: p.port > 0 ? `${p.port}${p.service ? ` ${p.service}` : ""}` : p.protocol_name || `proto:${p.protocol}`,
                value: p.bytes,
                path: flowSearchPath(filterSearch, p.protocol, p.port),
              }))}
              formatValue={(v) => formatTraffic(v, periodSeconds)}
              label={`${direction === "in" ? "Inbound" : "Outbound"} traffic by service port`}
            />
          ) : (
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border">
                  <th scope="col" className="pb-1.5 text-left font-medium text-muted-foreground">#</th>
                  <th scope="col" className="pb-1.5 text-left font-medium text-muted-foreground">Service port</th>
                  <th scope="col" className="pb-1.5 text-left font-medium text-muted-foreground">Service</th>
                  <th scope="col" className="pb-1.5 text-left font-medium text-muted-foreground">Proto</th>
                  <th scope="col" className="pb-1.5 text-right font-medium text-muted-foreground">Traffic</th>
                  <th scope="col" className="pb-1.5 text-right font-medium text-muted-foreground hidden sm:table-cell">Flows</th>
                  <th scope="col" className="pb-1.5 text-right font-medium text-muted-foreground">%</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((p, i) => (
                  <tr key={`${p.protocol}-${p.port}`} className="border-b border-border-soft last:border-0 hover:bg-accent/50 transition-colors">
                    <td className="py-1.5 text-muted-foreground tabular-nums">{i + 1}</td>
                    <td className="py-1.5">
                      <Link
                        to={flowSearchPath(filterSearch, p.protocol, p.port)}
                        className="text-primary hover:underline"
                        title="Flows with this port on either side"
                      >
                        {portLabel(p.port)}
                      </Link>
                    </td>
                    <td className="py-1.5 text-foreground/80">{p.service || "-"}</td>
                    <td className="py-1.5">
                      <span className="px-1.5 py-0.5 text-xs rounded bg-muted/50 text-foreground">
                        {p.protocol_name || `proto:${p.protocol}`}
                      </span>
                    </td>
                    <td className="py-1.5 text-right">{formatTraffic(p.bytes, periodSeconds, p.packets)}</td>
                    <td className="py-1.5 text-right text-muted-foreground hidden sm:table-cell">
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
