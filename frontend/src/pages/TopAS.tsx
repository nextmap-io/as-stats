import { useMemo } from "react"
import { Link } from "react-router-dom"
import { useTopAS } from "@/hooks/useApi"
import { useFilters } from "@/hooks/useFilters"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { QueryBoundary } from "@/components/QueryBoundary"
import { DataTable, PercentBar, type Column } from "@/components/DataTable"
import { ExportButton, type ExportColumn } from "@/components/ExportButton"
import { formatNumber } from "@/lib/utils"
import { useUnit } from "@/hooks/useUnit"
import { countryFlag, hasCountry } from "@/lib/countries"
import type { ASTraffic } from "@/lib/types"

export function TopAS() {
  const { filters, setFilter, periodSeconds, filterSearch } = useFilters()
  const { formatTraffic } = useUnit()
  const query = useTopAS({ ...filters, limit: 50 })
  const rows = query.data?.data ?? []

  const offset = filters.offset || 0

  const columns = useMemo<Column<ASTraffic>[]>(() => [
    {
      key: "rank",
      header: "#",
      className: "text-muted-foreground w-6",
      render: (_row, i) => offset + i + 1,
    },
    {
      key: "as_number",
      header: "ASN",
      sortable: true,
      render: (as) => (
        <Link to={`/as/${as.as_number}${filterSearch}`} className="text-primary hover:underline font-mono">
          {as.as_number}
        </Link>
      ),
    },
    {
      key: "as_name",
      header: "Name",
      sortable: true,
      className: "truncate max-w-64",
      render: (as) => (
        <span className="inline-flex items-center gap-1.5">
          {hasCountry(as.country) && (
            <span aria-hidden title={as.country} className="leading-none">
              {countryFlag(as.country)}
            </span>
          )}
          <span className="truncate">{as.as_name || "-"}</span>
        </span>
      ),
    },
    {
      key: "bytes",
      header: "Traffic",
      align: "right",
      numeric: true,
      sortable: true,
      render: (as) => formatTraffic(as.bytes, periodSeconds),
    },
    {
      key: "packets",
      header: "Packets",
      align: "right",
      numeric: true,
      sortable: true,
      hideOnMobile: true,
      className: "text-muted-foreground",
      render: (as) => formatNumber(as.packets),
    },
    {
      key: "flows",
      header: "Flows",
      align: "right",
      numeric: true,
      sortable: true,
      hideOnMobile: true,
      className: "text-muted-foreground",
      render: (as) => formatNumber(as.flows),
    },
    {
      key: "pct",
      header: "%",
      align: "right",
      sortable: true,
      render: (as) => <PercentBar pct={as.pct || 0} />,
    },
  ], [offset, filterSearch, formatTraffic, periodSeconds])

  const exportColumns: ExportColumn<ASTraffic>[] = [
    { key: "as_number", header: "ASN", value: (r) => r.as_number },
    { key: "as_name", header: "Name", value: (r) => r.as_name },
    { key: "country", header: "Country", value: (r) => r.country ?? "" },
    { key: "bytes", header: "Bytes", value: (r) => r.bytes },
    { key: "packets", header: "Packets", value: (r) => r.packets },
    { key: "flows", header: "Flows", value: (r) => r.flows },
    { key: "pct", header: "Percent", value: (r) => r.pct },
  ]

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold tracking-tight">Top AS</h1>

      <Card>
        <CardHeader className="pb-3">
          <div className="flex items-center justify-between gap-2">
            <CardTitle className="text-base">
              Autonomous Systems by traffic volume
            </CardTitle>
            <ExportButton rows={rows} columns={exportColumns} filename="top-as" />
          </div>
        </CardHeader>
        <CardContent>
          <QueryBoundary query={query} isEmpty={(d) => d.data.length === 0} loadingCols={7}>
            {(data) => (
              <>
                <DataTable rows={data.data} columns={columns} rowKey={(as) => as.as_number} />

                {/* Pagination */}
                <div className="flex items-center justify-between mt-4 pt-4 border-t border-border">
                  <button
                    disabled={!filters.offset || filters.offset === 0}
                    onClick={() => setFilter("offset", String(Math.max(0, offset - 50)))}
                    className="px-3 py-1.5 text-sm border border-input rounded-md hover:bg-accent disabled:opacity-50"
                  >
                    Previous
                  </button>
                  <span className="text-sm text-muted-foreground">
                    Showing {offset + 1} - {offset + data.data.length}
                  </span>
                  <button
                    disabled={data.data.length < 50}
                    onClick={() => setFilter("offset", String(offset + 50))}
                    className="px-3 py-1.5 text-sm border border-input rounded-md hover:bg-accent disabled:opacity-50"
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
