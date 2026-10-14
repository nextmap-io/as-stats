import { useMemo } from "react"
import { useSearchParams } from "react-router-dom"
import { useLinksTraffic } from "@/hooks/useApi"
import { useFilters } from "@/hooks/useFilters"
import { useUnit } from "@/hooks/useUnit"
import { TrafficChart } from "@/components/charts/TrafficChart"
import { previousWindow, shiftSeries, useCompareEnabled } from "@/lib/comparison"
import { directionStats, sumSeries } from "@/lib/home"
import { formatBitRate, formatRate } from "@/lib/rate"
import type { TrafficPoint } from "@/lib/types"
import { cn } from "@/lib/utils"
import { Key } from "./Section"
import { formatClock, splitUnit } from "@/lib/format"

type IPChoice = "both" | "4" | "6"

const IP_CHOICES: { value: IPChoice; label: string }[] = [
  { value: "both", label: "Both" },
  { value: "4", label: "IPv4" },
  { value: "6", label: "IPv6" },
]

/**
 * TotalTraffic: inbound and outbound now in large figures, with average and
 * peak, and ONE mirrored chart. The IPv4 / IPv6 split is a switch, not a
 * second chart; "compare with the previous window" is a checkbox on it. Both
 * live in the URL (?ipv=4|6, ?compare=prev) like every other filter.
 */
export function TotalTraffic({
  total,
  step,
}: {
  total: { both: TrafficPoint[]; v4: TrafficPoint[]; v6: TrafficPoint[] }
  step: number
}) {
  const [searchParams, setSearchParams] = useSearchParams()
  const { filters, bounds, periodSeconds, timeBounds } = useFilters()
  const { unit } = useUnit()
  const ipv = (searchParams.get("ipv") as IPChoice | null) ?? "both"
  const choice: IPChoice = ipv === "4" || ipv === "6" ? ipv : "both"
  const points = choice === "4" ? total.v4 : choice === "6" ? total.v6 : total.both

  const setParam = (key: string, value: string | null) =>
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        if (value === null) next.delete(key)
        else next.set(key, value)
        return next
      },
      { replace: true },
    )

  // Previous window: only fetched while the checkbox is on. When off, the
  // queries reuse the current filters and dedupe with the main ones.
  const compare = useCompareEnabled()
  const { prevFilters, windowMs } = useMemo(() => previousWindow(filters, bounds), [filters, bounds])
  const prevV4 = useLinksTraffic(4, compare ? prevFilters : filters)
  const prevV6 = useLinksTraffic(6, compare ? prevFilters : filters)
  const previous = useMemo(() => {
    if (!compare) return undefined
    const lists = [
      ...(choice !== "6" ? (prevV4.data?.data ?? []) : []),
      ...(choice !== "4" ? (prevV6.data?.data ?? []) : []),
    ]
    return shiftSeries(sumSeries(lists), windowMs)
  }, [compare, choice, prevV4.data, prevV6.data, windowMs])

  const sIn = directionStats(points, "in", step, bounds.to, periodSeconds)
  const sOut = directionStats(points, "out", step, bounds.to, periodSeconds)
  // Packets per second only where the series carries packet counts.
  const pps = unit === "pps" && (sIn.volumePackets > 0 || sOut.volumePackets > 0)
  const nowLabel = (s: typeof sIn) => (pps ? formatRate(s.nowPackets, 1, "pps", "packets") : formatBitRate(s.now))

  const multiDay = periodSeconds > 86_400
  const at = (t: number | null) =>
    t === null ? "" : ` at ${multiDay ? new Date(t).toLocaleString(undefined, { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" }) : formatClock(t)}`
  const windowLabel = periodSeconds <= 86_400 ? `${Math.round(periodSeconds / 3600)} h` : `${Math.round(periodSeconds / 86_400)} days`

  return (
    <div>
      <div className="mb-5 flex flex-wrap items-end gap-x-14 gap-y-4">
        <Figure dir="in" label="Inbound now" value={nowLabel(sIn)} avg={sIn.avg} peak={sIn.peak} peakAt={at(sIn.peakAt)} showRates={!pps} />
        <Figure dir="out" label="Outbound now" value={nowLabel(sOut)} avg={sOut.avg} peak={sOut.peak} peakAt={at(sOut.peakAt)} showRates={!pps} />
        <div className="flex w-full flex-wrap items-center justify-between gap-3 text-[13px] text-foreground-secondary lg:ml-auto lg:w-auto lg:justify-end">
          <div className="inline-flex overflow-hidden rounded border border-input" role="group" aria-label="IP version">
            {IP_CHOICES.map((c) => (
              <button
                key={c.value}
                type="button"
                aria-pressed={choice === c.value}
                onClick={() => setParam("ipv", c.value === "both" ? null : c.value)}
                className={cn(
                  "px-2.5 py-1",
                  choice === c.value ? "bg-accent font-semibold text-foreground" : "hover:text-foreground",
                )}
              >
                {c.label}
              </button>
            ))}
          </div>
          <label className="inline-flex cursor-pointer items-center gap-2">
            <input
              type="checkbox"
              checked={compare}
              onChange={(e) => setParam("compare", e.target.checked ? "prev" : null)}
              className="size-3.5 accent-[var(--color-ring)]"
            />
            Compare with the previous {windowLabel}
          </label>
        </div>
      </div>
      {points.length > 0 ? (
        <TrafficChart
          data={points}
          previous={previous}
          stepSeconds={step}
          timeBounds={timeBounds}
          height={280}
          showLegend={compare}
          ariaLabel={`Total traffic: inbound peaked at ${formatBitRate(sIn.peak)}, outbound at ${formatBitRate(sOut.peak)}.`}
        />
      ) : (
        <p className="py-16 text-center text-sm text-muted-foreground">No traffic in this window. Try a longer time range.</p>
      )}
      <p className="mt-2 text-[13px] text-muted-foreground">
        Volume over the window: {formatRate(sIn.volume, 1, "bytes", "bytes")} in, {formatRate(sOut.volume, 1, "bytes", "bytes")} out.
        {unit === "pps" && !pps && " Packet counts are not available for this view, so rates are in b/s."}
      </p>
    </div>
  )
}

function Figure({
  dir,
  label,
  value,
  avg,
  peak,
  peakAt,
  showRates,
}: {
  dir: "in" | "out"
  label: string
  value: string
  avg: number
  peak: number
  peakAt: string
  showRates: boolean
}) {
  const [num, unitLabel] = splitUnit(value)
  return (
    <div>
      <div className="flex items-center gap-2 text-sm text-foreground-secondary">
        <Key dir={dir} />
        {label}
      </div>
      <div className="mt-1.5 font-display text-[44px] leading-none sm:text-[64px]">
        {num}
        <span className="ml-1.5 align-baseline text-lg font-normal tracking-normal text-foreground-secondary sm:text-[22px]">{unitLabel}</span>
      </div>
      {showRates && (
        <div className="mt-1.5 text-[13px] text-muted-foreground">
          Average {formatBitRate(avg)}, peak {formatBitRate(peak)}
          {peakAt}
        </div>
      )}
    </div>
  )
}
