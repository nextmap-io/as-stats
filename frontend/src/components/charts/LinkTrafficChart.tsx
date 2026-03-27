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
import type { LinkTimeSeries } from "@/lib/types"
import { useUnit } from "@/hooks/useUnit"

const COLORS = [
  "hsl(174 72% 46%)",  // teal
  "hsl(36 100% 55%)",  // amber
  "hsl(210 80% 56%)",  // blue
  "hsl(330 70% 60%)",  // pink
  "hsl(270 60% 62%)",  // purple
  "hsl(152 60% 44%)",  // green
  "hsl(15 85% 55%)",   // orange
  "hsl(195 75% 50%)",  // cyan
]

interface LinkTrafficChartProps {
  series: LinkTimeSeries[]
  height?: number
  title?: string
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

export function LinkTrafficChart({ series, height = 260, title }: LinkTrafficChartProps) {
  const { formatTraffic } = useUnit()
  if (!series.length) return null
  const interval = getIntervalSeconds(series)

  // Build a unified time axis with one key per link
  const timeMap = new Map<string, Record<string, number>>()
  const linkTags: string[] = []
  const linkLabels: Record<string, string> = {}

  for (const ls of series) {
    linkTags.push(ls.link_tag)
    linkLabels[ls.link_tag] = ls.description || ls.link_tag
    for (const pt of ls.points) {
      const key = pt.t
      if (!timeMap.has(key)) timeMap.set(key, {})
      const row = timeMap.get(key)!
      row[ls.link_tag] = (pt.bytes_in || 0) + (pt.bytes_out || 0)
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
        <AreaChart data={data} margin={{ top: 4, right: 4, left: 0, bottom: 0 }}>
          <defs>
            {linkTags.map((tag, i) => (
              <linearGradient key={tag} id={`grad-${tag}`} x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor={COLORS[i % COLORS.length]} stopOpacity={0.3} />
                <stop offset="100%" stopColor={COLORS[i % COLORS.length]} stopOpacity={0} />
              </linearGradient>
            ))}
          </defs>
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
            tickFormatter={(v) => formatTraffic(v, interval)}
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
            formatter={(value, name) => [
              formatTraffic(Number(value), interval),
              linkLabels[String(name)] || String(name),
            ]}
            labelStyle={{ color: "var(--color-muted-foreground)", marginBottom: 4 }}
          />
          <Legend
            formatter={(v) => (
              <span className="text-xs">{linkLabels[String(v)] || String(v)}</span>
            )}
            iconType="plainline"
            wrapperStyle={{ fontSize: 11, paddingTop: 8 }}
          />
          {linkTags.map((tag, i) => (
            <Area
              key={tag}
              type="monotone"
              dataKey={tag}
              stackId="1"
              stroke={COLORS[i % COLORS.length]}
              fill={`url(#grad-${tag})`}
              strokeWidth={1.5}
              dot={false}
              activeDot={{ r: 3, fill: COLORS[i % COLORS.length] }}
            />
          ))}
        </AreaChart>
      </ResponsiveContainer>
    </div>
  )
}
