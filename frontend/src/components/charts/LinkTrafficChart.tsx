import {
  BarChart,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
  Legend,
  ReferenceLine,
} from "recharts"
import type { LinkTimeSeries } from "@/lib/types"
import { useUnit } from "@/hooks/useUnit"

// Default link colors — each link gets a base hue, out = lighter version
const DEFAULT_COLORS = [
  "#e74c3c", // red
  "#3498db", // blue
  "#2ecc71", // green
  "#f39c12", // orange
  "#9b59b6", // purple
  "#1abc9c", // teal
  "#e67e22", // dark orange
  "#2980b9", // dark blue
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

export function LinkTrafficChart({ series, height = 260, title, linkColors }: LinkTrafficChartProps) {
  const { formatTraffic } = useUnit()
  if (!series.length) return null
  const interval = getIntervalSeconds(series)

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

  // Build unified time axis: per link, in (positive) and out (negative)
  const timeMap = new Map<string, Record<string, number>>()

  for (const ls of series) {
    for (const pt of ls.points) {
      const key = pt.t
      if (!timeMap.has(key)) timeMap.set(key, {})
      const row = timeMap.get(key)!
      row[`${ls.link_tag}_in`] = pt.bytes_in || 0
      row[`${ls.link_tag}_out`] = -(pt.bytes_out || 0)
    }
  }

  const data = Array.from(timeMap.entries())
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([t, vals]) => ({
      time: new Date(t).toLocaleString(undefined, {
        month: "short",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
      }),
      ...vals,
    }))

  return (
    <div className="animate-fade-in">
      {title && (
        <h3 className="text-xs font-medium text-muted-foreground mb-3 uppercase tracking-wider">
          {title}
        </h3>
      )}
      <ResponsiveContainer width="100%" height={height}>
        <BarChart data={data} margin={{ top: 4, right: 4, left: 0, bottom: 0 }} barCategoryGap="15%">
          <CartesianGrid strokeDasharray="3 3" stroke="var(--color-border)" opacity={0.5} />
          <XAxis
            dataKey="time"
            tick={{ fontSize: 10, fill: "var(--color-muted-foreground)" }}
            tickLine={false}
            axisLine={{ stroke: "var(--color-border)" }}
            interval="preserveStartEnd"
          />
          <YAxis
            tick={{ fontSize: 10, fill: "var(--color-muted-foreground)" }}
            tickLine={false}
            axisLine={false}
            tickFormatter={(v) => formatTraffic(Math.abs(v), interval)}
            width={64}
          />
          <ReferenceLine y={0} stroke="var(--color-muted-foreground)" strokeOpacity={0.5} />
          <Tooltip
            contentStyle={{
              backgroundColor: "var(--color-card)",
              border: "1px solid var(--color-border)",
              borderRadius: "var(--radius)",
              fontSize: "11px",
              fontFamily: "inherit",
              boxShadow: "0 4px 12px rgba(0,0,0,0.3)",
            }}
            formatter={(value, name) => {
              const tag = String(name).replace(/_in$|_out$/, "")
              const dir = String(name).endsWith("_in") ? "In" : "Out"
              return [
                formatTraffic(Math.abs(Number(value)), interval),
                `${linkLabels[tag] || tag} ${dir}`,
              ]
            }}
            labelStyle={{ color: "var(--color-muted-foreground)", marginBottom: 4 }}
          />
          <Legend
            formatter={(v) => {
              const tag = String(v).replace(/_in$|_out$/, "")
              if (String(v).endsWith("_out")) return null
              return <span className="text-xs">{linkLabels[tag] || tag}</span>
            }}
            iconType="square"
            wrapperStyle={{ fontSize: 11, paddingTop: 8 }}
          />
          {/* All bars share one stackId — positive values stack up, negative stack down */}
          {linkTags.map((tag) => (
            <Bar
              key={`${tag}_in`}
              dataKey={`${tag}_in`}
              stackId="traffic"
              fill={colors[tag].in}
              fillOpacity={0.9}
            />
          ))}
          {linkTags.map((tag) => (
            <Bar
              key={`${tag}_out`}
              dataKey={`${tag}_out`}
              stackId="traffic"
              fill={colors[tag].out}
              fillOpacity={0.9}
              legendType="none"
            />
          ))}
        </BarChart>
      </ResponsiveContainer>
    </div>
  )
}
