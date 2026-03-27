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
    month: "short", day: "numeric", hour: "2-digit", minute: "2-digit",
  })
}

export function TrafficChart({ data, height = 280, showLegend = true, title, timeBounds }: TrafficChartProps) {
  const { formatTraffic } = useUnit()
  const interval = getIntervalSeconds(data)
  const stepMs = interval * 1000

  const dataByTs = new Map<number, { inbound: number; outbound: number }>()
  for (const d of data) {
    const ts = new Date(d.t).getTime()
    dataByTs.set(ts, { inbound: d.bytes_in, outbound: -(d.bytes_out || 0) })
  }

  const formatted: { time: string; inbound: number; outbound: number }[] = []
  if (timeBounds && stepMs > 0) {
    const start = Math.floor(timeBounds.from / stepMs) * stepMs
    for (let t = start; t <= timeBounds.to; t += stepMs) {
      const existing = dataByTs.get(t)
      formatted.push({ time: formatTime(t), inbound: existing?.inbound || 0, outbound: existing?.outbound || 0 })
    }
  } else {
    for (const [t, vals] of Array.from(dataByTs.entries()).sort(([a], [b]) => a - b)) {
      formatted.push({ time: formatTime(t), ...vals })
    }
  }

  return (
    <div className="animate-fade-in">
      {title && <h3 className="text-xs font-medium text-muted-foreground mb-2 uppercase tracking-wider">{title}</h3>}
      <ResponsiveContainer width="100%" height={height}>
        <BarChart data={formatted} margin={{ top: 4, right: 4, left: 0, bottom: 0 }} barCategoryGap="10%">
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
              return [formatTraffic(abs, interval), name === "inbound" ? "\u2193 In" : "\u2191 Out"]
            }}
            labelStyle={{ color: "hsl(215 12% 50%)", marginBottom: 2, fontSize: "10px" }}
          />
          <Bar dataKey="inbound" fill="hsl(174 72% 46%)" fillOpacity={0.9} />
          <Bar dataKey="outbound" fill="hsl(174 72% 46%)" fillOpacity={0.4} />
        </BarChart>
      </ResponsiveContainer>
      {showLegend && (
        <div className="flex gap-4 mt-2 px-1 text-[10px] text-muted-foreground">
          <div className="flex items-center gap-1.5">
            <span className="inline-block w-2.5 h-2.5 rounded-sm" style={{ backgroundColor: "hsl(174 72% 46%)" }} />
            <span>{"\u2193"} In</span>
          </div>
          <div className="flex items-center gap-1.5">
            <span className="inline-block w-2.5 h-2.5 rounded-sm" style={{ backgroundColor: "hsl(174 72% 46%)", opacity: 0.4 }} />
            <span>{"\u2191"} Out</span>
          </div>
        </div>
      )}
    </div>
  )
}
