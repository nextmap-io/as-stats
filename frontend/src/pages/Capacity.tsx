import { useMemo } from "react"
import { Link } from "react-router-dom"
import { useLinksCapacity } from "@/hooks/useApi"
import { useFilters } from "@/hooks/useFilters"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { QueryBoundary } from "@/components/QueryBoundary"
import { DataTable, PercentBar, type Column } from "@/components/DataTable"
import { ExportButton, type ExportColumn } from "@/components/ExportButton"
import { formatBitsPerSec, formatPercent } from "@/lib/utils"
import type { LinkCapacity } from "@/lib/types"

// utilBarClass maps a utilization percentage to a bar color: green < 70,
// amber >= 70, red >= 90.
function utilBarClass(pct: number): string {
  if (pct >= 90) return "bg-destructive"
  if (pct >= 70) return "bg-warning"
  return "bg-success"
}

// minForecastDays returns the soonest of the 80/95/100% saturation forecasts,
// or null when none are available (capacity unset or trend flat/declining).
function minForecastDays(lc: LinkCapacity): number | null {
  const vals = [lc.forecast_days_80, lc.forecast_days_95, lc.forecast_days_100].filter(
    (v): v is number => v != null,
  )
  return vals.length ? Math.min(...vals) : null
}

function projectedDate(days: number): string {
  const d = new Date(Date.now() + days * 86_400_000)
  return d.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" })
}

// SaturationCell renders "days to saturation" with the projected date, or an
// em dash when there is no upward forecast.
function SaturationCell({ lc }: { lc: LinkCapacity }) {
  const days = minForecastDays(lc)
  if (days == null) return <span className="text-muted-foreground">—</span>
  if (days <= 0) {
    return <span className="font-medium text-destructive">reached</span>
  }
  const rounded = Math.round(days)
  return (
    <span className="inline-flex flex-col items-end leading-tight">
      <span className={days <= 30 ? "font-medium text-destructive" : days <= 90 ? "font-medium text-warning" : "font-medium"}>
        {rounded.toLocaleString()}d
      </span>
      <span className="text-[10px] text-muted-foreground">{projectedDate(days)}</span>
    </span>
  )
}

export function Capacity() {
  const { filters, filterSearch } = useFilters()
  const capacityQuery = useLinksCapacity(filters)
  const rows = capacityQuery.data?.data ?? []

  const columns = useMemo<Column<LinkCapacity>[]>(
    () => [
      {
        key: "tag",
        header: "Link",
        sortable: true,
        render: (l) => (
          <Link to={`/link/${l.tag}${filterSearch}`} className="text-primary hover:underline font-medium">
            {l.tag}
          </Link>
        ),
      },
      {
        key: "description",
        header: "Description",
        sortable: true,
        hideOnMobile: true,
        className: "text-muted-foreground truncate max-w-48",
        render: (l) => l.description || "-",
      },
      {
        key: "capacity_mbps",
        header: "Capacity",
        align: "right",
        numeric: true,
        sortable: true,
        className: "text-muted-foreground",
        sortValue: (l) => l.capacity_mbps || 0,
        render: (l) => (l.capacity_mbps ? `${l.capacity_mbps.toLocaleString()} Mbps` : "-"),
      },
      {
        key: "current_bps",
        header: "Current",
        align: "right",
        numeric: true,
        sortable: true,
        render: (l) => formatBitsPerSec(l.current_bps),
      },
      {
        key: "p95_bps",
        header: "P95",
        align: "right",
        numeric: true,
        sortable: true,
        className: "font-medium",
        render: (l) => formatBitsPerSec(l.p95_bps),
      },
      {
        key: "utilization",
        header: "Utilization",
        align: "right",
        sortable: true,
        sortValue: (l) => (l.utilization_pct == null ? -1 : l.utilization_pct),
        render: (l) =>
          l.utilization_pct == null ? (
            <span className="text-muted-foreground">—</span>
          ) : (
            <PercentBar pct={l.utilization_pct} barClassName={utilBarClass(l.utilization_pct)} />
          ),
      },
      {
        key: "forecast",
        header: "Days to saturation",
        align: "right",
        sortable: true,
        // nulls sort as "least urgent" (farthest away)
        sortValue: (l) => minForecastDays(l) ?? Number.MAX_SAFE_INTEGER,
        render: (l) => <SaturationCell lc={l} />,
      },
    ],
    [filterSearch],
  )

  const exportColumns: ExportColumn<LinkCapacity>[] = [
    { key: "tag", header: "Link", value: (l) => l.tag },
    { key: "description", header: "Description", value: (l) => l.description },
    { key: "capacity_mbps", header: "Capacity (Mbps)", value: (l) => l.capacity_mbps || "" },
    { key: "current_bps", header: "Current (bps)", value: (l) => l.current_bps },
    { key: "p95_bps", header: "P95 (bps)", value: (l) => l.p95_bps },
    {
      key: "utilization_pct",
      header: "Utilization (%)",
      value: (l) => (l.utilization_pct == null ? "" : l.utilization_pct.toFixed(2)),
    },
    {
      key: "days_to_saturation",
      header: "Days to saturation",
      value: (l) => {
        const d = minForecastDays(l)
        return d == null ? "" : Math.round(d)
      },
    },
    {
      key: "projected_date",
      header: "Projected saturation date",
      value: (l) => {
        const d = minForecastDays(l)
        return d == null || d <= 0 ? "" : projectedDate(d)
      },
    },
  ]

  return (
    <div className="space-y-6">
      <div className="flex items-baseline justify-between">
        <h1 className="text-lg font-semibold tracking-tight">Capacity</h1>
      </div>

      <Card>
        <CardHeader className="pb-3">
          <div className="flex items-center justify-between gap-2">
            <CardTitle className="text-base">Link utilization &amp; saturation forecast</CardTitle>
            <ExportButton rows={rows} columns={exportColumns} filename="capacity" />
          </div>
        </CardHeader>
        <CardContent>
          <QueryBoundary query={capacityQuery} isEmpty={(d) => d.data.length === 0} loadingCols={7}>
            {(data) => (
              <DataTable
                rows={data.data}
                columns={columns}
                rowKey={(l) => l.tag}
                tableClassName="text-sm"
              />
            )}
          </QueryBoundary>
          <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1 text-[10px] text-muted-foreground">
            <span className="inline-flex items-center gap-1">
              <span className="inline-block size-2 rounded-sm bg-success" /> &lt; 70% (utilization based on window P95)
            </span>
            <span className="inline-flex items-center gap-1">
              <span className="inline-block size-2 rounded-sm bg-warning" /> {formatPercent(70)}–90%
            </span>
            <span className="inline-flex items-center gap-1">
              <span className="inline-block size-2 rounded-sm bg-destructive" /> &gt;= 90%
            </span>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
