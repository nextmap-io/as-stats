import { useMemo, useState } from "react"
import type { HeatmapCell } from "@/lib/types"
import { formatBitsPerSec } from "@/lib/utils"
import { cn } from "@/lib/utils"

// Day 1 = Monday .. 7 = Sunday (ClickHouse toDayOfWeek convention).
const DAY_LABELS = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"]
const HOURS = Array.from({ length: 24 }, (_, h) => h)

type Metric = "mean" | "peak"

function valueOf(cell: HeatmapCell, metric: Metric): number {
  return metric === "peak" ? cell.peak_bps : cell.mean_bps
}

/**
 * TrafficHeatmap (U8) — a 7×24 day-of-week × hour-of-day grid of throughput,
 * rendered as a pure CSS grid (no chart lib). Cells are keyboard-focusable with
 * an accessible label, colour encodes intensity via the theme `primary` token
 * (opacity ramp over a muted base, so it adapts to light/dark automatically),
 * and a visually-hidden data table mirrors the grid for screen readers.
 */
export function TrafficHeatmap({ cells }: { cells: HeatmapCell[] }) {
  const [metric, setMetric] = useState<Metric>("mean")

  // Index by (day, hour) so lookups are O(1); the backend already zero-fills.
  const grid = useMemo(() => {
    const byKey = new Map<number, HeatmapCell>()
    for (const c of cells) byKey.set(c.day * 100 + c.hour, c)
    return byKey
  }, [cells])

  const max = useMemo(() => {
    let m = 0
    for (const c of cells) m = Math.max(m, valueOf(c, metric))
    return m
  }, [cells, metric])

  const cell = (day: number, hour: number): HeatmapCell =>
    grid.get(day * 100 + hour) ?? { day, hour, mean_bps: 0, peak_bps: 0 }

  return (
    <div>
      <div className="mb-2 flex items-center justify-between gap-2">
        <div
          className="flex gap-0.5 rounded border border-input bg-muted/30 p-0.5"
          role="group"
          aria-label="Heatmap metric"
        >
          {(["mean", "peak"] as Metric[]).map((m) => (
            <button
              key={m}
              type="button"
              onClick={() => setMetric(m)}
              aria-pressed={metric === m}
              className={cn(
                "px-2 py-0.5 text-[11px] font-medium rounded transition-colors capitalize",
                metric === m
                  ? "bg-primary text-primary-foreground"
                  : "text-muted-foreground hover:text-foreground hover:bg-accent",
              )}
            >
              {m}
            </button>
          ))}
        </div>
        <Legend max={max} />
      </div>

      <div className="overflow-x-auto -mx-4 px-4 sm:-mx-5 sm:px-5">
        <div className="min-w-[520px]">
          {/* Hour axis */}
          <div className="flex pl-9">
            {HOURS.map((h) => (
              <div
                key={h}
                className="flex-1 text-center text-[8px] text-muted-foreground tabular-nums"
                aria-hidden
              >
                {h % 3 === 0 ? h : ""}
              </div>
            ))}
          </div>

          {DAY_LABELS.map((label, i) => {
            const day = i + 1
            return (
              <div key={day} className="flex items-center">
                <div className="w-9 shrink-0 text-[9px] text-muted-foreground uppercase tracking-wider" aria-hidden>
                  {label}
                </div>
                <div className="flex flex-1 gap-px py-px">
                  {HOURS.map((hour) => {
                    const c = cell(day, hour)
                    const v = valueOf(c, metric)
                    const intensity = max > 0 ? Math.pow(v / max, 0.7) : 0
                    return (
                      <button
                        key={hour}
                        type="button"
                        title={`${label} ${String(hour).padStart(2, "0")}:00 — ${metric} ${formatBitsPerSec(v)}`}
                        aria-label={`${label} ${String(hour).padStart(2, "0")}:00, ${metric} throughput ${formatBitsPerSec(v)}`}
                        className="relative flex-1 aspect-square min-w-[10px] rounded-[2px] bg-muted/60 outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:z-10"
                      >
                        <span
                          aria-hidden
                          className="absolute inset-0 rounded-[2px] bg-primary"
                          style={{ opacity: intensity }}
                        />
                      </button>
                    )
                  })}
                </div>
              </div>
            )
          })}
        </div>
      </div>

      {/* Screen-reader data fallback */}
      <table className="sr-only">
        <caption>Traffic {metric} throughput by day of week and hour of day</caption>
        <thead>
          <tr>
            <th scope="col">Day</th>
            {HOURS.map((h) => (
              <th key={h} scope="col">{`${h}:00`}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {DAY_LABELS.map((label, i) => (
            <tr key={label}>
              <th scope="row">{label}</th>
              {HOURS.map((hour) => (
                <td key={hour}>{formatBitsPerSec(valueOf(cell(i + 1, hour), metric))}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function Legend({ max }: { max: number }) {
  return (
    <div className="flex items-center gap-1.5 text-[9px] text-muted-foreground">
      <span className="tabular-nums">0</span>
      <div className="flex h-2 w-20 overflow-hidden rounded-sm bg-muted/60">
        {[0.15, 0.35, 0.55, 0.75, 1].map((o) => (
          <span key={o} className="flex-1 bg-primary" style={{ opacity: o }} aria-hidden />
        ))}
      </div>
      <span className="tabular-nums">{formatBitsPerSec(max)}</span>
    </div>
  )
}
