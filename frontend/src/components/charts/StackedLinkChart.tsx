import {
  AreaChart,
  Area,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
  Legend,
} from "recharts"
import type { TrafficPoint } from "@/lib/types"
import { formatBytes } from "@/lib/utils"

// Default palette for links without assigned colors
const DEFAULT_COLORS = [
  "#10b981", "#f59e0b", "#3b82f6", "#ef4444", "#8b5cf6",
  "#06b6d4", "#ec4899", "#84cc16", "#f97316", "#6366f1",
]

interface LinkSeries {
  tag: string
  color?: string
  data: TrafficPoint[]
}

interface StackedLinkChartProps {
  series: LinkSeries[]
  height?: number
  direction?: "in" | "out"
}

export function StackedLinkChart({ series, height = 300, direction = "in" }: StackedLinkChartProps) {
  if (series.length === 0) return null

  const dataKey = direction === "in" ? "bytes_in" : "bytes_out"

  // Build merged time series: each timestamp has one key per link
  const timeMap = new Map<string, Record<string, number>>()

  for (const s of series) {
    for (const pt of s.data) {
      const t = pt.t
      if (!timeMap.has(t)) {
        timeMap.set(t, {})
      }
      const row = timeMap.get(t)!
      row[s.tag] = direction === "in" ? pt.bytes_in : pt.bytes_out
    }
  }

  const merged = Array.from(timeMap.entries())
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([t, values]) => ({
      time: new Date(t).toLocaleString(undefined, {
        month: "short",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
      }),
      ...values,
    }))

  return (
    <ResponsiveContainer width="100%" height={height}>
      <AreaChart data={merged} margin={{ top: 4, right: 4, left: 0, bottom: 0 }}>
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
          tickFormatter={(v) => formatBytes(v)}
          width={56}
        />
        <Tooltip
          contentStyle={{
            backgroundColor: "var(--color-card)",
            border: "1px solid var(--color-border)",
            borderRadius: "var(--radius)",
            fontSize: "11px",
            fontFamily: "inherit",
            boxShadow: "0 4px 12px rgba(0,0,0,0.3)",
          }}
          formatter={(value, name) => [formatBytes(Number(value)), name]}
          labelStyle={{ color: "var(--color-muted-foreground)", marginBottom: 4 }}
        />
        <Legend
          iconType="plainline"
          wrapperStyle={{ fontSize: 11, paddingTop: 8 }}
        />
        {series.map((s, i) => {
          const color = s.color || DEFAULT_COLORS[i % DEFAULT_COLORS.length]
          return (
            <Area
              key={s.tag}
              type="monotone"
              dataKey={s.tag}
              stackId="1"
              stroke={color}
              fill={color}
              fillOpacity={0.3}
              strokeWidth={1.5}
              dot={false}
            />
          )
        })}
      </AreaChart>
    </ResponsiveContainer>
  )
}
