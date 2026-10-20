import { useMemo } from "react"
import { Link, useSearchParams } from "react-router-dom"
import { useMovers, useTalkers, useTrafficHeatmap } from "@/hooks/useApi"
import { TrafficHeatmap } from "@/components/charts/TrafficHeatmap"
import { useFilters } from "@/hooks/useFilters"
import { useFeatureFlags } from "@/hooks/useFeatures"
import { useUnit } from "@/hooks/useUnit"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { QueryBoundary } from "@/components/QueryBoundary"
import { DataTable, type Column } from "@/components/DataTable"
import { ExportButton, type ExportColumn } from "@/components/ExportButton"
import { IPWithPTR } from "@/components/PTR"
import { formatPercent, cn } from "@/lib/utils"
import { countryFlag, countryName, hasCountry } from "@/lib/countries"
import { isPrivateASGroup } from "@/lib/asn"
import { ASRef } from "@/components/ASRef"
import { ArrowDown, ArrowUp } from "lucide-react"
import type { Mover, TalkerChange } from "@/lib/types"
import { Segmented } from "@/components/ui/segmented"
import { portHistoryNote } from "@/lib/ports"

type MoverDim = "as" | "prefix" | "port" | "country"
type TalkerDim = "as" | "ip" | "prefix"

const MOVER_DIMS: { value: MoverDim; label: string; feature?: "port_stats" }[] = [
  { value: "as", label: "AS" },
  { value: "prefix", label: "Prefix" },
  { value: "port", label: "Service port", feature: "port_stats" },
  { value: "country", label: "Country" },
]

const TALKER_DIMS: { value: TalkerDim; label: string }[] = [
  { value: "as", label: "AS" },
  { value: "ip", label: "IP" },
  { value: "prefix", label: "Prefix" },
]

function asMoverDim(v: string | null): MoverDim {
  return v === "prefix" || v === "port" || v === "country" ? v : "as"
}
function asTalkerDim(v: string | null): TalkerDim {
  return v === "ip" || v === "prefix" ? v : "as"
}

/** Renders the entity identity for a changes row, linking where a detail page
 *  exists (as/ip) and formatting port/country specially. */
function EntityCell({
  dim,
  entityKey,
  label,
  filterSearch,
}: {
  dim: MoverDim | TalkerDim
  entityKey: string
  label?: string
  filterSearch: string
}) {
  if (dim === "as") {
    return (
      <span className="inline-flex items-baseline gap-2 min-w-0">
        <ASRef
          asn={entityKey}
          search={filterSearch}
          className="text-primary hover:underline shrink-0"
          groupClassName="text-muted-foreground shrink-0"
        />
        {label && !isPrivateASGroup(entityKey) && (
          <span className="text-muted-foreground truncate text-[13px]">{label}</span>
        )}
      </span>
    )
  }
  if (dim === "ip") {
    return (
      <Link
        to={`/ip/${entityKey}${filterSearch}`}
        className="text-primary hover:underline text-[13px] block truncate max-w-[260px]"
      >
        <IPWithPTR ip={entityKey} />
      </Link>
    )
  }
  if (dim === "country") {
    const code = entityKey
    return (
      <span className="inline-flex items-baseline gap-2">
        {hasCountry(code) && <span aria-hidden>{countryFlag(code)}</span>}
        <span className="">{code}</span>
        <span className="text-muted-foreground truncate text-[13px]">{label || countryName(code)}</span>
      </span>
    )
  }
  // prefix | port — plain monospaced identity
  return <span className="text-[13px]">{entityKey}</span>
}

