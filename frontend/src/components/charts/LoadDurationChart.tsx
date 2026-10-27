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
import type { LoadCurve } from "@/lib/types"
import { axisTick, tooltipBoxStyle, useChartColors, annotationLabel, X_TICK_MARGIN } from "@/hooks/useChartColors"
import { formatBitsPerSec } from "@/lib/utils"
import { rateAxisTicks } from "@/lib/rate"

interface LoadDurationChartProps {
  curve: LoadCurve
  /** Configured link capacity in bps — draws a horizontal reference line when > 0. */
  capacityBps?: number
  height?: number
  title?: string
}

/**
 * Load-duration curve: per-bucket throughput sorted descending, plotted against
 * the fraction of time it was met or exceeded (0–100%). A point at (x%, y bps)
 * means "throughput was >= y for x% of the window". An optional horizontal line
 * marks the link capacity.
 */
export function LoadDurationChart({ curve, capacityBps, height = 300, title }: LoadDurationChartProps) {
  const chartColors = useChartColors()

  const n = curve.points.length
  const data = curve.points.map((bps, i) => ({
    pct: n > 1 ? (i / (n - 1)) * 100 : 0,
    bps,
  }))

  const maxData = n > 0 ? curve.points[0] : 0
  // Drawn against capacity when there is one (a full chart means a full pipe),
  // on round bit rates: the top used to be capacity × 1.05, labelled "1.05 Gb/s".
  const domainMax = capacityBps && capacityBps > maxData ? capacityBps : maxData
  const y = rateAxisTicks(0, domainMax, 1, "bytes", "bytes")

  return (
    <div className="overflow-hidden">
      {title && <h3 className="mb-1 text-[13px] text-foreground-secondary">{title}</h3>}
      <ResponsiveContainer width="100%" height={height}>
        <AreaChart data={data} margin={{ top: 8, right: 2, left: 0, bottom: 0 }}>
          <CartesianGrid vertical={false} stroke={chartColors.grid} />
          <XAxis
            tickMargin={X_TICK_MARGIN}
            dataKey="pct"
            type="number"
            domain={[0, 100]}
            tick={axisTick(chartColors)}
            tickLine={false}
            axisLine={{ stroke: chartColors.ink }}
            tickFormatter={(v) => `${Math.round(Number(v))}%`}
            ticks={[0, 25, 50, 75, 100]}
          />
          <YAxis
            type="number"
            domain={y.domain}
            ticks={y.ticks}
            interval={0}
            tick={axisTick(chartColors)}
            tickLine={false}
            axisLine={false}
            tickFormatter={(v) => formatBitsPerSec(Number(v))}
            width={72}
          />
          {capacityBps != null && capacityBps > 0 && (
            <ReferenceLine
              y={capacityBps}
              stroke={chartColors.ink}
              strokeDasharray="4 3"
              strokeWidth={1}
              label={annotationLabel(chartColors, `capacity: ${formatBitsPerSec(capacityBps)}`, "insideTopRight")}
            />
          )}
          <Tooltip
            cursor={{ stroke: chartColors.text, strokeOpacity: 0.4 }}
            contentStyle={tooltipBoxStyle(chartColors)}
            itemStyle={{ padding: 0, color: chartColors.tooltipText }}
            labelStyle={{ color: chartColors.text, marginBottom: 2 }}
            labelFormatter={(v) => `${Math.round(Number(v))}% of time`}
            formatter={(value) => [formatBitsPerSec(Number(value)), "Throughput"]}
          />
          <Area
            type="stepAfter"
            dataKey="bps"
            // The busier direction, either way: neutral ink, not the outbound cobalt.
            stroke={chartColors.ink}
            fill={chartColors.text}
            fillOpacity={0.3}
            strokeWidth={1}
            dot={false}
            isAnimationActive={false}
          />
        </AreaChart>
      </ResponsiveContainer>
    </div>
  )
}
