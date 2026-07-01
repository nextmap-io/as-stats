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
import { useChartColors } from "@/hooks/useChartColors"
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
    <div className="animate-fade-in overflow-hidden">
      {title && (
        <h3 className="text-[10px] font-medium text-muted-foreground mb-1 uppercase tracking-wider">{title}</h3>
      )}
      <ResponsiveContainer width="100%" height={height}>
        <AreaChart data={data} margin={{ top: 2, right: 2, left: 0, bottom: 0 }}>
          <CartesianGrid strokeDasharray="3 3" stroke={chartColors.grid} opacity={0.5} />
          <XAxis
            dataKey="pct"
            type="number"
            domain={[0, 100]}
            tick={{ fontSize: 8, fill: chartColors.text }}
            tickLine={{ stroke: chartColors.grid }}
            axisLine={{ stroke: chartColors.grid }}
            tickFormatter={(v) => `${Math.round(Number(v))}%`}
            ticks={[0, 25, 50, 75, 100]}
          />
          <YAxis
            type="number"
            domain={[0, yMax]}
            tick={{ fontSize: 8, fill: chartColors.text }}
            tickLine={false}
            axisLine={false}
            tickFormatter={(v) => formatBitsPerSec(Number(v))}
            width={54}
          />
          {capacityBps != null && capacityBps > 0 && (
            <ReferenceLine
              y={capacityBps}
              stroke="#e74c3c"
              strokeDasharray="4 2"
              strokeWidth={1}
              label={{
                value: `capacity: ${formatBitsPerSec(capacityBps)}`,
                position: "insideTopRight",
                fontSize: 8,
                fill: "#e74c3c",
              }}
            />
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
            labelStyle={{ color: chartColors.text, marginBottom: 2, fontSize: 9 }}
            labelFormatter={(v) => `${Math.round(Number(v))}% of time`}
            formatter={(value) => [formatBitsPerSec(Number(value)), "Throughput"]}
          />
          <Area
            type="stepAfter"
            dataKey="bps"
            stroke="hsl(174 72% 46%)"
            fill="hsl(174 72% 46%)"
            fillOpacity={0.5}
            strokeWidth={1}
            dot={false}
            isAnimationActive={false}
          />
        </AreaChart>
      </ResponsiveContainer>
    </div>
  )
}
