import { Link } from "react-router-dom"
import { UTIL_WATCH_PCT, utilLevel, type LinkRowData } from "@/lib/home"
import { bytesToBps, formatBitRate, formatRate } from "@/lib/rate"
import { cn } from "@/lib/utils"
import { formatCapacity, formatPct } from "@/lib/format"

/**
 * LinkTable: one row per link, worst peak utilisation first. Utilisation is the
 * busier direction against capacity (a link is full duplex): the bar is now,
 * the tick is the window peak, the dashed line is 70%. The small chart is drawn
 * to the link's own capacity, so a full chart is a full pipe.
 */
export function LinkTable({ rows, step, filterSearch }: { rows: LinkRowData[]; step: number; filterSearch: string }) {
  return (
    <div>
      <div
        className="hidden grid-cols-[minmax(150px,1.5fr)_80px_120px_minmax(180px,1.6fr)_110px_130px] items-end gap-x-5 border-b border-foreground pb-2 text-[13px] text-muted-foreground lg:grid"
        aria-hidden
      >
        <span>Link</span>
        <span className="text-right">Capacity</span>
        <span className="text-right">Now in / out</span>
        <span>Utilisation now, and peak</span>
        <span className="text-right">Volume in / out</span>
        <span>Traffic to capacity</span>
      </div>
      <ul>
        {rows.map((r) => (
          <li
            key={r.tag}
            className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-5 gap-y-2 border-b border-border py-3 [grid-template-areas:'name_cap''meter_meter''now_vol'] lg:grid-cols-[minmax(150px,1.5fr)_80px_120px_minmax(180px,1.6fr)_110px_130px] lg:[grid-template-areas:'name_cap_now_meter_vol_spark']"
          >
            <div className="min-w-0 [grid-area:name]">
              <Link to={`/link/${encodeURIComponent(r.tag)}${filterSearch}`} className="font-semibold hover:underline">
                {r.name}
              </Link>
              {r.name !== r.tag && <span className="block truncate text-xs text-muted-foreground">{r.tag}</span>}
            </div>
            <div className="text-right text-sm text-foreground-secondary [grid-area:cap]">{formatCapacity(r.capacityBps)}</div>
            <div className="text-sm leading-snug [grid-area:now] lg:text-right">
              <span className="text-traffic-in">{formatBitRate(r.nowIn)}</span>
              <span className="text-muted-foreground lg:hidden"> in, </span>
              <br className="hidden lg:block" />
              <span className="text-traffic-out">{formatBitRate(r.nowOut)}</span>
              <span className="text-muted-foreground lg:hidden"> out</span>
            </div>
            <div className="[grid-area:meter]">
              <Meter row={r} />
            </div>
            <div className="text-right text-sm leading-snug text-foreground-secondary [grid-area:vol]">
              {formatRate(r.volumeIn, 1, "bytes", "bytes")}
              <span className="lg:hidden"> / </span>
              <br className="hidden lg:block" />
              {formatRate(r.volumeOut, 1, "bytes", "bytes")}
            </div>
            <div className="hidden [grid-area:spark] lg:block">
              <CapacitySpark row={r} step={step} />
            </div>
          </li>
        ))}
      </ul>
      <div className="mt-3 flex flex-wrap gap-x-5 gap-y-1 text-[13px] text-muted-foreground">
        <span className="inline-flex items-center gap-1.5">
          <span className="inline-block h-2.5 w-3.5 rounded-[1px] bg-traffic-in" aria-hidden />
          Inbound
        </span>
        <span className="inline-flex items-center gap-1.5">
          <span className="inline-block h-2.5 w-3.5 rounded-[1px] bg-traffic-out" aria-hidden />
          Outbound
        </span>
        <span>Small charts: inbound up, outbound down, each edge is the link's capacity.</span>
      </div>
    </div>
  )
}

