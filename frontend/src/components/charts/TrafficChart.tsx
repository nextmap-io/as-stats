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
import type { TrafficPoint } from "@/lib/types"
import { useUnit } from "@/hooks/useUnit"

interface TrafficChartProps {
  data: TrafficPoint[]
  height?: number
  showLegend?: boolean
  title?: string
  timeBounds?: { from: number; to: number }
  p95In?: number
  p95Out?: number
}

function getIntervalSeconds(data: TrafficPoint[]): number {
  if (data.length < 2) return 300
  const t0 = new Date(data[0].t).getTime()
  const t1 = new Date(data[1].t).getTime()
  const diff = (t1 - t0) / 1000
  return diff > 0 ? diff : 300
}

function formatTimeShort(ts: number): string {
  return new Date(ts).toLocaleString(undefined, { hour: "2-digit", minute: "2-digit" })
}

export function TrafficChart({ data, height = 280, showLegend = true, title, timeBounds, p95In, p95Out }: TrafficChartProps) {
  const { formatTraffic, formatAxis, unit } = useUnit()
  const interval = getIntervalSeconds(data)
  const stepMs = interval * 1000
  const usePps = unit === "pps"

  const dataByTs = new Map<number, { inbound: number; outbound: number }>()
  for (const d of data) {
    const ts = new Date(d.t).getTime()
    const inVal = usePps ? (d.packets_in || 0) : d.bytes_in
    const outVal = usePps ? (d.packets_out || 0) : (d.bytes_out || 0)
    dataByTs.set(ts, { inbound: inVal, outbound: -outVal })
  }

  const formatted: { time: string; inbound: number; outbound: number }[] = []
  if (timeBounds && stepMs > 0) {
    const maxSlots = 300
    let fillStep = stepMs
    const totalSlots = Math.ceil((timeBounds.to - timeBounds.from) / stepMs)
    if (totalSlots > maxSlots) {
      fillStep = Math.ceil((timeBounds.to - timeBounds.from) / maxSlots / stepMs) * stepMs
    }
    const start = Math.floor(timeBounds.from / fillStep) * fillStep
    for (let t = start; t <= timeBounds.to; t += fillStep) {
      let inbound = 0, outbound = 0
      for (let s = t; s < t + fillStep && s <= timeBounds.to; s += stepMs) {
        const existing = dataByTs.get(s)
        if (existing) { inbound += existing.inbound; outbound += existing.outbound }
      }
      formatted.push({ time: formatTimeShort(t), inbound, outbound })
    }
  } else {
    for (const [t, vals] of Array.from(dataByTs.entries()).sort(([a], [b]) => a - b)) {
      formatted.push({ time: formatTimeShort(t), ...vals })
    }
  }

  const tickInterval = formatted.length > 0 ? Math.max(1, Math.floor(formatted.length / 8)) : 1

  return (
    <div className="animate-fade-in overflow-hidden">
      {title && <h3 className="text-[10px] font-medium text-muted-foreground mb-1 uppercase tracking-wider">{title}</h3>}
      <ResponsiveContainer width="100%" height={height}>
        <AreaChart data={formatted} margin={{ top: 2, right: 2, left: 0, bottom: 0 }}>
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
            <ReferenceLine y={p95In} stroke="#e74c3c" strokeDasharray="4 2" strokeWidth={1} label={{ value: `p95: ${formatTraffic(p95In, interval)}`, position: "right", fontSize: 8, fill: "#e74c3c" }} />
          )}
          {p95Out != null && p95Out > 0 && (
            <ReferenceLine y={-p95Out} stroke="#e74c3c" strokeDasharray="4 2" strokeWidth={1} label={{ value: `p95: ${formatTraffic(p95Out, interval)}`, position: "right", fontSize: 8, fill: "#e74c3c" }} />
          )}
          <Tooltip
            cursor={{ stroke: "hsl(215 12% 50%)", strokeOpacity: 0.3 }}
            contentStyle={{
              backgroundColor: "hsl(220 18% 10%)",
              border: "1px solid hsl(220 15% 20%)",
              borderRadius: 4,
              fontSize: 10,
              boxShadow: "0 4px 12px rgba(0,0,0,0.5)",
              padding: "5px 8px",
            }}
            itemStyle={{ padding: 0, color: "hsl(210 20% 88%)" }}
            formatter={(value, name) => {
              const abs = Math.abs(Number(value))
              if (abs === 0) return [null, null]
              return [formatTraffic(abs, interval), name === "inbound" ? "\u2193 In" : "\u2191 Out"]
            }}
            labelStyle={{ color: "hsl(215 12% 50%)", marginBottom: 2, fontSize: 9 }}
          />
          <Area
            type="stepAfter"
            dataKey="inbound"
            stroke="hsl(174 72% 46%)"
            fill="hsl(174 72% 46%)"
            fillOpacity={0.85}
            strokeWidth={0.5}
            dot={false}
            isAnimationActive={false}
          />
          <Area
            type="stepAfter"
            dataKey="outbound"
            stroke="hsl(174 60% 56%)"
            fill="hsl(174 60% 56%)"
            fillOpacity={0.65}
            strokeWidth={0.5}
            dot={false}
            isAnimationActive={false}
          />
        </AreaChart>
      </ResponsiveContainer>
      {showLegend && (
        <div className="flex gap-3 mt-1 px-1 text-[9px] text-muted-foreground">
          <div className="flex items-center gap-1">
            <span className="inline-block w-2 h-2 rounded-sm" style={{ backgroundColor: "hsl(174 72% 46%)" }} />
            <span>{"\u2193"} In</span>
          </div>
          <div className="flex items-center gap-1">
            <span className="inline-block w-2 h-2 rounded-sm" style={{ backgroundColor: "hsl(174 72% 46%)", opacity: 0.4 }} />
            <span>{"\u2191"} Out</span>
          </div>
        </div>
      )}
    </div>
  )
}
