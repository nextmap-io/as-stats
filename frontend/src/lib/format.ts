import { formatBitRate, siParts } from "@/lib/rate"
import type { Alert } from "@/lib/types"

const TIME = new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit" })
const NUMBER = new Intl.NumberFormat()

export function formatClock(ms: number): string {
  return TIME.format(ms)
}

export function formatCount(n: number): string {
  return NUMBER.format(Math.round(n))
}

/** "605 Mb/s" → ["605", "Mb/s"], for the large figures. */
export function splitUnit(formatted: string): [string, string] {
  const i = formatted.lastIndexOf(" ")
  return i < 0 ? [formatted, ""] : [formatted.slice(0, i), formatted.slice(i + 1)]
}

/** Link capacity: "1 Gb/s", "200 Mb/s", "10 Gb/s". */
export function formatCapacity(bps: number): string {
  if (bps <= 0) return "not set"
  const mbps = bps / 1e6
  if (mbps >= 1000) return `${Number((mbps / 1000).toFixed(1))} Gb/s`
  return `${Number(mbps.toFixed(1))} Mb/s`
}

/** Percent with one decimal below 10, none above: "0.4%", "75%". */
export function formatPct(pct: number | null): string {
  if (pct === null) return "–"
  if (pct > 0 && pct < 10) return `${pct.toFixed(1)}%`
  return `${Math.round(pct)}%`
}

/** An alert's value or threshold in its own unit. */
export function formatAlertMetric(value: number, type: Alert["metric_type"]): string {
  if (type === "bps") return formatBitRate(value)
  if (type === "pps") {
    const { value: v, prefix } = siParts(value)
    return `${v} ${prefix}p/s`
  }
  return NUMBER.format(Math.round(value * 10) / 10)
}
