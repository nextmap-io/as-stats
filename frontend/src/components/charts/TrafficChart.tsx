import {
  AreaChart,
  Area,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
  ReferenceLine,
} from "recharts"
import type { TrafficPoint } from "@/lib/types"
import { useUnit } from "@/hooks/useUnit"
import { useChartColors } from "@/hooks/useChartColors"

interface TrafficChartProps {
  data: TrafficPoint[]
  height?: number
  showLegend?: boolean
  title?: string
  p95In?: number
  p95Out?: number
  timeBounds?: { from: number; to: number }
  /**
   * Optional previous-period series (Module D comparison overlay), already
   * time-aligned onto the current axis via `shiftSeries`. When present it is
   * drawn as dashed, muted in/out lines on top of the current areas.
   */
  previous?: TrafficPoint[]
}

function getIntervalSeconds(data: TrafficPoint[]): number {
  let minDiff = Infinity
  for (let i = 1; i < data.length; i++) {
    const diff = (new Date(data[i].t).getTime() - new Date(data[i - 1].t).getTime()) / 1000
    if (diff > 0 && diff < minDiff) minDiff = diff
  }
  return minDiff === Infinity ? 300 : minDiff
}

function formatTimeShort(ts: number, multiDay: boolean): string {
  if (multiDay) {
    const d = new Date(ts)
    return `${d.getDate()}/${d.getMonth() + 1} ${d.getHours().toString().padStart(2, "0")}:${d.getMinutes().toString().padStart(2, "0")}`
  }
  return new Date(ts).toLocaleString(undefined, { hour: "2-digit", minute: "2-digit" })
}

