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
import { useUnit } from "@/hooks/useUnit"

interface TrafficChartProps {
  data: TrafficPoint[]
  height?: number
  showLegend?: boolean
  title?: string
}

function getIntervalSeconds(data: TrafficPoint[]): number {
  if (data.length < 2) return 300
  const t0 = new Date(data[0].t).getTime()
  const t1 = new Date(data[1].t).getTime()
  const diff = (t1 - t0) / 1000
  return diff > 0 ? diff : 300
}

export function TrafficChart({ data, height = 280, showLegend = true, title }: TrafficChartProps) {
  const { formatTraffic } = useUnit()
  const interval = getIntervalSeconds(data)

  const formatted = data.map(d => ({
    ...d,
    time: new Date(d.t).toLocaleString(undefined, {
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
    }),
  }))

  return (
    <div className="animate-fade-in">
      {title && <h3 className="text-xs font-medium text-muted-foreground mb-3 uppercase tracking-wider">{title}</h3>}
      <ResponsiveContainer width="100%" height={height}>
        <AreaChart data={formatted} margin={{ top: 4, right: 4, left: 0, bottom: 0 }}>
          <defs>
            <linearGradient id="gradIn" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor="var(--color-traffic-in)" stopOpacity={0.25} />
              <stop offset="100%" stopColor="var(--color-traffic-in)" stopOpacity={0} />
            </linearGradient>
            <linearGradient id="gradOut" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor="var(--color-traffic-out)" stopOpacity={0.25} />
              <stop offset="100%" stopColor="var(--color-traffic-out)" stopOpacity={0} />
            </linearGradient>
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
            width={64}
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
              name === "bytes_in" ? "Inbound" : "Outbound",
            ]}
            labelStyle={{ color: "var(--color-muted-foreground)", marginBottom: 4 }}
          />
          {showLegend && (
            <Legend
              formatter={(v) => (
                <span className="text-xs">{v === "bytes_in" ? "Inbound" : "Outbound"}</span>
              )}
              iconType="plainline"
              wrapperStyle={{ fontSize: 11, paddingTop: 8 }}
            />
          )}
          <Area
            type="monotone"
            dataKey="bytes_in"
            stroke="var(--color-traffic-in)"
            fill="url(#gradIn)"
            strokeWidth={1.5}
            dot={false}
            activeDot={{ r: 3, fill: "var(--color-traffic-in)" }}
          />
          <Area
            type="monotone"
            dataKey="bytes_out"
            stroke="var(--color-traffic-out)"
            fill="url(#gradOut)"
            strokeWidth={1.5}
            dot={false}
            activeDot={{ r: 3, fill: "var(--color-traffic-out)" }}
          />
        </AreaChart>
      </ResponsiveContainer>
    </div>
  )
}