function Meter({ row }: { row: LinkRowData }) {
  if (row.capacityBps <= 0) {
    return <span className="text-[13px] text-muted-foreground">No capacity set</span>
  }
  const now = Math.min(100, row.nowPct ?? 0)
  const peak = Math.min(100, row.peakPct ?? 0)
  const level = utilLevel(row.peakPct)
  return (
    <div
      className="flex items-center gap-3"
      role="img"
      aria-label={`Utilisation now ${formatPct(row.nowPct)}, peak ${formatPct(row.peakPct)} of capacity`}
    >
      <div className="relative h-2 flex-1 rounded-[1px] bg-border-soft">
        <div
          className={cn(
            "absolute inset-y-0 left-0 rounded-[1px]",
            level === "critical" ? "bg-destructive" : level === "watch" ? "bg-warning-mark" : "bg-foreground-secondary",
          )}
          style={{ width: `${Math.max(now, now > 0 ? 0.6 : 0)}%` }}
        />
        <div
          className="absolute -inset-y-0.5 border-l border-dashed border-muted-foreground"
          style={{ left: `${UTIL_WATCH_PCT}%` }}
        />
        <div className="absolute -top-1 h-4 w-0.5 bg-foreground" style={{ left: `calc(${peak}% - 1px)` }} />
      </div>
      <span
        className={cn(
          "w-12 text-right text-sm font-semibold",
          level === "watch" && "text-warning",
          level === "critical" && "text-destructive",
        )}
      >
        {formatPct(row.peakPct)}
      </span>
    </div>
  )
}

/**
 * CapacitySpark: a mirrored step chart where the top edge is 100% of capacity
 * inbound and the bottom edge 100% outbound. Without a capacity it falls back to
 * the link's own peak and says so with a dashed frame.
 */
function CapacitySpark({ row, step }: { row: LinkRowData; step: number }) {
  const W = 130
  const H = 36
  const mid = H / 2
  const pts = row.points
  if (pts.length < 2) return <div className="h-9" />
  const t0 = Date.parse(pts[0].t)
  const t1 = Date.parse(pts[pts.length - 1].t) + step * 1000
  const x = (t: number) => ((t - t0) / Math.max(1, t1 - t0)) * W
  const rateIn = pts.map((p) => bytesToBps(p.bytes_in, step))
  const rateOut = pts.map((p) => bytesToBps(p.bytes_out, step))
  const scaled = row.capacityBps > 0
  const scale = scaled ? row.capacityBps : Math.max(1, ...rateIn, ...rateOut)
  const y = (v: number) => Math.min(mid, (v / scale) * mid)

  const path = (rates: number[], sign: 1 | -1) => {
    let d = `M0,${mid}`
    pts.forEach((p, i) => {
      const xa = x(Date.parse(p.t))
      const xb = i + 1 < pts.length ? x(Date.parse(pts[i + 1].t)) : W
      const yy = mid - sign * y(rates[i])
      d += `L${xa.toFixed(1)},${yy.toFixed(1)}L${xb.toFixed(1)},${yy.toFixed(1)}`
    })
    return `${d}L${W},${mid}Z`
  }

  return (
    <svg
      viewBox={`0 0 ${W} ${H}`}
      preserveAspectRatio="none"
      className="block h-9 w-full overflow-visible"
      role="img"
      aria-label={
        scaled
          ? `Traffic against capacity over the window; peak ${formatPct(row.peakPct)}`
          : "Traffic over the window, scaled to its own peak (no capacity set)"
      }
    >
      <line x1="0" x2={W} y1="0.5" y2="0.5" className="stroke-border" strokeDasharray={scaled ? undefined : "2 2"} vectorEffect="non-scaling-stroke" />
      <line x1="0" x2={W} y1={H - 0.5} y2={H - 0.5} className="stroke-border" strokeDasharray={scaled ? undefined : "2 2"} vectorEffect="non-scaling-stroke" />
      <path d={path(rateIn, 1)} className="fill-traffic-in" opacity={0.85} />
      <path d={path(rateOut, -1)} className="fill-traffic-out" opacity={0.9} />
      <line x1="0" x2={W} y1={mid} y2={mid} className="stroke-foreground" strokeWidth={0.75} vectorEffect="non-scaling-stroke" />
    </svg>
  )
}