export function TrafficChart({ data, height = 280, showLegend = true, title, p95In, p95Out, timeBounds, previous }: TrafficChartProps) {
  const { formatTraffic, formatAxis, unit } = useUnit()
  const chartColors = useChartColors()
  const interval = getIntervalSeconds(data)
  const stepMs = interval * 1000
  const usePps = unit === "pps"
  const hasPrev = !!previous && previous.length > 0

  // Previous-period lookup, keyed by timestamp snapped to the bucket step so a
  // shifted prior series lines up with the current buckets. Outbound is mirrored
  // to the negative axis, matching the current series.
  const snap = (ts: number) => (stepMs > 0 ? Math.round(ts / stepMs) * stepMs : ts)
  const prevByTs = new Map<number, { inbound: number; outbound: number }>()
  if (previous) {
    for (const d of previous) {
      const ts = snap(new Date(d.t).getTime())
      const inVal = usePps ? d.packets_in || 0 : d.bytes_in
      const outVal = usePps ? d.packets_out || 0 : d.bytes_out || 0
      prevByTs.set(ts, { inbound: inVal, outbound: -outVal })
    }
  }

  // Detect multi-day range
  let minTs = Infinity, maxTs = -Infinity
  for (const d of data) {
    const t = new Date(d.t).getTime()
    if (t < minTs) minTs = t
    if (t > maxTs) maxTs = t
  }
  const multiDay = (maxTs - minTs) > 86400000

  const dataByTs = new Map<number, { inbound: number; outbound: number }>()
  for (const d of data) {
    const ts = new Date(d.t).getTime()
    const inVal = usePps ? (d.packets_in || 0) : d.bytes_in
    const outVal = usePps ? (d.packets_out || 0) : (d.bytes_out || 0)
    dataByTs.set(ts, { inbound: inVal, outbound: -outVal })
  }

  // Build data array from actual timestamps, inserting zeros for gaps.
  // Gap threshold: 4x the step — up to 3 missing buckets are normal jitter
  // and the area stays up; 4+ is a real outage and we drop to zero.
  const GAP_THRESHOLD = 4
  const sortedTs = Array.from(dataByTs.keys()).sort((a, b) => a - b)
  const formatted: {
    time: string
    inbound: number
    outbound: number
    prevInbound?: number
    prevOutbound?: number
  }[] = []

  if (timeBounds && sortedTs.length > 0 && stepMs > 0) {
    const firstTs = sortedTs[0]
    if (firstTs > timeBounds.from + stepMs * GAP_THRESHOLD) {
      formatted.push({ time: formatTimeShort(firstTs - stepMs, multiDay), inbound: 0, outbound: 0 })
    }
  }

  for (let i = 0; i < sortedTs.length; i++) {
    if (i > 0 && stepMs > 0 && (sortedTs[i] - sortedTs[i - 1]) > stepMs * GAP_THRESHOLD) {
      formatted.push({ time: formatTimeShort(sortedTs[i - 1] + stepMs, multiDay), inbound: 0, outbound: 0 })
      formatted.push({ time: formatTimeShort(sortedTs[i] - stepMs, multiDay), inbound: 0, outbound: 0 })
    }
    const t = sortedTs[i]
    const v = dataByTs.get(t)!
    const prev = hasPrev ? prevByTs.get(snap(t)) : undefined
    formatted.push({
      time: formatTimeShort(t, multiDay),
      ...v,
      prevInbound: prev?.inbound,
      prevOutbound: prev?.outbound,
    })
  }

  if (timeBounds && sortedTs.length > 0 && stepMs > 0) {
    const lastTs = sortedTs[sortedTs.length - 1]
    if (lastTs < timeBounds.to - stepMs * GAP_THRESHOLD) {
      formatted.push({ time: formatTimeShort(lastTs + stepMs, multiDay), inbound: 0, outbound: 0 })
    }
  }

  const tickInterval = formatted.length > 0 ? Math.max(1, Math.floor(formatted.length / 8)) : 1

  return (
    <div className="animate-fade-in overflow-hidden">
      {title && <h3 className="text-[10px] font-medium text-muted-foreground mb-1 uppercase tracking-wider">{title}</h3>}
      <ResponsiveContainer width="100%" height={height}>
        <AreaChart data={formatted} margin={{ top: 2, right: 2, left: 0, bottom: 0 }}>
          <CartesianGrid strokeDasharray="3 3" stroke={chartColors.grid} opacity={0.5} />
          <XAxis
            dataKey="time"
            tick={{ fontSize: 8, fill: chartColors.text }}
            tickLine={{ stroke: chartColors.grid }}
            axisLine={{ stroke: chartColors.grid }}
            interval={tickInterval}
          />
          <YAxis
            tick={{ fontSize: 8, fill: chartColors.text }}
            tickLine={false}
            axisLine={false}
            tickFormatter={(v) => formatAxis(Math.abs(v), interval)}
            width={40}
          />
          <ReferenceLine y={0} stroke={chartColors.text} strokeWidth={1} />
          {p95In != null && p95In > 0 && (
            <ReferenceLine y={p95In} stroke="#e74c3c" strokeDasharray="4 2" strokeWidth={1} label={{ value: `p95: ${formatTraffic(p95In, interval)}`, position: "right", fontSize: 8, fill: "#e74c3c" }} />
          )}
          {p95Out != null && p95Out > 0 && (
            <ReferenceLine y={-p95Out} stroke="#e74c3c" strokeDasharray="4 2" strokeWidth={1} label={{ value: `p95: ${formatTraffic(p95Out, interval)}`, position: "right", fontSize: 8, fill: "#e74c3c" }} />
          )}
          <Tooltip
            cursor={{ stroke: "hsl(215 12% 50%)", strokeOpacity: 0.3 }}
            contentStyle={{
              backgroundColor: chartColors.tooltipBg,
              border: `1px solid ${chartColors.tooltipBorder}`,
              borderRadius: 4,
              fontSize: 10,
              boxShadow: "0 4px 12px rgba(0,0,0,0.5)",
              padding: "5px 8px",
            }}
            itemStyle={{ padding: 0, color: chartColors.tooltipText }}
            formatter={(value, name) => {
              if (value == null) return [null, null]
              const abs = Math.abs(Number(value))
              if (abs === 0) return [null, null]
              const labels: Record<string, string> = {
                inbound: "\u2193 In",
                outbound: "\u2191 Out",
                prevInbound: "\u2193 In (prev)",
                prevOutbound: "\u2191 Out (prev)",
              }
              return [formatTraffic(abs, interval), labels[String(name)] ?? String(name)]
            }}
            labelStyle={{ color: chartColors.text, marginBottom: 2, fontSize: 9 }}
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
          {hasPrev && (
            <Line
              type="stepAfter"
              dataKey="prevInbound"
              stroke={chartColors.text}
              strokeDasharray="4 3"
              strokeWidth={1}
              dot={false}
              connectNulls
              isAnimationActive={false}
            />
          )}
          {hasPrev && (
            <Line
              type="stepAfter"
              dataKey="prevOutbound"
              stroke={chartColors.text}
              strokeDasharray="4 3"
              strokeWidth={1}
              dot={false}
              connectNulls
              isAnimationActive={false}
            />
          )}
        </AreaChart>
      </ResponsiveContainer>
      {showLegend && (
        <div className="flex gap-3 mt-1 px-1 text-[9px] text-muted-foreground">
          <div className="flex items-center gap-1">
            <span className="inline-block size-2 rounded-sm" style={{ backgroundColor: "hsl(174 72% 46%)" }} />
            <span>{"\u2193"} In</span>
          </div>
          <div className="flex items-center gap-1">
            <span className="inline-block size-2 rounded-sm" style={{ backgroundColor: "hsl(174 72% 46%)", opacity: 0.4 }} />
            <span>{"\u2191"} Out</span>
          </div>
          {hasPrev && (
            <div className="flex items-center gap-1">
              <span
                className="inline-block h-0 w-3 border-t border-dashed"
                style={{ borderColor: chartColors.text }}
              />
              <span>Previous period</span>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
