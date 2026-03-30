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
  timeBounds?: { from: number; to: number }
  p95In?: number
  p95Out?: number
  hideLegend?: boolean
}

function getIntervalSeconds(series: LinkTimeSeries[]): number {
  for (const s of series) {
    if (s.points.length >= 2) {
      const diff = (new Date(s.points[1].t).getTime() - new Date(s.points[0].t).getTime()) / 1000
      if (diff > 0) return diff
    }
  }
  return 300
}

function formatTimeShort(ts: number): string {
  return new Date(ts).toLocaleString(undefined, { hour: "2-digit", minute: "2-digit" })
}

export function LinkTrafficChart({ series, height = 260, title, linkColors, timeBounds, p95In, p95Out, hideLegend }: LinkTrafficChartProps) {
  const { formatTraffic, formatAxis, unit } = useUnit()
  if (!series.length) return null
  const interval = getIntervalSeconds(series)
  const stepMs = interval * 1000
  const usePps = unit === "pps"

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

  const dataByTs = new Map<number, Record<string, number>>()
  for (const ls of series) {
    for (const pt of ls.points) {
      const ts = new Date(pt.t).getTime()
      if (!dataByTs.has(ts)) dataByTs.set(ts, {})
      const row = dataByTs.get(ts)!
      row[`${ls.link_tag}_in`] = usePps ? (pt.packets_in || 0) : (pt.bytes_in || 0)
      row[`${ls.link_tag}_out`] = -(usePps ? (pt.packets_out || 0) : (pt.bytes_out || 0))
    }
  }

  const data: Record<string, unknown>[] = []
  if (timeBounds && stepMs > 0) {
    const maxSlots = 300
    let fillStep = stepMs
    const totalSlots = Math.ceil((timeBounds.to - timeBounds.from) / stepMs)
    if (totalSlots > maxSlots) {
      fillStep = Math.ceil((timeBounds.to - timeBounds.from) / maxSlots / stepMs) * stepMs
    }
    const start = Math.floor(timeBounds.from / fillStep) * fillStep
    for (let t = start; t <= timeBounds.to; t += fillStep) {
      const row: Record<string, number> = {}
      for (let s = t; s < t + fillStep && s <= timeBounds.to; s += stepMs) {
        const existing = dataByTs.get(s)
        if (existing) {
          for (const [k, v] of Object.entries(existing)) {
            row[k] = (row[k] || 0) + v
          }
        }
      }
      data.push({ time: formatTimeShort(t), ...row })
    }
  } else {
    for (const [t, vals] of Array.from(dataByTs.entries()).sort(([a], [b]) => a - b)) {
      data.push({ time: formatTimeShort(t), ...vals })
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
          <CartesianGrid strokeDasharray="3 3" stroke="hsl(220 15% 85%)" opacity={0.5} />
          <XAxis
            dataKey="time"
            tick={{ fontSize: 8, fill: "hsl(215 12% 50%)" }}
            tickLine={{ stroke: "hsl(220 15% 16%)" }}
            axisLine={{ stroke: "hsl(220 15% 16%)" }}
            interval={tickInterval}
          />
          <YAxis
            tick={{ fontSize: 8, fill: "hsl(215 12% 50%)" }}
            tickLine={false}
            axisLine={false}
            tickFormatter={(v) => formatAxis(Math.abs(v), interval)}
            width={40}
          />
          <ReferenceLine y={0} stroke="hsl(215 12% 40%)" strokeWidth={1} />
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
                <div style={{ backgroundColor: "hsl(220 18% 10%)", border: "1px solid hsl(220 15% 20%)", borderRadius: 4, fontSize: 10, boxShadow: "0 4px 12px rgba(0,0,0,0.5)", padding: "5px 8px" }}>
                  <div style={{ color: "hsl(215 12% 50%)", marginBottom: 3, fontSize: 9 }}>{label}</div>
                  {Array.from(byLink.entries()).map(([tag, { inVal, outVal }]) => {
                    if (inVal === 0 && outVal === 0) return null
                    return (
                      <div key={tag} style={{ display: "flex", alignItems: "center", gap: 4, lineHeight: 1.6, color: "hsl(210 20% 88%)" }}>
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
