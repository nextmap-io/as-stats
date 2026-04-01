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

const AS_COLORS = [
  "#e74c3c", "#3498db", "#2ecc71", "#f39c12", "#9b59b6",
  "#1abc9c", "#e67e22", "#2980b9", "#e91e63", "#00bcd4",
]

function lighten(hex: string, amount = 0.35): string {
  const r = parseInt(hex.slice(1, 3), 16)
  const g = parseInt(hex.slice(3, 5), 16)
  const b = parseInt(hex.slice(5, 7), 16)
  return `#${Math.round(r + (255 - r) * amount).toString(16).padStart(2, "0")}${Math.round(g + (255 - g) * amount).toString(16).padStart(2, "0")}${Math.round(b + (255 - b) * amount).toString(16).padStart(2, "0")}`
}

interface ASTrafficChartProps {
  data: ASTrafficDetail[]
  height?: number
  title?: string
}

function formatTimeShort(ts: number, multiDay: boolean): string {
  if (multiDay) {
    const d = new Date(ts)
    return `${d.getDate()}/${d.getMonth() + 1} ${d.getHours().toString().padStart(2, "0")}:${d.getMinutes().toString().padStart(2, "0")}`
  }
  return new Date(ts).toLocaleString(undefined, { hour: "2-digit", minute: "2-digit" })
}

// Color assignment is done internally and exposed via the static array
// Use getASColorsFromData in the parent component


export function ASTrafficChart({ data, height = 300, title }: ASTrafficChartProps) {
  const { formatTraffic, formatAxis } = useUnit()
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
  const interval = minDiff === Infinity ? 300 : minDiff
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
    const base = AS_COLORS[i % AS_COLORS.length]
    colors[d.as_number] = { in: base, out: lighten(base) }
  })

  // Pivot: collect all timestamps → per-AS in/out
  const dataByTs = new Map<number, Record<string, number>>()
  for (const as of data) {
    for (const s of as.series) {
      for (const pt of s.points) {
        const ts = new Date(pt.t).getTime()
        if (!dataByTs.has(ts)) dataByTs.set(ts, {})
        const row = dataByTs.get(ts)!
        row[`${as.as_number}_in`] = (row[`${as.as_number}_in`] || 0) + (pt.bytes_in || 0)
        row[`${as.as_number}_out`] = (row[`${as.as_number}_out`] || 0) - (pt.bytes_out || 0)
      }
    }
  }

  // Build sorted data with gap fill
  const sortedTs = Array.from(dataByTs.keys()).sort((a, b) => a - b)
  const chartData: Record<string, unknown>[] = []
  for (let i = 0; i < sortedTs.length; i++) {
    if (i > 0 && stepMs > 0 && (sortedTs[i] - sortedTs[i - 1]) > stepMs * 2) {
      chartData.push({ time: formatTimeShort(sortedTs[i - 1] + stepMs, multiDay) })
      chartData.push({ time: formatTimeShort(sortedTs[i] - stepMs, multiDay) })
    }
    chartData.push({ time: formatTimeShort(sortedTs[i], multiDay), ...dataByTs.get(sortedTs[i]) })
  }

  const tickInterval = chartData.length > 0 ? Math.max(1, Math.floor(chartData.length / 8)) : 1

  return (
    <div className="animate-fade-in overflow-hidden">
      {title && <h3 className="text-[10px] font-medium text-muted-foreground mb-2 uppercase tracking-wider">{title}</h3>}
      <ResponsiveContainer width="100%" height={height}>
        <AreaChart data={chartData} margin={{ top: 2, right: 2, left: 0, bottom: 0 }}>
          <CartesianGrid strokeDasharray="3 3" stroke="hsl(220 15% 85%)" opacity={0.5} />
          <XAxis dataKey="time" tick={{ fontSize: 8, fill: "hsl(215 12% 50%)" }} tickLine={{ stroke: "hsl(220 15% 16%)" }} axisLine={{ stroke: "hsl(220 15% 16%)" }} interval={tickInterval} />
          <YAxis tick={{ fontSize: 8, fill: "hsl(215 12% 50%)" }} tickLine={false} axisLine={false} tickFormatter={(v) => formatAxis(Math.abs(v), interval)} width={40} />
          <ReferenceLine y={0} stroke="hsl(215 12% 40%)" strokeWidth={1} />
          <Tooltip
            cursor={{ stroke: "hsl(215 12% 50%)", strokeOpacity: 0.3 }}
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
                <div style={{ backgroundColor: "hsl(220 18% 10%)", border: "1px solid hsl(220 15% 20%)", borderRadius: 4, fontSize: 10, boxShadow: "0 4px 12px rgba(0,0,0,0.5)", padding: "5px 8px", maxWidth: 300 }}>
                  <div style={{ color: "hsl(215 12% 50%)", marginBottom: 3, fontSize: 9 }}>{label}</div>
                  {Array.from(byAS.entries()).map(([asn, { inVal, outVal }]) => {
                    if (inVal === 0 && outVal === 0) return null
                    return (
                      <div key={asn} style={{ display: "flex", alignItems: "center", gap: 4, lineHeight: 1.6, color: "hsl(210 20% 88%)" }}>
                        <span style={{ width: 6, height: 6, borderRadius: 1, backgroundColor: colors[asn]?.in || "#888", flexShrink: 0 }} />
                        <span style={{ fontSize: 9, flex: 1, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{asLabels[asn]}</span>
                        <span style={{ whiteSpace: "nowrap", fontSize: 9 }}>
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
          {asKeys.map((asn) => (
            <Area key={`${asn}_in`} type="stepAfter" dataKey={`${asn}_in`} stackId="up" stroke={colors[asn].in} fill={colors[asn].in} fillOpacity={0.85} strokeWidth={0.5} dot={false} isAnimationActive={false} />
          ))}
          {asKeys.map((asn) => (
            <Area key={`${asn}_out`} type="stepAfter" dataKey={`${asn}_out`} stackId="down" stroke={colors[asn].out} fill={colors[asn].out} fillOpacity={0.7} strokeWidth={0.5} dot={false} isAnimationActive={false} />
          ))}
        </AreaChart>
      </ResponsiveContainer>
    </div>
  )
}
