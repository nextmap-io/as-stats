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

interface TrafficChartProps {
  data: TrafficPoint[]
  height?: number
  showLegend?: boolean
  title?: string
}

export function TrafficChart({ data, height = 300, showLegend = true, title }: TrafficChartProps) {
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
    <div>
      {title && <h3 className="text-sm font-medium text-muted-foreground mb-2">{title}</h3>}
      <ResponsiveContainer width="100%" height={height}>
        <AreaChart data={formatted} margin={{ top: 5, right: 10, left: 0, bottom: 0 }}>
          <defs>
            <linearGradient id="gradIn" x1="0" y1="0" x2="0" y2="1">
              <stop offset="5%" stopColor="hsl(173, 58%, 39%)" stopOpacity={0.3} />
              <stop offset="95%" stopColor="hsl(173, 58%, 39%)" stopOpacity={0} />
            </linearGradient>
            <linearGradient id="gradOut" x1="0" y1="0" x2="0" y2="1">
              <stop offset="5%" stopColor="hsl(12, 76%, 61%)" stopOpacity={0.3} />
              <stop offset="95%" stopColor="hsl(12, 76%, 61%)" stopOpacity={0} />
            </linearGradient>
          </defs>
          <CartesianGrid strokeDasharray="3 3" className="stroke-border" />
          <XAxis
            dataKey="time"
            tick={{ fontSize: 11 }}
            className="text-muted-foreground"
            interval="preserveStartEnd"
          />
          <YAxis
            tick={{ fontSize: 11 }}
            className="text-muted-foreground"
            tickFormatter={(v) => formatBytes(v)}
            width={70}
          />
          <Tooltip
            contentStyle={{
              backgroundColor: "hsl(var(--color-card))",
              border: "1px solid hsl(var(--color-border))",
              borderRadius: "0.375rem",
              fontSize: "12px",
            }}
            formatter={(value, name) => [
              formatBytes(Number(value)),
              name === "bytes_in" ? "Inbound" : "Outbound",
            ]}
          />
          {showLegend && <Legend formatter={(v) => (v === "bytes_in" ? "Inbound" : "Outbound")} />}
          <Area
            type="monotone"
            dataKey="bytes_in"
            stroke="hsl(173, 58%, 39%)"
            fill="url(#gradIn)"
            strokeWidth={2}
          />
          <Area
            type="monotone"
            dataKey="bytes_out"
            stroke="hsl(12, 76%, 61%)"
            fill="url(#gradOut)"
            strokeWidth={2}
          />
        </AreaChart>
      </ResponsiveContainer>
    </div>
  )
}
