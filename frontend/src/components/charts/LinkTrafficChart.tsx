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
import { useChartColors } from "@/hooks/useChartColors"

const DEFAULT_COLORS = [
  "#e74c3c",
  "#3498db",
  "#2ecc71",
  "#f39c12",
  "#9b59b6",
  "#1abc9c",
  "#e67e22",
  "#2980b9",
]

function lighten(hex: string, amount = 0.35): string {
  const r = parseInt(hex.slice(1, 3), 16)
  const g = parseInt(hex.slice(3, 5), 16)
  const b = parseInt(hex.slice(5, 7), 16)
  const lr = Math.round(r + (255 - r) * amount)
  const lg = Math.round(g + (255 - g) * amount)
  const lb = Math.round(b + (255 - b) * amount)
  return `#${lr.toString(16).padStart(2, "0")}${lg.toString(16).padStart(2, "0")}${lb.toString(16).padStart(2, "0")}`
}

interface LinkTrafficChartProps {
  series: LinkTimeSeries[]
  height?: number
  title?: string
  linkColors?: Record<string, string>
  p95In?: number
  p95Out?: number
  hideLegend?: boolean
  timeBounds?: { from: number; to: number }
}

function getIntervalSeconds(series: LinkTimeSeries[]): number {
  let minDiff = Infinity
  for (const s of series) {
    for (let i = 1; i < s.points.length; i++) {
      const diff = (new Date(s.points[i].t).getTime() - new Date(s.points[i - 1].t).getTime()) / 1000
      if (diff > 0 && diff < minDiff) minDiff = diff
    }
  }
  return minDiff === Infinity ? 300 : minDiff
}

function formatTimeShort(ts: number, multiDay: boolean): string {
  if (multiDay) {
    const d = new Date(ts)
    return `${d.getDate()}/${d.getMonth() + 1} ${d.getHours().toString().padStart(2, "0")}:${d.getMinutes().toString().padStart(2, "0")}`
  }
  return new Date(ts).toLocaleString(undefined, { hour: "2-digit", minute: "2-digit" })
}

