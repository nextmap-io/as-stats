import { useMemo, useState } from "react"
import { Link } from "react-router-dom"
import { useTopPrefix } from "@/hooks/useApi"
import { useFilters } from "@/hooks/useFilters"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { QueryBoundary } from "@/components/QueryBoundary"
import { DataTable, PercentBar, type Column } from "@/components/DataTable"
import { ExportButton, type ExportColumn } from "@/components/ExportButton"
import { MetricToggle } from "@/components/MetricToggle"
import { asMetric, metricValue } from "@/lib/metric"
import { formatNumber, formatBytes, cn } from "@/lib/utils"
import { useUnit } from "@/hooks/useUnit"
import type { PrefixTraffic } from "@/lib/types"

type Scope = "all" | "internal" | "external"

export function TopPrefixes() {
  const { filters, setFilter, periodSeconds, filterSearch } = useFilters()
  const { formatTraffic } = useUnit()
  const [scope, setScope] = useState<Scope>("all")

  const query = useTopPrefix({ ...filters, limit: 50, scope: scope === "all" ? undefined : scope })
  const rows = query.data?.data ?? []
  const offset = filters.offset || 0
  const metric = asMetric(filters.metric)
  const metricTotal = rows.reduce((s, r) => s + metricValue(r, metric), 0)

  const columns = useMemo<Column<PrefixTraffic>[]>(() => {
    const metricCell = (m: typeof metric) =>
      cn(metric === m ? "text-foreground font-medium" : "text-muted-foreground")
    const metricHeader = (m: typeof metric) => (metric === m ? "text-primary" : undefined)
    return [
      {
        key: "rank",
        header: "#",
        className: "text-muted-foreground w-8",
        render: (_row, i) => offset + i + 1,
      },
      {
        key: "prefix",
        header: "Prefix",
        sortable: true,
        className: "font-mono text-[11px]",
        render: (pfx) => pfx.prefix,
      },
      {
        key: "as_number",
        header: "AS",
        sortable: true,
        render: (pfx) =>
          pfx.as_number > 0 ? (
            <Link to={`/as/${pfx.as_number}${filterSearch}`} className="hover:underline">
              <span className="font-mono">AS{pfx.as_number}</span>
              {pfx.as_name && <span className="ml-1 text-muted-foreground">{pfx.as_name}</span>}
            </Link>
          ) : (
            "-"
          ),
      },
      {
        key: "bytes",
        header: "Traffic",
        align: "right",
        numeric: true,
        sortable: true,
        headerClassName: metricHeader("bytes"),
        render: (pfx) => formatTraffic(pfx.bytes, periodSeconds),
      },
      {
        key: "packets",
        header: "Packets",
        align: "right",
        numeric: true,
        sortable: true,
        hideOnMobile: true,
        className: metricCell("packets"),
        headerClassName: metricHeader("packets"),
        render: (pfx) => formatNumber(pfx.packets),
      },
      {
        key: "flows",
        header: "Flows",
        align: "right",
        numeric: true,
        sortable: true,
        hideOnMobile: true,
        className: metricCell("flows"),
        headerClassName: metricHeader("flows"),
        render: (pfx) => formatNumber(pfx.flows),
      },
      {
        key: "avg_pkt_size",
        header: "Avg Pkt",
        align: "right",
        numeric: true,
        sortable: true,
        hideOnMobile: true,
        className: "text-muted-foreground",
        render: (pfx) => formatBytes(pfx.avg_pkt_size || 0),
      },
      {
        key: "pct",
        header: "%",
        align: "right",
        sortable: true,
        hideOnMobile: true,
        sortValue: (pfx) => metricValue(pfx, metric),
        render: (pfx) => (
          <PercentBar pct={metricTotal > 0 ? (metricValue(pfx, metric) / metricTotal) * 100 : 0} />
        ),
      },
    ]
  }, [offset, filterSearch, formatTraffic, periodSeconds, metric, metricTotal])

  const exportColumns: ExportColumn<PrefixTraffic>[] = [
    { key: "prefix", header: "Prefix", value: (r) => r.prefix },
    { key: "as_number", header: "AS", value: (r) => r.as_number },
    { key: "as_name", header: "AS Name", value: (r) => r.as_name },
    { key: "bytes", header: "Bytes", value: (r) => r.bytes },
    { key: "packets", header: "Packets", value: (r) => r.packets },
    { key: "flows", header: "Flows", value: (r) => r.flows },
    { key: "avg_pkt_size", header: "Avg Pkt Size", value: (r) => Math.round(r.avg_pkt_size || 0) },
  ]

  return (
    <div className="space-y-6">
      <div className="flex items-baseline justify-between">
        <h1 className="text-lg font-semibold tracking-tight">Top Prefixes</h1>
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
          <div className="flex items-center justify-between gap-2">
            <CardTitle className="text-sm">
              {scope === "internal" ? "Internal" : scope === "external" ? "External" : "All"} prefixes by traffic volume
              {scope === "internal" && <span className="font-normal text-muted-foreground ml-2">(grouped by announced prefix)</span>}
            </CardTitle>
            <div className="flex items-center gap-2">
              <MetricToggle value={metric} onChange={(v) => setFilter("metric", v)} />
              <ExportButton rows={rows} columns={exportColumns} filename="top-prefixes" />
            </div>
          </div>
        </CardHeader>
        <CardContent>
          <QueryBoundary query={query} isEmpty={(d) => d.data.length === 0} loadingCols={7}>
            {(data) => (
              <>
                <DataTable
                  rows={data.data}
                  columns={columns}
                  rowKey={(pfx) => `${pfx.prefix}-${pfx.as_number}`}
                  rowClassName="border-border/40"
                />

                <div className="flex items-center justify-between mt-3 pt-3 border-t border-border">
                  <button
                    disabled={!filters.offset || filters.offset === 0}
                    onClick={() => setFilter("offset", String(Math.max(0, offset - 50)))}
                    className="px-3 py-1 text-xs border border-input rounded hover:bg-accent disabled:opacity-50"
                  >
                    Previous
                  </button>
                  <button
                    disabled={data.data.length < 50}
                    onClick={() => setFilter("offset", String(offset + 50))}
                    className="px-3 py-1 text-xs border border-input rounded hover:bg-accent disabled:opacity-50"
                  >
                    Next
                  </button>
                </div>
              </>
            )}
          </QueryBoundary>
        </CardContent>
      </Card>
    </div>
  )
}