export function Changes() {
  const { filters, filterSearch, periodSeconds } = useFilters()
  const { formatTraffic } = useUnit()
  const features = useFeatureFlags()
  const [searchParams, setSearchParams] = useSearchParams()

  const moverDims = MOVER_DIMS.filter((d) => !d.feature || features[d.feature])
  const mdim = asMoverDim(searchParams.get("mdim"))
  // Guard: if the URL asks for a feature-gated dimension that is disabled, fall
  // back to "as" so we never issue a request the backend will reject.
  const activeMoverDim = moverDims.some((d) => d.value === mdim) ? mdim : "as"
  const tdim = asTalkerDim(searchParams.get("tdim"))

  const moversQuery = useMovers(activeMoverDim, filters)
  const moverPortNote = portHistoryNote(moversQuery.data?.meta?.port_history_since, true)
  const talkersQuery = useTalkers(tdim, filters)

  const setDim = (param: "mdim" | "tdim", value: string) => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        next.set(param, value)
        return next
      },
      { replace: true },
    )
  }

  // Movers: gainers + losers merged, ranked by absolute delta desc.
  const moverRows: Mover[] = useMemo(() => {
    const d = moversQuery.data?.data
    if (!d) return []
    return [...d.gainers, ...d.losers].sort((a, b) => Math.abs(b.delta) - Math.abs(a.delta))
  }, [moversQuery.data])

  // Talkers: new + gone merged, appeared first then by volume desc.
  const talkerRows: TalkerChange[] = useMemo(() => {
    const d = talkersQuery.data?.data
    if (!d) return []
    return [...d.new, ...d.gone].sort((a, b) => b.bytes - a.bytes)
  }, [talkersQuery.data])

  const moverColumns = useMemo<Column<Mover>[]>(
    () => [
      {
        key: "entity",
        header: "Entity",
        className: "max-w-[280px]",
        render: (m) => <EntityCell dim={activeMoverDim} entityKey={m.key} label={m.label} filterSearch={filterSearch} />,
      },
      {
        key: "current",
        header: "Current",
        align: "right",
        numeric: true,
        sortable: true,
        render: (m) => formatTraffic(m.current, periodSeconds),
      },
      {
        key: "previous",
        header: "Previous",
        align: "right",
        numeric: true,
        sortable: true,
        hideOnMobile: true,
        className: "text-muted-foreground",
        render: (m) => formatTraffic(m.previous, periodSeconds),
      },
      {
        key: "delta",
        header: "Δ",
        align: "right",
        numeric: true,
        sortable: true,
        sortValue: (m) => m.delta,
        render: (m) => {
          const up = m.delta > 0
          const cls = m.delta === 0 ? "text-muted-foreground" : up ? "text-foreground" : "text-foreground-secondary"
          const Icon = up ? ArrowUp : ArrowDown
          const sign = up ? "+" : "-"
          return (
            <span className={cn("inline-flex items-center justify-end gap-1", cls)}>
              {m.delta !== 0 && <Icon className="size-3 shrink-0" aria-hidden />}
              <span>
                {sign}
                {formatTraffic(Math.abs(m.delta), periodSeconds)}
              </span>
              {m.previous > 0 && (
                <span className="text-xs text-muted-foreground">
                  ({up ? "+" : ""}
                  {formatPercent(m.pct)})
                </span>
              )}
            </span>
          )
        },
      },
    ],
    [activeMoverDim, filterSearch, formatTraffic, periodSeconds],
  )

  const talkerColumns = useMemo<Column<TalkerChange>[]>(
    () => [
      {
        key: "entity",
        header: "Entity",
        className: "max-w-[280px]",
        render: (t) => <EntityCell dim={tdim} entityKey={t.key} label={t.label} filterSearch={filterSearch} />,
      },
      {
        key: "bytes",
        header: "Volume",
        align: "right",
        numeric: true,
        sortable: true,
        render: (t) => formatTraffic(t.bytes, periodSeconds),
      },
      {
        key: "status",
        header: "Status",
        align: "right",
        sortable: true,
        render: (t) => (
          <span
            className={cn(
              "inline-flex items-center rounded px-1.5 py-0.5 text-xs font-medium  ",
              t.status === "new"
                ? "border border-foreground text-foreground"
                : "border border-border text-muted-foreground",
            )}
          >
            {t.status === "new" ? "New" : "Gone"}
          </span>
        ),
      },
    ],
    [tdim, filterSearch, formatTraffic, periodSeconds],
  )

  const moverExport: ExportColumn<Mover>[] = [
    { key: "dimension", header: "Dimension", value: (r) => r.dimension },
    { key: "key", header: "Key", value: (r) => r.key },
    { key: "label", header: "Label", value: (r) => r.label ?? "" },
    { key: "current", header: "Current Bytes", value: (r) => r.current },
    { key: "previous", header: "Previous Bytes", value: (r) => r.previous },
    { key: "delta", header: "Delta Bytes", value: (r) => r.delta },
    { key: "pct", header: "Pct", value: (r) => r.pct },
  ]

  const talkerExport: ExportColumn<TalkerChange>[] = [
    { key: "dimension", header: "Dimension", value: (r) => r.dimension },
    { key: "key", header: "Key", value: (r) => r.key },
    { key: "label", header: "Label", value: (r) => r.label ?? "" },
    { key: "bytes", header: "Bytes", value: (r) => r.bytes },
    { key: "status", header: "Status", value: (r) => r.status },
  ]

  return (
    <div className="space-y-8">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h1 className="text-2xl font-semibold">What changed</h1>
        <span className="text-[13px] text-muted-foreground">
          This window against the equal-length window just before it
        </span>
      </div>

      {/* Top movers */}
      <Card>
        <CardHeader>
          <div className="flex flex-wrap items-center justify-between gap-2">
            <CardTitle>Top movers</CardTitle>
            <div className="flex flex-wrap items-center gap-2">
              <Segmented
                label="Movers dimension"
                value={activeMoverDim}
                onChange={(v) => setDim("mdim", v)}
                options={moverDims}
              />
              <ExportButton rows={moverRows} columns={moverExport} filename={`movers-${activeMoverDim}`} />
            </div>
          </div>
        </CardHeader>
        <CardContent>
          {activeMoverDim === "port" && (
            <div className="mb-2 space-y-1 text-[13px] text-muted-foreground">
              <p>Protocol / service port, all directions, each flow counted once.</p>
              {moverPortNote && <p>{moverPortNote}</p>}
            </div>
          )}
          <QueryBoundary query={moversQuery} isEmpty={() => moverRows.length === 0} loadingCols={4}>
            {() => (
              <DataTable
                rows={moverRows}
                columns={moverColumns}
                rowKey={(m) => m.key}
                sortParam="msort"
                dirParam="mdir"
              />
            )}
          </QueryBoundary>
        </CardContent>
      </Card>

      {/* New / disappeared talkers */}
      <Card>
        <CardHeader>
          <div className="flex flex-wrap items-center justify-between gap-2">
            <CardTitle>New and disappeared talkers</CardTitle>
            <div className="flex flex-wrap items-center gap-2">
              <Segmented
                label="Talkers dimension"
                value={tdim}
                onChange={(v) => setDim("tdim", v)}
                options={TALKER_DIMS}
              />
              <ExportButton rows={talkerRows} columns={talkerExport} filename={`talkers-${tdim}`} />
            </div>
          </div>
        </CardHeader>
        <CardContent>
          <QueryBoundary query={talkersQuery} isEmpty={() => talkerRows.length === 0} loadingCols={3}>
            {() => (
              <DataTable
                rows={talkerRows}
                columns={talkerColumns}
                rowKey={(t) => `${t.status}-${t.key}`}
                sortParam="tsort"
                dirParam="tdir"
              />
            )}
          </QueryBoundary>
        </CardContent>
      </Card>

      <WeeklyPattern />
    </div>
  )
}

