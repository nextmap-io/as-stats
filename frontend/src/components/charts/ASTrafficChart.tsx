import {
  AreaChart,
  Area,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
  ReferenceLine,
} from "recharts"
import type { ASTrafficDetail } from "@/lib/types"
import { useUnit } from "@/hooks/useUnit"
import { asSeriesColor, axisTick, tooltipBoxStyle, useChartColors, X_TICK_MARGIN, SERIES_COUNT } from "@/hooks/useChartColors"
import { rateAxisTicks } from "@/lib/rate"
import { formatTimeShort, timeTicks } from "./time"


interface ASTrafficChartProps {
  data: ASTrafficDetail[]
  height?: number
  title?: string
  timeBounds?: { from: number; to: number }
  /** Bucket width from the API (meta.step_seconds); inferred when absent. */
  stepSeconds?: number
}

/** Series key of the folded "other networks" area. */
const OTHER = "other"

/**
 * Stacked per-AS traffic, inbound above the axis and outbound below. The first
 * SERIES_COUNT networks get the ordinal palette, in order (the same colours
 * `asSeriesColor` gives a table next to the chart); the rest fold into one
 * neutral "Other networks" area instead of reusing a colour already on screen.
 */
export function ASTrafficChart({ data, height = 300, title, timeBounds, stepSeconds }: ASTrafficChartProps) {
  const { formatValue, formatAxis, unit } = useUnit()
  const chartColors = useChartColors()
  if (!data.length) return null

  // Compute step from all series
  let minDiff = Infinity
  for (const as of data) {
    for (const s of as.series) {
      for (let i = 1; i < s.points.length; i++) {
        const diff = (new Date(s.points[i].t).getTime() - new Date(s.points[i - 1].t).getTime()) / 1000
        if (diff > 0 && diff < minDiff) minDiff = diff
      }
    }
  }
  const interval = stepSeconds && stepSeconds > 0 ? stepSeconds : minDiff === Infinity ? 300 : minDiff
  const stepMs = interval * 1000

  // Series keys: one per charted AS, plus OTHER for the folded tail.
  const keyOf = (i: number) => (i < SERIES_COUNT ? String(data[i].as_number) : OTHER)
  const keys: string[] = []
  const labels: Record<string, string> = {}
  const colors: Record<string, string> = {}
  data.forEach((d, i) => {
    const k = keyOf(i)
    if (k === OTHER && labels[OTHER]) return
    keys.push(k)
    labels[k] = k === OTHER ? `Other networks (${data.length - SERIES_COUNT})` : d.as_name || `AS${d.as_number}`
    colors[k] = asSeriesColor(chartColors, i)
  })

  const makeZeroRow = (): Record<string, number> => {
    const row: Record<string, number> = {}
    for (const k of keys) {
      row[`${k}_in`] = 0
      row[`${k}_out`] = 0
    }
    return row
  }
  const dataByTs = new Map<number, Record<string, number>>()
  data.forEach((as, i) => {
    const k = keyOf(i)
    for (const s of as.series) {
      for (const pt of s.points) {
        const ts = new Date(pt.t).getTime()
        let row = dataByTs.get(ts)
        if (!row) {
          row = makeZeroRow()
          dataByTs.set(ts, row)
        }
        row[`${k}_in`] += pt.bytes_in || 0
        row[`${k}_out`] -= pt.bytes_out || 0
      }
    }
  })

  // Numeric time axis on the window, zero rows around gaps (same rules as the
  // other traffic charts) so a sparse AS does not balloon into blocks.
  const GAP_THRESHOLD = 4
  const sortedTs = Array.from(dataByTs.keys()).sort((a, b) => a - b)
  const rows: Record<string, number>[] = []
  if (timeBounds && sortedTs.length > 0 && stepMs > 0 && sortedTs[0] > timeBounds.from + stepMs * GAP_THRESHOLD) {
    rows.push({ t: sortedTs[0] - stepMs, ...makeZeroRow() })
  }
  for (let i = 0; i < sortedTs.length; i++) {
    if (i > 0 && stepMs > 0 && sortedTs[i] - sortedTs[i - 1] > stepMs * GAP_THRESHOLD) {
      rows.push({ t: sortedTs[i - 1] + stepMs, ...makeZeroRow() })
      rows.push({ t: sortedTs[i] - stepMs, ...makeZeroRow() })
    }
    rows.push({ t: sortedTs[i], ...dataByTs.get(sortedTs[i]) })
  }
  if (timeBounds && sortedTs.length > 0 && stepMs > 0) {
    const lastTs = sortedTs[sortedTs.length - 1]
    if (lastTs < timeBounds.to - stepMs * GAP_THRESHOLD) rows.push({ t: lastTs + stepMs, ...makeZeroRow() })
  }

  const domainFrom = timeBounds ? timeBounds.from : (sortedTs[0] ?? 0)
  const domainTo = timeBounds ? timeBounds.to : (sortedTs[sortedTs.length - 1] ?? domainFrom + stepMs)
  const multiDay = domainTo - domainFrom > 86_400_000
  const xTicks = domainTo > domainFrom ? timeTicks(domainFrom, domainTo) : undefined

  let yMin = 0
  let yMax = 0
  for (const row of dataByTs.values()) {
    let up = 0
    let down = 0
    for (const k of keys) {
      up += row[`${k}_in`]
      down += row[`${k}_out`]
    }
    yMax = Math.max(yMax, up)
    yMin = Math.min(yMin, down)
  }
  const y = rateAxisTicks(yMin, yMax, interval, unit, "bytes")

  return (
    <div className="overflow-hidden">
      {/* Top-level block of its page, so an h2 under the page h1. */}
      {title && <h2 className="mb-1 text-[13px] font-normal text-foreground-secondary">{title}</h2>}
      <ResponsiveContainer width="100%" height={height}>
        <AreaChart data={rows} margin={{ top: 2, right: 2, left: 0, bottom: 0 }}>
          <CartesianGrid vertical={false} stroke={chartColors.grid} />
          <XAxis
            type="number"
            dataKey="t"
            scale="time"
            domain={[domainFrom, domainTo]}
            ticks={xTicks}
            allowDataOverflow
            tick={axisTick(chartColors)}
            tickLine={false}
            axisLine={false}
            tickMargin={X_TICK_MARGIN}
            tickFormatter={(t) => formatTimeShort(Number(t), multiDay)}
          />
          <YAxis
            ticks={y.ticks}
            domain={y.domain}
            interval={0}
            tick={axisTick(chartColors)}
            tickLine={false}
            axisLine={false}
            tickFormatter={(v) => formatAxis(Math.abs(Number(v)), interval)}
            width={44}
          />
          <ReferenceLine y={0} stroke={chartColors.ink} strokeWidth={1} />
          <Tooltip
            cursor={{ stroke: chartColors.text, strokeOpacity: 0.4 }}
            content={({ active, payload, label }) => {
              if (!active || !payload?.length) return null
              const byKey = new Map<string, { inVal: number; outVal: number }>()
              for (const e of payload) {
                const k = String(e.dataKey).replace(/_in$|_out$/, "")
                if (!labels[k]) continue
                if (!byKey.has(k)) byKey.set(k, { inVal: 0, outVal: 0 })
                const l = byKey.get(k)!
                if (String(e.dataKey).endsWith("_in")) l.inVal = Math.abs(Number(e.value) || 0)
                else l.outVal = Math.abs(Number(e.value) || 0)
              }
              return (
                <div style={{ ...tooltipBoxStyle(chartColors), maxWidth: 340 }}>
                  <div style={{ color: chartColors.text, marginBottom: 3 }}>{formatTimeShort(Number(label), true)}</div>
                  {Array.from(byKey.entries()).map(([k, { inVal, outVal }]) => {
                    if (inVal === 0 && outVal === 0) return null
                    return (
                      <div key={k} style={{ display: "flex", alignItems: "center", gap: 6, lineHeight: 1.6 }}>
                        <span style={{ width: 8, height: 8, borderRadius: 1, backgroundColor: colors[k], flexShrink: 0 }} />
                        <span style={{ flex: 1, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{labels[k]}</span>
                        <span style={{ whiteSpace: "nowrap" }}>
                          {inVal > 0 && <>in {formatValue(inVal, interval)}</>}
                          {inVal > 0 && outVal > 0 && ", "}
                          {outVal > 0 && <>out {formatValue(outVal, interval)}</>}
                        </span>
                      </div>
                    )
                  })}
                </div>
              )
            }}
          />
          {keys.map((k) => (
            <Area key={`${k}_in`} type="stepAfter" dataKey={`${k}_in`} stackId="up" stroke={colors[k]} fill={colors[k]} fillOpacity={0.85} strokeWidth={0} dot={false} isAnimationActive={false} />
          ))}
          {keys.map((k) => (
            <Area key={`${k}_out`} type="stepAfter" dataKey={`${k}_out`} stackId="down" stroke={colors[k]} fill={colors[k]} fillOpacity={0.5} strokeWidth={0} dot={false} isAnimationActive={false} />
          ))}
        </AreaChart>
      </ResponsiveContainer>
    </div>
  )
}