export function LinkTrafficChart({ series, height = 260, title, linkColors, p95In, p95Out, hideLegend, timeBounds }: LinkTrafficChartProps) {
  const { formatTraffic, formatAxis, unit } = useUnit()
  const chartColors = useChartColors()
  if (!series.length) return null
  const interval = getIntervalSeconds(series)
  const stepMs = interval * 1000
  const usePps = unit === "pps"

  // Detect if data spans multiple days
  let minTs = Infinity, maxTs = -Infinity
  for (const ls of series) {
    for (const pt of ls.points) {
      const t = new Date(pt.t).getTime()
      if (t < minTs) minTs = t
      if (t > maxTs) maxTs = t
    }
  }
  const multiDay = (maxTs - minTs) > 86400000

  const linkTags: string[] = []
  const linkLabels: Record<string, string> = {}
  const colors: Record<string, { in: string; out: string }> = {}

  for (let i = 0; i < series.length; i++) {
    const ls = series[i]
    linkTags.push(ls.link_tag)
    linkLabels[ls.link_tag] = ls.description || ls.link_tag
    const base = linkColors?.[ls.link_tag] || DEFAULT_COLORS[i % DEFAULT_COLORS.length]
    colors[ls.link_tag] = { in: base, out: lighten(base) }
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

  // Build data array from actual timestamps, inserting zeros for gaps
  const sortedTs = Array.from(dataByTs.keys()).sort((a, b) => a - b)
  const data: Record<string, unknown>[] = []

  // Boundary padding: add zero points at the start of the time range if data
  // begins late. Cheap (just 2 extra points) so it's safe for mobile.
  if (timeBounds && sortedTs.length > 0 && stepMs > 0) {
    const firstTs = sortedTs[0]
    if (firstTs > timeBounds.from + stepMs) {
      data.push({ time: formatTimeShort(timeBounds.from, multiDay), ...makeZeroRow() })
      data.push({ time: formatTimeShort(firstTs - stepMs, multiDay), ...makeZeroRow() })
    }
  }

  for (let i = 0; i < sortedTs.length; i++) {
    // Insert zero rows before a gap (gap > 2x step) so the chart drops to 0
    if (i > 0 && stepMs > 0 && (sortedTs[i] - sortedTs[i - 1]) > stepMs * 2) {
      data.push({ time: formatTimeShort(sortedTs[i - 1] + stepMs, multiDay), ...makeZeroRow() })
      data.push({ time: formatTimeShort(sortedTs[i] - stepMs, multiDay), ...makeZeroRow() })
    }
    const t = sortedTs[i]
    data.push({ time: formatTimeShort(t, multiDay), ...dataByTs.get(t) })
  }

  // Boundary padding at the end of the time range
  if (timeBounds && sortedTs.length > 0 && stepMs > 0) {
    const lastTs = sortedTs[sortedTs.length - 1]
    if (lastTs < timeBounds.to - stepMs) {
      data.push({ time: formatTimeShort(lastTs + stepMs, multiDay), ...makeZeroRow() })
      data.push({ time: formatTimeShort(timeBounds.to, multiDay), ...makeZeroRow() })
    }
  }

  const tickInterval = data.length > 0 ? Math.max(1, Math.floor(data.length / 8)) : 1

  return (
    <div className="animate-fade-in overflow-hidden">
      {title && (
        <h3 className="text-[10px] font-medium text-muted-foreground mb-2 uppercase tracking-wider">{title}</h3>
      )}
      <ResponsiveContainer width="100%" height={height}>
        <AreaChart data={data} margin={{ top: 2, right: 2, left: 0, bottom: 0 }}>
          {/* No gradients — solid fills like classic rrdtool */}
          <CartesianGrid strokeDasharray="3 3" stroke={chartColors.grid} opacity={0.5} />
          <XAxis
            dataKey="time"
            tick={{ fontSize: 8, fill: chartColors.text }}
            tickLine={{ stroke: chartColors.grid }}
            axisLine={{ stroke: chartColors.grid }}
            interval={tickInterval}
          />
          <YAxis
            tick={{ fontSize: 8, fill: chartColors.text }}
            tickLine={false}
            axisLine={false}
            tickFormatter={(v) => formatAxis(Math.abs(v), interval)}
            width={40}
          />
          <ReferenceLine y={0} stroke={chartColors.text} strokeWidth={1} />
          {p95In != null && p95In > 0 && (
            <ReferenceLine y={p95In} stroke="#e74c3c" strokeDasharray="4 2" strokeWidth={1} label={{ value: `p95 in: ${formatTraffic(p95In, interval)}`, position: "right", fontSize: 8, fill: "#e74c3c" }} />
          )}
          {p95Out != null && p95Out > 0 && (
            <ReferenceLine y={-p95Out} stroke="#e74c3c" strokeDasharray="4 2" strokeWidth={1} label={{ value: `p95 out: ${formatTraffic(p95Out, interval)}`, position: "right", fontSize: 8, fill: "#e74c3c" }} />
          )}
          <Tooltip
            cursor={{ stroke: "hsl(215 12% 50%)", strokeOpacity: 0.3 }}
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
                <div style={{ backgroundColor: chartColors.tooltipBg, border: `1px solid ${chartColors.tooltipBorder}`, borderRadius: 4, fontSize: 10, boxShadow: "0 4px 12px rgba(0,0,0,0.5)", padding: "5px 8px" }}>
                  <div style={{ color: chartColors.text, marginBottom: 3, fontSize: 9 }}>{label}</div>
                  {Array.from(byLink.entries()).map(([tag, { inVal, outVal }]) => {
                    if (inVal === 0 && outVal === 0) return null
                    return (
                      <div key={tag} style={{ display: "flex", alignItems: "center", gap: 4, lineHeight: 1.6, color: chartColors.tooltipText }}>
                        <span style={{ width: 6, height: 6, borderRadius: 1, backgroundColor: colors[tag]?.in || "#888", flexShrink: 0 }} />
                        <span style={{ fontSize: 9 }}>{linkLabels[tag] || tag}</span>
                        <span style={{ marginLeft: "auto", paddingLeft: 8, whiteSpace: "nowrap", fontSize: 9 }}>
                          {inVal > 0 && <>{"\u2193"}{formatTraffic(inVal, interval)}</>}
                          {inVal > 0 && outVal > 0 && " "}
                          {outVal > 0 && <>{"\u2191"}{formatTraffic(outVal, interval)}</>}
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
              strokeWidth={0.5}
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
              fillOpacity={0.7}
              strokeWidth={0.5}
              dot={false}
              isAnimationActive={false}
            />
          ))}
        </AreaChart>
      </ResponsiveContainer>
      {!hideLegend && (
        <div className="flex flex-wrap gap-x-3 gap-y-0.5 mt-1 px-1">
          {linkTags.map((tag) => (
            <div key={tag} className="flex items-center gap-1 text-[9px] text-muted-foreground">
              <span className="inline-block w-2 h-2 rounded-sm" style={{ backgroundColor: colors[tag].in }} />
              <span>{linkLabels[tag]}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