const HEATMAP_WEEKS = [4, 8, 12, 26] as const

/**
 * WeeklyPattern (moved here from the home page): a day × hour heatmap. Each of
 * the 7×24 cells only means something once several weeks of samples land in
 * it, so it uses its own multi-week lookback (?hweeks=, default 12) instead of
 * the global time range, while still honouring the link / direction filters.
 * `to` is snapped to the hour to keep the query key stable between renders.
 */
function WeeklyPattern() {
  const { filters, bounds } = useFilters()
  const [searchParams, setSearchParams] = useSearchParams()
  const raw = Number(searchParams.get("hweeks"))
  const weeks = (HEATMAP_WEEKS as readonly number[]).includes(raw) ? raw : 12
  const heatmapFilters = useMemo(() => {
    const hour = Math.floor(bounds.to / 3_600_000) * 3_600_000
    return {
      ...filters,
      period: undefined,
      from: new Date(hour - weeks * 7 * 86_400_000).toISOString(),
      to: new Date(hour).toISOString(),
    }
  }, [filters, bounds.to, weeks])
  const heatmap = useTrafficHeatmap(heatmapFilters)

  const setWeeks = (w: number) =>
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        if (w === 12) next.delete("hweeks")
        else next.set("hweeks", String(w))
        return next
      },
      { replace: true },
    )

  return (
    <Card>
      <CardHeader>
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div>
            <CardTitle>Weekly pattern</CardTitle>
            <p className="mt-0.5 text-[13px] text-muted-foreground">
              Throughput by day of week and hour, over the last {weeks} weeks. It ignores the time range above.
            </p>
          </div>
          <div className="inline-flex overflow-hidden rounded border border-input" role="group" aria-label="Heatmap lookback">
            {HEATMAP_WEEKS.map((w) => (
              <button
                key={w}
                type="button"
                onClick={() => setWeeks(w)}
                aria-pressed={weeks === w}
                title={`Aggregate the last ${w} weeks`}
                className={cn(
                  "px-2.5 py-1 text-[13px]",
                  weeks === w ? "bg-accent font-semibold text-foreground" : "text-foreground-secondary hover:text-foreground",
                )}
              >
                {w} weeks
              </button>
            ))}
          </div>
        </div>
      </CardHeader>
      <CardContent>
        <QueryBoundary
          query={heatmap}
          isEmpty={(d) => !d.data?.cells?.length || d.data.cells.every((c) => c.mean_bps === 0 && c.peak_bps === 0)}
          loadingRows={7}
          loadingCols={12}
        >
          {(data) => <TrafficHeatmap cells={data.data.cells} />}
        </QueryBoundary>
      </CardContent>
    </Card>
  )
}
