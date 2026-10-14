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
import { axisTick, tooltipBoxStyle, useChartColors } from "@/hooks/useChartColors"
import { inferStepSeconds, formatTimeShort } from "./time"

interface TrafficChartProps {
  data: TrafficPoint[]
  height?: number
  showLegend?: boolean
  title?: string
  /** p95 per direction, in bytes per bucket (same unit as the points). */
  p95In?: number
  p95Out?: number
  timeBounds?: { from: number; to: number }
  /**
   * Bucket width in seconds reported by the API (meta.step_seconds). Falls back
   * to the spacing between points for servers that do not send it.
   */
  stepSeconds?: number
  /**
   * Optional previous-period series (Module D comparison overlay), already
   * time-aligned onto the current axis via `shiftSeries`. When present it is
   * drawn as dashed lines on top of the current areas.
   */
  previous?: TrafficPoint[]
  /** Accessible summary of what the chart shows. */
  ariaLabel?: string
}

/**
 * TrafficChart: the classic AS-Stats mirrored step chart. Inbound sits above a
 * solid zero line and outbound below it, on ONE scale so the two areas compare
 * directly. Steps are honest about bucketed data.
 */
export function TrafficChart({
  data,
  height = 280,
  showLegend = true,
  title,
  p95In,
  p95Out,
  timeBounds,
  stepSeconds,
  previous,
  ariaLabel,
}: TrafficChartProps) {
  const { formatValue, formatAxis, unit } = useUnit()
  const colors = useChartColors()
  const interval = stepSeconds && stepSeconds > 0 ? stepSeconds : inferStepSeconds(data)
  const stepMs = interval * 1000
  // pps needs packet counts. Without them the chart stays in bits per second.
  const hasPackets = data.some((d) => (d.packets_in ?? 0) > 0 || (d.packets_out ?? 0) > 0)
  const usePps = unit === "pps" && hasPackets
  const kind = usePps ? "packets" : "bytes"
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

  const dataByTs = new Map<number, { inbound: number; outbound: number }>()
  for (const d of data) {
    const ts = new Date(d.t).getTime()
    const inVal = usePps ? d.packets_in || 0 : d.bytes_in
    const outVal = usePps ? d.packets_out || 0 : d.bytes_out || 0
    dataByTs.set(ts, { inbound: inVal, outbound: -outVal })
  }

  const sortedTs = Array.from(dataByTs.keys()).sort((a, b) => a - b)
  const span = timeBounds
    ? timeBounds.to - timeBounds.from
    : sortedTs.length > 1
      ? sortedTs[sortedTs.length - 1] - sortedTs[0]
      : 0
  const multiDay = span > 86_400_000

  // Build rows from actual timestamps, inserting zeros for gaps. Up to 3
  // missing buckets are normal jitter and the area stays up; 4+ is a real
  // outage and the area drops to zero.
  const GAP_THRESHOLD = 4
  type Row = { t: number; inbound: number; outbound: number; prevInbound?: number; prevOutbound?: number }
  const rows: Row[] = []

  if (timeBounds && sortedTs.length > 0 && stepMs > 0 && sortedTs[0] > timeBounds.from + stepMs * GAP_THRESHOLD) {
    rows.push({ t: sortedTs[0] - stepMs, inbound: 0, outbound: 0 })
  }
  for (let i = 0; i < sortedTs.length; i++) {
    if (i > 0 && stepMs > 0 && sortedTs[i] - sortedTs[i - 1] > stepMs * GAP_THRESHOLD) {
      rows.push({ t: sortedTs[i - 1] + stepMs, inbound: 0, outbound: 0 })
      rows.push({ t: sortedTs[i] - stepMs, inbound: 0, outbound: 0 })
    }
    const t = sortedTs[i]
    const prev = hasPrev ? prevByTs.get(snap(t)) : undefined
    rows.push({ t, ...dataByTs.get(t)!, prevInbound: prev?.inbound, prevOutbound: prev?.outbound })
  }
  if (timeBounds && sortedTs.length > 0 && stepMs > 0) {
    const lastTs = sortedTs[sortedTs.length - 1]
    if (lastTs < timeBounds.to - stepMs * GAP_THRESHOLD) rows.push({ t: lastTs + stepMs, inbound: 0, outbound: 0 })
  }

  const domainFrom = timeBounds ? timeBounds.from : (sortedTs[0] ?? 0)
  const domainTo = timeBounds ? timeBounds.to : (sortedTs[sortedTs.length - 1] ?? domainFrom + stepMs)
  const TICKS = 6
  const xTicks =
    domainTo > domainFrom
      ? Array.from({ length: TICKS + 1 }, (_, i) => Math.round(domainFrom + ((domainTo - domainFrom) * i) / TICKS))
      : undefined

  const labels: Record<string, string> = {
    inbound: "In",
    outbound: "Out",
    prevInbound: "In, previous",
    prevOutbound: "Out, previous",
  }

  return (
    <figure className="m-0 overflow-hidden" aria-label={ariaLabel ?? title ?? "Traffic over time, inbound above the axis, outbound below"}>
      {title && <figcaption className="mb-1 text-[13px] text-foreground-secondary">{title}</figcaption>}
      <ResponsiveContainer width="100%" height={height}>
        <AreaChart data={rows} margin={{ top: 6, right: 4, left: 0, bottom: 0 }}>
          <CartesianGrid vertical={false} stroke={colors.grid} />
          <XAxis
            type="number"
            dataKey="t"
            scale="time"
            domain={[domainFrom, domainTo]}
            ticks={xTicks}
            allowDataOverflow
            tick={axisTick(colors)}
            tickLine={false}
            axisLine={false}
            tickFormatter={(t) => formatTimeShort(Number(t), multiDay)}
          />
          <YAxis
            tick={axisTick(colors)}
            tickLine={false}
            axisLine={false}
            tickFormatter={(v) => formatAxis(Math.abs(Number(v)), interval, kind)}
            width={44}
          />
          <ReferenceLine y={0} stroke={colors.ink} strokeWidth={1} />
          {!usePps && p95In != null && p95In > 0 && (
            <ReferenceLine
              y={p95In}
              stroke={colors.ink}
              strokeDasharray="2 3"
              label={{ value: `p95 in ${formatValue(p95In, interval)}`, position: "insideTopRight", fontSize: 12, fill: colors.text }}
            />
          )}
          {!usePps && p95Out != null && p95Out > 0 && (
            <ReferenceLine
              y={-p95Out}
              stroke={colors.ink}
              strokeDasharray="2 3"
              label={{ value: `p95 out ${formatValue(p95Out, interval)}`, position: "insideBottomRight", fontSize: 12, fill: colors.text }}
            />
          )}
          <Tooltip
            cursor={{ stroke: colors.text, strokeOpacity: 0.4 }}
            contentStyle={tooltipBoxStyle(colors)}
            itemStyle={{ padding: 0, color: colors.tooltipText }}
            labelStyle={{ color: colors.text, marginBottom: 2 }}
            labelFormatter={(t) => formatTimeShort(Number(t), true)}
            formatter={(value, name) => {
              if (value == null) return [null, null]
              const abs = Math.abs(Number(value))
              return [formatValue(abs, interval, kind), labels[String(name)] ?? String(name)]
            }}
          />
          <Area
            type="stepAfter"
            dataKey="inbound"
            stroke={colors.inbound}
            fill={colors.inbound}
            fillOpacity={0.85}
            strokeWidth={0}
            dot={false}
            isAnimationActive={false}
          />
          <Area
            type="stepAfter"
            dataKey="outbound"
            stroke={colors.outbound}
            fill={colors.outbound}
            fillOpacity={0.9}
            strokeWidth={0}
            dot={false}
            isAnimationActive={false}
          />
          {hasPrev && (
            <Line
              type="stepAfter"
              dataKey="prevInbound"
              stroke={colors.ink}
              strokeDasharray="4 3"
              strokeWidth={1.25}
              dot={false}
              connectNulls
              isAnimationActive={false}
            />
          )}
          {hasPrev && (
            <Line
              type="stepAfter"
              dataKey="prevOutbound"
              stroke={colors.ink}
              strokeDasharray="4 3"
              strokeWidth={1.25}
              dot={false}
              connectNulls
              isAnimationActive={false}
            />
          )}
        </AreaChart>
      </ResponsiveContainer>
      {showLegend && (
        <div className="mt-2 flex flex-wrap gap-x-5 gap-y-1 text-[13px] text-muted-foreground">
          <span className="inline-flex items-center gap-1.5">
            <span className="inline-block h-2.5 w-3.5 rounded-[1px] bg-traffic-in" aria-hidden />
            Inbound, above the axis
          </span>
          <span className="inline-flex items-center gap-1.5">
            <span className="inline-block h-2.5 w-3.5 rounded-[1px] bg-traffic-out" aria-hidden />
            Outbound, below
          </span>
          {hasPrev && (
            <span className="inline-flex items-center gap-1.5">
              <span className="inline-block w-4 border-t border-dashed border-foreground" aria-hidden />
              Previous period
            </span>
          )}
          {unit === "pps" && !hasPackets && <span>Packet counts are not available here, so this chart stays in b/s.</span>}
        </div>
      )}
    </figure>
  )
}
