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
  timeBounds?: { from: number; to: number }
}

function getIntervalSeconds(data: TrafficPoint[]): number {
  if (data.length < 2) return 300
  const t0 = new Date(data[0].t).getTime()
  const t1 = new Date(data[1].t).getTime()
  const diff = (t1 - t0) / 1000
  return diff > 0 ? diff : 300
}

function formatTime(ts: number): string {
  return new Date(ts).toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  })
}

export function TrafficChart({ data, height = 280, showLegend = true, title, timeBounds }: TrafficChartProps) {
  const { formatTraffic } = useUnit()
  const interval = getIntervalSeconds(data)

  // Build data with numeric timestamps; fill the full period range
  const stepMs = interval * 1000
  const dataByTs = new Map<number, { inbound: number; outbound: number }>()
  for (const d of data) {
    const ts = new Date(d.t).getTime()
    dataByTs.set(ts, { inbound: d.bytes_in, outbound: -(d.bytes_out || 0) })
  }

  // Fill empty time slots if bounds provided
  const formatted: { ts: number; time: string; inbound: number; outbound: number }[] = []
  if (timeBounds && stepMs > 0) {
    const start = Math.floor(timeBounds.from / stepMs) * stepMs
    for (let t = start; t <= timeBounds.to; t += stepMs) {
      const existing = dataByTs.get(t)
      formatted.push({
        ts: t,
        time: formatTime(t),
        inbound: existing?.inbound || 0,
        outbound: existing?.outbound || 0,
      })
    }
  } else {
    for (const d of data) {
      const ts = new Date(d.t).getTime()
      formatted.push({
        ts,
        time: formatTime(ts),
        inbound: d.bytes_in,
        outbound: -(d.bytes_out || 0),
      })
    }
  }

  return (
    <div className="animate-fade-in">
      {title && <h3 className="text-xs font-medium text-muted-foreground mb-3 uppercase tracking-wider">{title}</h3>}
      <ResponsiveContainer width="100%" height={height}>
        <BarChart data={formatted} margin={{ top: 4, right: 4, left: 0, bottom: 0 }} barCategoryGap="10%">
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
              backgroundColor: "hsl(220 18% 10%)",
              border: "1px solid hsl(220 15% 16%)",
              borderRadius: "0.375rem",
              fontSize: "10px",
              fontFamily: "inherit",
              boxShadow: "0 4px 12px rgba(0,0,0,0.5)",
              padding: "6px 10px",
            }}
            itemStyle={{ padding: 0 }}
            formatter={(value, name) => {
              const abs = Math.abs(Number(value))
              if (abs === 0) return null
              return [
                formatTraffic(abs, interval),
                name === "inbound" ? "In" : "Out",
              ]
            }}
            labelStyle={{ color: "hsl(215 12% 50%)", marginBottom: 2, fontSize: "10px" }}
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
            stackId="traffic"
            fill="var(--color-traffic-in)"
            fillOpacity={0.9}
          />
          <Bar
            dataKey="outbound"
            stackId="traffic"
            fill="var(--color-traffic-in)"
            fillOpacity={0.45}
          />
        </BarChart>
      </ResponsiveContainer>
    </div>
  )
}
