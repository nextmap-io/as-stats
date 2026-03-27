import {
  BarChart,
  Bar,
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

function formatTime(ts: number): string {
  return new Date(ts).toLocaleString(undefined, {
    month: "short", day: "numeric", hour: "2-digit", minute: "2-digit",
  })
}

export function LinkTrafficChart({ series, height = 260, title, linkColors, timeBounds }: LinkTrafficChartProps) {
  const { formatTraffic } = useUnit()
  if (!series.length) return null
  const interval = getIntervalSeconds(series)
  const stepMs = interval * 1000

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

  // Build data indexed by timestamp
  const dataByTs = new Map<number, Record<string, number>>()
  for (const ls of series) {
    for (const pt of ls.points) {
      const ts = new Date(pt.t).getTime()
      if (!dataByTs.has(ts)) dataByTs.set(ts, {})
      const row = dataByTs.get(ts)!
      row[`${ls.link_tag}_in`] = pt.bytes_in || 0
      row[`${ls.link_tag}_out`] = -(pt.bytes_out || 0)
    }
  }

  // Fill full time range
  const data: Record<string, unknown>[] = []
  if (timeBounds && stepMs > 0) {
    const start = Math.floor(timeBounds.from / stepMs) * stepMs
    for (let t = start; t <= timeBounds.to; t += stepMs) {
      data.push({ time: formatTime(t), ...(dataByTs.get(t) || {}) })
    }
  } else {
    for (const [t, vals] of Array.from(dataByTs.entries()).sort(([a], [b]) => a - b)) {
      data.push({ time: formatTime(t), ...vals })
    }
  }

  return (
    <div className="animate-fade-in">
      {title && (
        <h3 className="text-xs font-medium text-muted-foreground mb-2 uppercase tracking-wider">{title}</h3>
      )}
      <ResponsiveContainer width="100%" height={height}>
        <BarChart data={data} margin={{ top: 4, right: 4, left: 0, bottom: 0 }} barCategoryGap="15%">
          <CartesianGrid strokeDasharray="3 3" stroke="hsl(220 15% 16%)" opacity={0.5} />
          <XAxis
            dataKey="time"
            tick={{ fontSize: 9, fill: "hsl(215 12% 50%)" }}
            tickLine={false}
            axisLine={{ stroke: "hsl(220 15% 16%)" }}
            interval="preserveStartEnd"
          />
          <YAxis
            tick={{ fontSize: 9, fill: "hsl(215 12% 50%)" }}
            tickLine={false}
            axisLine={false}
            tickFormatter={(v) => formatTraffic(Math.abs(v), interval)}
            width={60}
          />
          <ReferenceLine y={0} stroke="hsl(215 12% 50%)" strokeOpacity={0.5} />
          <Tooltip
            contentStyle={{
              backgroundColor: "hsl(220 18% 10%)",
              border: "1px solid hsl(220 15% 16%)",
              borderRadius: "0.375rem",
              fontSize: "10px",
              boxShadow: "0 4px 12px rgba(0,0,0,0.5)",
              padding: "6px 10px",
            }}
            itemStyle={{ padding: 0, color: "hsl(210 20% 88%)" }}
            formatter={(value, name) => {
              const abs = Math.abs(Number(value))
              if (abs === 0) return [null, null]
              const tag = String(name).replace(/_in$|_out$/, "")
              const dir = String(name).endsWith("_in") ? "\u2193" : "\u2191"
              return [formatTraffic(abs, interval), `${dir} ${linkLabels[tag] || tag}`]
            }}
            labelStyle={{ color: "hsl(215 12% 50%)", marginBottom: 2, fontSize: "10px" }}
          />
          {/* Separate stackIds so positive stacks up, negative stacks down */}
          {linkTags.map((tag) => (
            <Bar key={`${tag}_in`} dataKey={`${tag}_in`} stackId="in" fill={colors[tag].in} fillOpacity={0.9} />
          ))}
          {linkTags.map((tag) => (
            <Bar key={`${tag}_out`} dataKey={`${tag}_out`} stackId="out" fill={colors[tag].out} fillOpacity={0.9} />
          ))}
        </BarChart>
      </ResponsiveContainer>
      {/* Custom legend — one line per link */}
      <div className="flex flex-wrap gap-x-4 gap-y-1 mt-2 px-1">
        {linkTags.map((tag) => (
          <div key={tag} className="flex items-center gap-1.5 text-[10px] text-muted-foreground">
            <span className="inline-block w-2.5 h-2.5 rounded-sm" style={{ backgroundColor: colors[tag].in }} />
            <span>{linkLabels[tag]}</span>
          </div>
        ))}
      </div>
    </div>
  )
}
