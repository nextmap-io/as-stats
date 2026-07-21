import { useMemo } from "react"
import {
  BarChart,
  Bar,
  XAxis,
  YAxis,
  Tooltip,
  ResponsiveContainer,
  Cell,
} from "recharts"
import { useTopCountry } from "@/hooks/useApi"
import { useFilters } from "@/hooks/useFilters"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { QueryBoundary } from "@/components/QueryBoundary"
import { DataTable, PercentBar, type Column } from "@/components/DataTable"
import { ExportButton, type ExportColumn } from "@/components/ExportButton"
import { formatNumber, formatBytes } from "@/lib/utils"
import { useUnit } from "@/hooks/useUnit"
import { useChartColors } from "@/hooks/useChartColors"
import { countryFlag, countryName } from "@/lib/countries"
import type { CountryTraffic } from "@/lib/types"

interface CountryBar {
  label: string
  bytes: number
}

function TopCountriesBar({ rows }: { rows: CountryTraffic[] }) {
  const colors = useChartColors()
  const data = useMemo<CountryBar[]>(
    () =>
      rows.slice(0, 10).map((c) => ({
        label: `${countryFlag(c.country)} ${c.country || "??"}`,
        bytes: c.bytes,
      })),
    [rows],
  )

  if (data.length === 0) return null

  return (
    <ResponsiveContainer width="100%" height={Math.max(160, data.length * 28)}>
      <BarChart data={data} layout="vertical" margin={{ top: 4, right: 16, bottom: 4, left: 8 }}>
        <XAxis
          type="number"
          tickFormatter={(v) => formatBytes(Number(v))}
          tick={{ fill: colors.text, fontSize: 10 }}
          stroke={colors.grid}
        />
        <YAxis
          type="category"
          dataKey="label"
          width={72}
          tick={{ fill: colors.text, fontSize: 11 }}
          stroke={colors.grid}
        />
        <Tooltip
          cursor={{ fill: colors.grid, opacity: 0.2 }}
          contentStyle={{
            backgroundColor: colors.tooltipBg,
            border: `1px solid ${colors.tooltipBorder}`,
            borderRadius: 6,
            fontSize: 12,
          }}
          labelStyle={{ color: colors.tooltipText }}
          itemStyle={{ color: colors.tooltipText }}
          formatter={(v) => [formatBytes(Number(v)), "Traffic"]}
        />
        <Bar dataKey="bytes" radius={[0, 3, 3, 0]}>
          {data.map((d) => (
            <Cell key={d.label} fill="var(--color-primary)" />
          ))}
        </Bar>
      </BarChart>
    </ResponsiveContainer>
  )
}

export function Countries() {
  const { filters, periodSeconds } = useFilters()
  const { formatTraffic } = useUnit()
  const query = useTopCountry({ ...filters, limit: 100 })
  const rows = query.data?.data ?? []

  const columns = useMemo<Column<CountryTraffic>[]>(
    () => [
      {
        key: "rank",
        header: "#",
        className: "text-muted-foreground w-6",
        render: (_row, i) => i + 1,
      },
      {
        key: "country",
        header: "Country",
        sortable: true,
        render: (c) => (
          <span className="inline-flex items-center gap-2">
            <span aria-hidden className="text-base leading-none">
              {countryFlag(c.country)}
            </span>
            <span>{countryName(c.country, c.name)}</span>
            <span className="text-muted-foreground font-mono text-[10px]">
              {c.country || "??"}
            </span>
          </span>
        ),
      },
      {
        key: "bytes",
        header: "Traffic",
        align: "right",
        numeric: true,
        sortable: true,
        render: (c) => formatTraffic(c.bytes, periodSeconds),
      },
      {
        key: "packets",
        header: "Packets",
        align: "right",
        numeric: true,
        sortable: true,
        hideOnMobile: true,
        className: "text-muted-foreground",
        render: (c) => formatNumber(c.packets),
      },
      {
        key: "flows",
        header: "Flows",
        align: "right",
        numeric: true,
        sortable: true,
        hideOnMobile: true,
        className: "text-muted-foreground",
        render: (c) => formatNumber(c.flows),
      },
      {
        key: "pct",
        header: "%",
        align: "right",
        sortable: true,
        render: (c) => <PercentBar pct={c.pct || 0} />,
      },
    ],
    [formatTraffic, periodSeconds],
  )

  const exportColumns: ExportColumn<CountryTraffic>[] = [
    { key: "country", header: "Country Code", value: (r) => r.country },
    { key: "name", header: "Country", value: (r) => countryName(r.country, r.name) },
    { key: "bytes", header: "Bytes", value: (r) => r.bytes },
    { key: "packets", header: "Packets", value: (r) => r.packets },
    { key: "flows", header: "Flows", value: (r) => r.flows },
    { key: "pct", header: "Percent", value: (r) => r.pct },
  ]

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold tracking-tight">Countries</h1>
      <p className="text-sm text-muted-foreground -mt-4">
        Traffic by country, derived from the source AS registration (AS-level geo — no per-IP lookup).
      </p>

      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base">Top 10 by traffic volume</CardTitle>
        </CardHeader>
        <CardContent>
          <QueryBoundary
            query={query}
            isEmpty={(d) => d.data.length === 0}
            skeleton={<div className="h-64 animate-pulse rounded bg-muted/40" />}
          >
            {(data) => <TopCountriesBar rows={data.data} />}
          </QueryBoundary>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="pb-3">
          <div className="flex items-center justify-between gap-2">
            <CardTitle className="text-base">Countries by traffic volume</CardTitle>
            <ExportButton rows={rows} columns={exportColumns} filename="top-countries" />
          </div>
        </CardHeader>
        <CardContent>
          <QueryBoundary query={query} isEmpty={(d) => d.data.length === 0} loadingCols={6}>
            {(data) => (
              <DataTable rows={data.data} columns={columns} rowKey={(c) => c.country || "unknown"} />
            )}
          </QueryBoundary>
        </CardContent>
      </Card>
    </div>
  )
}
