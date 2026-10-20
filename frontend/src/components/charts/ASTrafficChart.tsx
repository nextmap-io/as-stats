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
import { axisTick, seriesColor, tooltipBoxStyle, useChartColors, X_TICK_MARGIN } from "@/hooks/useChartColors"
import { formatTimeShort } from "./time"


interface ASTrafficChartProps {
  data: ASTrafficDetail[]
  height?: number
  title?: string
  timeBounds?: { from: number; to: number }
  /** Bucket width from the API (meta.step_seconds); inferred when absent. */
  stepSeconds?: number
}


// Color assignment is done internally and exposed via the static array
// Use getASColorsFromData in the parent component


export function ASTrafficChart({ data, height = 300, title, timeBounds, stepSeconds }: ASTrafficChartProps) {
  const { formatValue, formatAxis } = useUnit()
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

  // Detect multi-day
  let minTs = Infinity, maxTs = -Infinity
  for (const as of data) {
    for (const s of as.series) {
      for (const pt of s.points) {
        const t = new Date(pt.t).getTime()
        if (t < minTs) minTs = t
        if (t > maxTs) maxTs = t
      }
    }
  }
  const multiDay = (maxTs - minTs) > 86400000

  // Build AS keys and colors
  const asKeys = data.map(d => d.as_number)
  const asLabels: Record<number, string> = {}
  const colors: Record<number, { in: string; out: string }> = {}
  data.forEach((d, i) => {
    asLabels[d.as_number] = d.as_name || `AS${d.as_number}`
    const base = seriesColor(chartColors, i)
    colors[d.as_number] = { in: base, out: base }
  })

  // Collect all unique timestamps
  const tsSet = new Set<number>()
  for (const as of data) {
    for (const s of as.series) {
      for (const pt of s.points) {
        tsSet.add(new Date(pt.t).getTime())
      }
    }
  }

  // Initialize each timestamp with zero values for ALL ASes (avoids stacked
  // area gaps when one AS has data at a timestamp and another doesn't).
  const makeZeroRow = (): Record<string, number> => {
    const row: Record<string, number> = {}
    for (const asn of asKeys) {
      row[`${asn}_in`] = 0
      row[`${asn}_out`] = 0
    }
    return row
  }
  const dataByTs = new Map<number, Record<string, number>>()
  for (const ts of tsSet) {
    dataByTs.set(ts, makeZeroRow())
  }

  // Fill in actual values
  for (const as of data) {
    for (const s of as.series) {
      for (const pt of s.points) {
        const ts = new Date(pt.t).getTime()
        const row = dataByTs.get(ts)!
        row[`${as.as_number}_in`] += (pt.bytes_in || 0)
        row[`${as.as_number}_out`] -= (pt.bytes_out || 0)
      }
    }
  }

  // Build sorted data with gap fill and boundary padding
  const sortedTs = Array.from(dataByTs.keys()).sort((a, b) => a - b)
  const chartData: Record<string, unknown>[] = []

  if (timeBounds && sortedTs.length > 0 && stepMs > 0) {
    const firstTs = sortedTs[0]
    if (firstTs > timeBounds.from + stepMs) {
      chartData.push({ time: formatTimeShort(timeBounds.from, multiDay), ...makeZeroRow() })
      chartData.push({ time: formatTimeShort(firstTs - stepMs, multiDay), ...makeZeroRow() })
    }
  }

  for (let i = 0; i < sortedTs.length; i++) {
    if (i > 0 && stepMs > 0 && (sortedTs[i] - sortedTs[i - 1]) > stepMs * 2) {
      chartData.push({ time: formatTimeShort(sortedTs[i - 1] + stepMs, multiDay), ...makeZeroRow() })
      chartData.push({ time: formatTimeShort(sortedTs[i] - stepMs, multiDay), ...makeZeroRow() })
    }
    chartData.push({ time: formatTimeShort(sortedTs[i], multiDay), ...dataByTs.get(sortedTs[i]) })
  }

  if (timeBounds && sortedTs.length > 0 && stepMs > 0) {
    const lastTs = sortedTs[sortedTs.length - 1]
    if (lastTs < timeBounds.to - stepMs) {
      chartData.push({ time: formatTimeShort(lastTs + stepMs, multiDay), ...makeZeroRow() })
      chartData.push({ time: formatTimeShort(timeBounds.to, multiDay), ...makeZeroRow() })
    }
  }

  const tickInterval = chartData.length > 0 ? Math.max(1, Math.floor(chartData.length / 8)) : 1

  return (
    <div className="overflow-hidden">
      {title && <h3 className="mb-1 text-[13px] text-foreground-secondary">{title}</h3>}
      <ResponsiveContainer width="100%" height={height}>
        <AreaChart data={chartData} margin={{ top: 2, right: 2, left: 0, bottom: 0 }}>
          <CartesianGrid vertical={false} stroke={chartColors.grid} />
          <XAxis dataKey="time" tick={axisTick(chartColors)} tickLine={false} axisLine={false} tickMargin={X_TICK_MARGIN} interval={tickInterval} />
          <YAxis tick={axisTick(chartColors)} tickLine={false} axisLine={false} tickFormatter={(v) => formatAxis(Math.abs(Number(v)), interval)} width={44} />
          <ReferenceLine y={0} stroke={chartColors.ink} strokeWidth={1} />
          <Tooltip
            cursor={{ stroke: chartColors.text, strokeOpacity: 0.4 }}
            content={({ active, payload, label }) => {
              if (!active || !payload?.length) return null
              const byAS = new Map<number, { inVal: number; outVal: number }>()
              for (const e of payload) {
                const k = parseInt(String(e.dataKey).replace(/_in$|_out$/, ""))
                if (isNaN(k)) continue
                if (!byAS.has(k)) byAS.set(k, { inVal: 0, outVal: 0 })
                const l = byAS.get(k)!
                if (String(e.dataKey).endsWith("_in")) l.inVal = Math.abs(Number(e.value) || 0)
                else l.outVal = Math.abs(Number(e.value) || 0)
              }
              return (
                <div style={{ ...tooltipBoxStyle(chartColors), maxWidth: 340 }}>
                  <div style={{ color: chartColors.text, marginBottom: 3 }}>{label}</div>
                  {Array.from(byAS.entries()).map(([asn, { inVal, outVal }]) => {
                    if (inVal === 0 && outVal === 0) return null
                    return (
                      <div key={asn} style={{ display: "flex", alignItems: "center", gap: 6, lineHeight: 1.6 }}>
                        <span style={{ width: 8, height: 8, borderRadius: 1, backgroundColor: colors[asn]?.in, flexShrink: 0 }} />
                        <span style={{ flex: 1, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{asLabels[asn]}</span>
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
          {asKeys.map((asn) => (
            <Area key={`${asn}_in`} type="stepAfter" dataKey={`${asn}_in`} stackId="up" stroke={colors[asn].in} fill={colors[asn].in} fillOpacity={0.85} strokeWidth={0} dot={false} isAnimationActive={false} />
          ))}
          {asKeys.map((asn) => (
            <Area key={`${asn}_out`} type="stepAfter" dataKey={`${asn}_out`} stackId="down" stroke={colors[asn].out} fill={colors[asn].out} fillOpacity={0.5} strokeWidth={0} dot={false} isAnimationActive={false} />
          ))}
        </AreaChart>
      </ResponsiveContainer>
    </div>
  )
}
