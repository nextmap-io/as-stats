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
import type { LinkTimeSeries } from "@/lib/types"
import { useUnit } from "@/hooks/useUnit"
import { axisTick, seriesColor, tooltipBoxStyle, useChartColors, annotationLabel, X_TICK_MARGIN } from "@/hooks/useChartColors"
import { inferStepSeconds, formatTimeShort } from "./time"


interface LinkTrafficChartProps {
  series: LinkTimeSeries[]
  height?: number
  title?: string
  linkColors?: Record<string, string>
  p95In?: number
  p95Out?: number
  hideLegend?: boolean
  timeBounds?: { from: number; to: number }
  /** Bucket width from the API (meta.step_seconds); inferred when absent. */
  stepSeconds?: number
}


export function LinkTrafficChart({ series, height = 260, title, linkColors, p95In, p95Out, hideLegend, timeBounds, stepSeconds }: LinkTrafficChartProps) {
  const { formatValue, formatAxis, unit } = useUnit()
  const chartColors = useChartColors()
  if (!series.length) return null
  const interval = stepSeconds && stepSeconds > 0 ? stepSeconds : inferStepSeconds(series.map((s) => s.points))
  const stepMs = interval * 1000
  // pps needs packet counts; without them the chart stays in bits per second.
  const hasPackets = series.some((s) => s.points.some((p) => (p.packets_in ?? 0) > 0 || (p.packets_out ?? 0) > 0))
  const usePps = unit === "pps" && hasPackets
  const kind = usePps ? "packets" : "bytes"

  // Detect if data spans multiple days
  let minTs = Infinity, maxTs = -Infinity
  for (const ls of series) {
    for (const pt of ls.points) {
      const t = new Date(pt.t).getTime()
      if (t < minTs) minTs = t
      if (t > maxTs) maxTs = t
    }
  }
  // Prefer the selected window's span (when provided) so the axis granularity
  // matches what's shown, not just where data happens to land.
  const span = timeBounds ? timeBounds.to - timeBounds.from : maxTs - minTs
  const multiDay = span > 86400000

  const linkTags: string[] = []
  const linkLabels: Record<string, string> = {}
  const colors: Record<string, { in: string; out: string }> = {}

  for (let i = 0; i < series.length; i++) {
    const ls = series[i]
    linkTags.push(ls.link_tag)
    linkLabels[ls.link_tag] = ls.description || ls.link_tag
    const base = seriesColor(chartColors, i, linkColors?.[ls.link_tag])
    colors[ls.link_tag] = { in: base, out: base }
  }

  // Collect all unique timestamps across all series
  const tsSet = new Set<number>()
  for (const ls of series) {
    for (const pt of ls.points) {
      tsSet.add(new Date(pt.t).getTime())
    }
  }

  // Initialize each timestamp with zero values for ALL link tags.
  // This ensures stacked areas don't break when one link has data at a
  // timestamp and another doesn't.
  const dataByTs = new Map<number, Record<string, number>>()
  const makeZeroRow = (): Record<string, number> => {
    const row: Record<string, number> = {}
    for (const tag of linkTags) {
      row[`${tag}_in`] = 0
      row[`${tag}_out`] = 0
    }
    return row
  }
  for (const ts of tsSet) {
    dataByTs.set(ts, makeZeroRow())
  }

  // Fill in actual values
  for (const ls of series) {
    for (const pt of ls.points) {
      const ts = new Date(pt.t).getTime()
      const row = dataByTs.get(ts)!
      row[`${ls.link_tag}_in`] = usePps ? (pt.packets_in || 0) : (pt.bytes_in || 0)
      row[`${ls.link_tag}_out`] = -(usePps ? (pt.packets_out || 0) : (pt.bytes_out || 0))
    }
  }

  // Build data array from actual timestamps, inserting zeros for gaps.
  //
  // The gap threshold controls when we drop the area to zero between data
  // points. Too aggressive (2x) fragments sparse per-AS data into 1-pixel
  // bars. Too lenient risks showing phantom plateaus across real outages.
  //
  // We use 4x the step: up to 3 consecutive missing buckets are "normal
  // jitter" and the area stays up; 4+ missing buckets are a real gap and
  // we drop to zero.
  const GAP_THRESHOLD = 4
  const sortedTs = Array.from(dataByTs.keys()).sort((a, b) => a - b)
  // Rows carry a numeric `t` (epoch ms) so the X axis is a real time scale with
  // a fixed domain (below), not a category axis derived from present points.
  // That keeps every card on the same time base and stops sparse data from
  // ballooning into giant blocks.
  const data: Record<string, unknown>[] = []

  // Boundary padding: add a single zero point at the start if data begins late,
  // so a stepAfter area doesn't back-fill the empty lead-in.
  if (timeBounds && sortedTs.length > 0 && stepMs > 0) {
    const firstTs = sortedTs[0]
    if (firstTs > timeBounds.from + stepMs * GAP_THRESHOLD) {
      data.push({ t: firstTs - stepMs, ...makeZeroRow() })
    }
  }

  for (let i = 0; i < sortedTs.length; i++) {
    // Insert a single zero row before a real gap so the chart drops to 0
    if (i > 0 && stepMs > 0 && (sortedTs[i] - sortedTs[i - 1]) > stepMs * GAP_THRESHOLD) {
      data.push({ t: sortedTs[i - 1] + stepMs, ...makeZeroRow() })
      data.push({ t: sortedTs[i] - stepMs, ...makeZeroRow() })
    }
    const t = sortedTs[i]
    data.push({ t, ...dataByTs.get(t) })
  }

  // Boundary padding at the end so the last value doesn't plateau across an
  // otherwise-empty tail of the window.
  if (timeBounds && sortedTs.length > 0 && stepMs > 0) {
    const lastTs = sortedTs[sortedTs.length - 1]
    if (lastTs < timeBounds.to - stepMs * GAP_THRESHOLD) {
      data.push({ t: lastTs + stepMs, ...makeZeroRow() })
    }
  }

  // Fixed time domain + evenly spaced ticks. Falls back to the data extent when
  // no window is supplied.
  const domainFrom = timeBounds ? timeBounds.from : sortedTs[0] ?? 0
  const domainTo = timeBounds ? timeBounds.to : sortedTs[sortedTs.length - 1] ?? domainFrom + stepMs
  const TICK_COUNT = 6
  const xTicks =
    domainTo > domainFrom
      ? Array.from({ length: TICK_COUNT + 1 }, (_, i) => Math.round(domainFrom + ((domainTo - domainFrom) * i) / TICK_COUNT))
      : undefined

  return (
    <div className="overflow-hidden">
      {title && <h3 className="mb-1 text-[13px] text-foreground-secondary">{title}</h3>}
      <ResponsiveContainer width="100%" height={height}>
        <AreaChart data={data} margin={{ top: 2, right: 2, left: 0, bottom: 0 }}>
          {/* No gradients — solid fills like classic rrdtool */}
          <CartesianGrid vertical={false} stroke={chartColors.grid} />
          <XAxis
            tickMargin={X_TICK_MARGIN}
            type="number"
            dataKey="t"
            scale="time"
            domain={[domainFrom, domainTo]}
            ticks={xTicks}
            allowDataOverflow
            tick={axisTick(chartColors)}
            tickLine={false}
            axisLine={false}
            tickFormatter={(t) => formatTimeShort(Number(t), multiDay)}
          />
          <YAxis
            tick={axisTick(chartColors)}
            tickLine={false}
            axisLine={false}
            tickFormatter={(v) => formatAxis(Math.abs(Number(v)), interval, kind)}
            width={44}
          />
          <ReferenceLine y={0} stroke={chartColors.ink} strokeWidth={1} />
          {!usePps && p95In != null && p95In > 0 && (
            <ReferenceLine y={p95In} stroke={chartColors.ink} strokeDasharray="2 3" label={annotationLabel(chartColors, `p95 in ${formatValue(p95In, interval)}`, "insideTopRight")} />
          )}
          {!usePps && p95Out != null && p95Out > 0 && (
            <ReferenceLine y={-p95Out} stroke={chartColors.ink} strokeDasharray="2 3" label={annotationLabel(chartColors, `p95 out ${formatValue(p95Out, interval)}`, "insideBottomRight")} />
          )}
          <Tooltip
            cursor={{ stroke: chartColors.text, strokeOpacity: 0.4 }}
            content={({ active, payload, label }) => {
              if (!active || !payload?.length) return null
              const byLink = new Map<string, { inVal: number; outVal: number }>()
              for (const e of payload) {
                const k = String(e.dataKey).replace(/_in$|_out$/, "")
                if (!byLink.has(k)) byLink.set(k, { inVal: 0, outVal: 0 })
                const l = byLink.get(k)!
                if (String(e.dataKey).endsWith("_in")) l.inVal = Math.abs(Number(e.value) || 0)
                else l.outVal = Math.abs(Number(e.value) || 0)
              }
              return (
                <div style={tooltipBoxStyle(chartColors)}>
                  <div style={{ color: chartColors.text, marginBottom: 3 }}>{formatTimeShort(Number(label), true)}</div>
                  {Array.from(byLink.entries()).map(([tag, { inVal, outVal }]) => {
                    if (inVal === 0 && outVal === 0) return null
                    return (
                      <div key={tag} style={{ display: "flex", alignItems: "center", gap: 6, lineHeight: 1.6 }}>
                        <span style={{ width: 8, height: 8, borderRadius: 1, backgroundColor: colors[tag]?.in, flexShrink: 0 }} />
                        <span>{linkLabels[tag] || tag}</span>
                        <span style={{ marginLeft: "auto", paddingLeft: 12, whiteSpace: "nowrap" }}>
                          {inVal > 0 && <>in {formatValue(inVal, interval, kind)}</>}
                          {inVal > 0 && outVal > 0 && ", "}
                          {outVal > 0 && <>out {formatValue(outVal, interval, kind)}</>}
                        </span>
                      </div>
                    )
                  })}
                </div>
              )
            }}
          />
          {/* Inbound areas (positive, stacked upward) — solid fill */}
          {linkTags.map((tag) => (
            <Area
              key={`${tag}_in`}
              type="stepAfter"
              dataKey={`${tag}_in`}
              stackId="up"
              stroke={colors[tag].in}
              fill={colors[tag].in}
              fillOpacity={0.85}
              strokeWidth={0}
              dot={false}
              isAnimationActive={false}
            />
          ))}
          {/* Outbound areas (negative, stacked downward) — lighter solid fill */}
          {linkTags.map((tag) => (
            <Area
              key={`${tag}_out`}
              type="stepAfter"
              dataKey={`${tag}_out`}
              stackId="down"
              stroke={colors[tag].out}
              fill={colors[tag].out}
              fillOpacity={0.5}
              strokeWidth={0}
              dot={false}
              isAnimationActive={false}
            />
          ))}
        </AreaChart>
      </ResponsiveContainer>
      {!hideLegend && (
        <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1">
          {linkTags.map((tag) => (
            <div key={tag} className="flex items-center gap-1.5 text-[13px] text-muted-foreground">
              <span className="inline-block h-2.5 w-3.5 rounded-[1px]" style={{ backgroundColor: colors[tag].in }} />
              <span>{linkLabels[tag]}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
