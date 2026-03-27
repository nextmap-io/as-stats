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

  // Transform: inbound positive, outbound negative
  const formatted = data.map(d => ({
    time: new Date(d.t).toLocaleString(undefined, {
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
    }),
    inbound: d.bytes_in,
    outbound: -(d.bytes_out || 0),
  }))

  return (
    <div className="animate-fade-in">
      {title && <h3 className="text-xs font-medium text-muted-foreground mb-3 uppercase tracking-wider">{title}</h3>}
      <ResponsiveContainer width="100%" height={height}>
        <BarChart data={formatted} margin={{ top: 4, right: 4, left: 0, bottom: 0 }} barGap={0} barCategoryGap="10%">
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
          <ReferenceLine y={0} stroke="var(--color-border)" />
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
              formatTraffic(Math.abs(Number(value)), interval),
              name === "inbound" ? "Inbound" : "Outbound",
            ]}
            labelStyle={{ color: "var(--color-muted-foreground)", marginBottom: 4 }}
          />
          {showLegend && (
            <Legend
              formatter={(v) => (
                <span className="text-xs">{v === "inbound" ? "Inbound" : "Outbound"}</span>
              )}
              iconType="square"
              wrapperStyle={{ fontSize: 11, paddingTop: 8 }}
            />
          )}
          <Bar
            dataKey="inbound"
            fill="var(--color-traffic-in)"
            fillOpacity={0.8}
          />
          <Bar
            dataKey="outbound"
            fill="var(--color-traffic-out)"
            fillOpacity={0.8}
          />
        </BarChart>
      </ResponsiveContainer>
    </div>
  )
}
