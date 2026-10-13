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
import { axisTick, tooltipBoxStyle, useChartColors } from "@/hooks/useChartColors"
import { formatBitsPerSec } from "@/lib/utils"

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
  const domainMax = capacityBps && capacityBps > maxData ? capacityBps : maxData
  const yMax = domainMax > 0 ? domainMax * 1.05 : 1

  return (
    <div className="overflow-hidden">
      {title && <h3 className="mb-1 text-[13px] text-foreground-secondary">{title}</h3>}
      <ResponsiveContainer width="100%" height={height}>
        <AreaChart data={data} margin={{ top: 2, right: 2, left: 0, bottom: 0 }}>
          <CartesianGrid vertical={false} stroke={chartColors.grid} />
          <XAxis
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
            domain={[0, yMax]}
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
              label={{
                value: `capacity: ${formatBitsPerSec(capacityBps)}`,
                position: "insideTopRight",
                fontSize: 12,
                fill: chartColors.ink,
              }}
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
            stroke={chartColors.outbound}
            fill={chartColors.outbound}
            fillOpacity={0.35}
            strokeWidth={1}
            dot={false}
            isAnimationActive={false}
          />
        </AreaChart>
      </ResponsiveContainer>
    </div>
  )
}
