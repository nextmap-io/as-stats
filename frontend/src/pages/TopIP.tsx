import { useMemo, useState } from "react"
import { Link } from "react-router-dom"
import { useTopIP } from "@/hooks/useApi"
import { useFilters } from "@/hooks/useFilters"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { QueryBoundary } from "@/components/QueryBoundary"
import { DataTable, type Column } from "@/components/DataTable"
import { ExportButton, type ExportColumn } from "@/components/ExportButton"
import { formatNumber } from "@/lib/utils"
import { useUnit } from "@/hooks/useUnit"
import { IPWithPTR } from "@/components/PTR"
import type { IPTraffic } from "@/lib/types"

type Scope = "all" | "internal" | "external"

export function TopIP() {
  const { filters, setFilter, periodSeconds, filterSearch } = useFilters()
  const { formatTraffic } = useUnit()
  const [scope, setScope] = useState<Scope>("all")

  const query = useTopIP({ ...filters, limit: 50, scope: scope === "all" ? undefined : scope })
  const rows = query.data?.data ?? []
  const offset = filters.offset || 0

  const columns = useMemo<Column<IPTraffic>[]>(() => [
    {
      key: "rank",
      header: "#",
      className: "text-muted-foreground w-8",
      render: (_row, i) => offset + i + 1,
    },
    {
      key: "ip",
      header: "IP Address",
      sortable: true,
      className: "max-w-[280px]",
      render: (ip) => (
        <Link to={`/ip/${ip.ip}${filterSearch}`} className="text-primary hover:underline font-mono text-[11px] block truncate">
          <IPWithPTR ip={ip.ip} />
        </Link>
      ),
    },
    {
      key: "as_number",
      header: "AS",
      sortable: true,
      render: (ip) =>
        ip.as_number > 0 ? (
          <Link to={`/as/${ip.as_number}${filterSearch}`} className="hover:underline">
            <span className="font-mono">AS{ip.as_number}</span>
            {ip.as_name && <span className="ml-1 text-muted-foreground">{ip.as_name}</span>}
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
      render: (ip) => formatTraffic(ip.bytes, periodSeconds),
    },
    {
      key: "packets",
      header: "Packets",
      align: "right",
      numeric: true,
      sortable: true,
      hideOnMobile: true,
      className: "text-muted-foreground",
      render: (ip) => formatNumber(ip.packets),
    },
    {
      key: "flows",
      header: "Flows",
      align: "right",
      numeric: true,
      sortable: true,
      hideOnMobile: true,
      className: "text-muted-foreground",
      render: (ip) => formatNumber(ip.flows),
    },
  ], [offset, filterSearch, formatTraffic, periodSeconds])

  const exportColumns: ExportColumn<IPTraffic>[] = [
    { key: "ip", header: "IP", value: (r) => r.ip },
    { key: "as_number", header: "AS", value: (r) => r.as_number },
    { key: "as_name", header: "AS Name", value: (r) => r.as_name },
    { key: "bytes", header: "Bytes", value: (r) => r.bytes },
    { key: "packets", header: "Packets", value: (r) => r.packets },
    { key: "flows", header: "Flows", value: (r) => r.flows },
  ]

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
          <div className="flex items-center justify-between gap-2">
            <CardTitle className="text-sm">
              {scope === "internal" ? "Internal" : scope === "external" ? "External" : "All"} IPs by traffic volume
            </CardTitle>
            <ExportButton rows={rows} columns={exportColumns} filename="top-ip" />
          </div>
        </CardHeader>
        <CardContent>
          <QueryBoundary query={query} isEmpty={(d) => d.data.length === 0} loadingCols={6}>
            {(data) => (
              <>
                <DataTable rows={data.data} columns={columns} rowKey={(ip) => ip.ip} rowClassName="border-border/40" />

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
