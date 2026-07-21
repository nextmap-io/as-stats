import { useNavigate } from "react-router-dom"
import { Treemap, PieChart, Pie, Cell, Tooltip, ResponsiveContainer } from "recharts"
import { useChartColors } from "@/hooks/useChartColors"

export interface VizDatum {
  /** Display name (legend / tooltip / label). */
  name: string
  /** Proportional magnitude (bytes/packets/flows — already numeric). */
  value: number
  /** Optional drill-down route; clicking the node navigates here. */
  path?: string
  /** Optional explicit fill; falls back to a generated hue ramp. */
  color?: string
}

// Deterministic, evenly-spread hue ramp so adjacent slices/tiles stay
// distinguishable even with many entries. Saturation/lightness are fixed to
// read on both themes.
function colorAt(i: number): string {
  return `hsl(${(i * 47) % 360} 62% 52%)`
}

interface TopNVizProps {
  data: VizDatum[]
  view: "treemap" | "donut"
  /** Formats the raw value for tooltips (e.g. bytes → "1.2 Gbps"). */
  formatValue: (v: number) => string
  /** Accessible summary of what the chart shows. */
  label: string
  height?: number
}

/**
 * TopNViz (U9) — proportional-share visualization for the Top-N pages. Renders
 * either a Recharts Treemap or a donut PieChart from data the page already
 * fetched; clicking a node drills to its detail route. A visually-hidden table
 * mirrors the data for screen readers, and the container is role="img" with a
 * summary label.
 */
export function TopNViz({ data, view, formatValue, label, height = 420 }: TopNVizProps) {
  const navigate = useNavigate()
  const colors = useChartColors()

  const total = data.reduce((s, d) => s + d.value, 0)
  // Precompute colour + share so the tooltip formatter can read them off the
  // node payload without needing the chart's total in scope.
  const colored = data.map((d, i) => ({
    ...d,
    color: d.color ?? colorAt(i),
    share: total > 0 ? (d.value / total) * 100 : 0,
  }))

  const go = (datum?: { path?: string }) => {
    if (datum?.path) navigate(datum.path)
  }

  const tooltipStyle = {
    backgroundColor: colors.tooltipBg,
    border: `1px solid ${colors.tooltipBorder}`,
    borderRadius: 4,
    fontSize: 11,
    padding: "4px 8px",
    color: colors.tooltipText,
  }

  // Recharts hands the formatter the datum payload as the 3rd arg; we read the
  // precomputed share off it and return a "<value> · <pct>%" string + name.
  // Typed inline so Recharts' generics drive the param types.
  const tooltip = (
    <Tooltip
      contentStyle={tooltipStyle}
      itemStyle={{ color: colors.tooltipText }}
      formatter={(value, _name, item) => {
        const p = item?.payload as (VizDatum & { share?: number }) | undefined
        const share = p?.share ?? 0
        return [`${formatValue(Number(value))} · ${share.toFixed(1)}%`, p?.name ?? String(_name)]
      }}
    />
  )

  return (
    <div>
      <div role="img" aria-label={label}>
        <ResponsiveContainer width="100%" height={height}>
          {view === "treemap" ? (
            <Treemap
              data={colored}
              dataKey="value"
              nameKey="name"
              stroke={colors.tooltipBg}
              isAnimationActive={false}
              content={<TreemapCell />}
              onClick={(node: unknown) => go(node as { path?: string })}
            >
              {tooltip}
            </Treemap>
          ) : (
            <PieChart>
              <Pie
                data={colored}
                dataKey="value"
                nameKey="name"
                cx="50%"
                cy="50%"
                innerRadius="55%"
                outerRadius="80%"
                paddingAngle={1}
                isAnimationActive={false}
                onClick={(node: unknown) => go(node as { path?: string })}
              >
                {colored.map((d) => (
                  <Cell
                    key={d.name}
                    fill={d.color}
                    stroke={colors.tooltipBg}
                    className={d.path ? "cursor-pointer" : undefined}
                  />
                ))}
              </Pie>
              {tooltip}
            </PieChart>
          )}
        </ResponsiveContainer>
      </div>

      {/* Screen-reader data fallback */}
      <table className="sr-only">
        <caption>{label}</caption>
        <thead>
          <tr>
            <th scope="col">Name</th>
            <th scope="col">Value</th>
            <th scope="col">Share</th>
          </tr>
        </thead>
        <tbody>
          {colored.map((d) => (
            <tr key={d.name}>
              <th scope="row">{d.name}</th>
              <td>{formatValue(d.value)}</td>
              <td>{total > 0 ? `${((d.value / total) * 100).toFixed(1)}%` : "0%"}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

// Custom treemap tile: fills with the datum colour, draws a readable label when
// the tile is large enough, and shows a pointer when it has a drill target.
interface TreemapCellProps {
  x?: number
  y?: number
  width?: number
  height?: number
  name?: string
  color?: string
  path?: string
}

function TreemapCell(props: TreemapCellProps) {
  const { x = 0, y = 0, width = 0, height = 0, name, color, path } = props
  const showLabel = width > 46 && height > 22
  return (
    <g className={path ? "cursor-pointer" : undefined}>
      <rect x={x} y={y} width={width} height={height} fill={color ?? "hsl(200 60% 50%)"} stroke="var(--color-card)" strokeWidth={1} />
      {showLabel && (
        <text
          x={x + 4}
          y={y + 14}
          fill="hsl(0 0% 100%)"
          fontSize={10}
          className="pointer-events-none"
          style={{ paintOrder: "stroke", stroke: "rgba(0,0,0,0.35)", strokeWidth: 2 }}
        >
          {name}
        </text>
      )}
    </g>
  )
}
