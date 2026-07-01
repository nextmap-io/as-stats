import { useMemo } from "react"
import { Link, useSearchParams } from "react-router-dom"
import { useMovers, useTalkers } from "@/hooks/useApi"
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
import { ArrowDown, ArrowUp } from "lucide-react"
import type { Mover, TalkerChange } from "@/lib/types"

type MoverDim = "as" | "prefix" | "port" | "country"
type TalkerDim = "as" | "ip" | "prefix"

const MOVER_DIMS: { value: MoverDim; label: string; feature?: "port_stats" }[] = [
  { value: "as", label: "AS" },
  { value: "prefix", label: "Prefix" },
  { value: "port", label: "Port", feature: "port_stats" },
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
        <Link to={`/as/${entityKey}${filterSearch}`} className="text-primary hover:underline font-mono shrink-0">
          AS{entityKey}
        </Link>
        {label && <span className="text-muted-foreground truncate text-[11px]">{label}</span>}
      </span>
    )
  }
  if (dim === "ip") {
    return (
      <Link
        to={`/ip/${entityKey}${filterSearch}`}
        className="text-primary hover:underline font-mono text-[11px] block truncate max-w-[260px]"
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
        <span className="font-mono">{code}</span>
        <span className="text-muted-foreground truncate text-[11px]">{label || countryName(code)}</span>
      </span>
    )
  }
  // prefix | port — plain monospaced identity
  return <span className="font-mono text-[11px]">{entityKey}</span>
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
          const cls = up ? "text-success" : m.delta < 0 ? "text-destructive" : "text-muted-foreground"
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
                <span className="text-[10px] opacity-70">
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
              "inline-flex items-center rounded px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide",
              t.status === "new"
                ? "bg-success/15 text-success border border-success/30"
                : "bg-warning/15 text-warning border border-warning/30",
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
    <div className="space-y-6">
      <div className="flex items-baseline justify-between">
        <h1 className="text-lg font-semibold tracking-tight">What changed</h1>
        <span className="text-[10px] text-muted-foreground">
          Current window vs. the immediately-prior equal-length window
        </span>
      </div>

      {/* Top movers */}
      <Card>
        <CardHeader className="pb-3">
          <div className="flex items-center justify-between gap-2">
            <CardTitle className="text-sm">Top movers</CardTitle>
            <div className="flex items-center gap-2">
              <div
                className="flex gap-0.5 rounded border border-input bg-muted/30 p-0.5"
                role="group"
                aria-label="Movers dimension"
              >
                {moverDims.map((d) => (
                  <button
                    key={d.value}
                    type="button"
                    onClick={() => setDim("mdim", d.value)}
                    aria-pressed={activeMoverDim === d.value}
                    className={cn(
                      "px-2 py-0.5 text-[11px] font-medium rounded transition-colors",
                      activeMoverDim === d.value
                        ? "bg-primary text-primary-foreground"
                        : "text-muted-foreground hover:text-foreground hover:bg-accent",
                    )}
                  >
                    {d.label}
                  </button>
                ))}
              </div>
              <ExportButton rows={moverRows} columns={moverExport} filename={`movers-${activeMoverDim}`} />
            </div>
          </div>
        </CardHeader>
        <CardContent>
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
        <CardHeader className="pb-3">
          <div className="flex items-center justify-between gap-2">
            <CardTitle className="text-sm">New / disappeared talkers</CardTitle>
            <div className="flex items-center gap-2">
              <div
                className="flex gap-0.5 rounded border border-input bg-muted/30 p-0.5"
                role="group"
                aria-label="Talkers dimension"
              >
                {TALKER_DIMS.map((d) => (
                  <button
                    key={d.value}
                    type="button"
                    onClick={() => setDim("tdim", d.value)}
                    aria-pressed={tdim === d.value}
                    className={cn(
                      "px-2 py-0.5 text-[11px] font-medium rounded transition-colors",
                      tdim === d.value
                        ? "bg-primary text-primary-foreground"
                        : "text-muted-foreground hover:text-foreground hover:bg-accent",
                    )}
                  >
                    {d.label}
                  </button>
                ))}
              </div>
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
    </div>
  )
}
