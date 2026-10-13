import type { ThreatDirection, ThreatLevel, ThreatSeriesPoint } from "@/lib/types"
import { cn } from "@/lib/utils"

/**
 * ThreatSpark: the host's last 60 complete minutes as a step area, with its
 * usual peak (p95 at this hour over 7 days) as a dashed line. Each chart is
 * scaled to its own host, never below the p95 line, so a jump reads as
 * "how far above its own normal". The run above p95 (`since`) takes the row's
 * level colour; the text next to the chart says the same in words.
 */
export function ThreatSpark({
  series,
  axis,
  p95,
  since,
  level,
  dir,
  label,
}: {
  series: ThreatSeriesPoint[]
  axis: "bps" | "pps"
  /** Usual peak on the same axis; 0 when the host has no history. */
  p95: number
  since: string
  level: ThreatLevel
  dir: ThreatDirection
  label: string
}) {
  const W = 240
  const H = 44
  const n = series.length
  if (n < 2) return <div className="h-11" aria-hidden />

  const values = series.map((p) => Math.max(0, axis === "pps" ? p.pps : p.bps))
  const top = Math.max(1, ...values, p95 * 1.15) * 1.05
  const x = (i: number) => (i / n) * W
  const y = (v: number) => H - (v / top) * H

  const area = (from: number) => {
    let d = `M${x(from).toFixed(1)},${H}`
    for (let i = from; i < n; i++) {
      const yy = y(values[i]).toFixed(1)
      d += `L${x(i).toFixed(1)},${yy}L${x(i + 1).toFixed(1)},${yy}`
    }
    return `${d}L${W},${H}Z`
  }

  const sinceMs = since ? Date.parse(since) : NaN
  const sinceIdx = Number.isNaN(sinceMs) ? -1 : series.findIndex((p) => Date.parse(p.t) >= sinceMs)
  const lit = level !== "normal" && sinceIdx >= 0

  return (
    <svg
      viewBox={`0 0 ${W} ${H}`}
      preserveAspectRatio="none"
      className="block h-11 w-full overflow-visible"
      role="img"
      aria-label={label}
    >
      <path d={area(0)} className={dir === "in" ? "fill-traffic-in" : "fill-traffic-out"} opacity={0.75} />
      {lit && (
        <path d={area(sinceIdx)} className={cn(level === "incident" ? "fill-destructive" : "fill-warning-mark")} />
      )}
      {p95 > 0 && (
        <line
          x1="0"
          x2={W}
          y1={y(p95)}
          y2={y(p95)}
          className="stroke-foreground"
          strokeWidth={1}
          strokeDasharray="3 3"
          vectorEffect="non-scaling-stroke"
        />
      )}
      <line x1="0" x2={W} y1={H - 0.5} y2={H - 0.5} className="stroke-border" vectorEffect="non-scaling-stroke" />
    </svg>
  )
}
