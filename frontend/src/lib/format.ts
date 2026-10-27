import { formatBitRate, siParts } from "@/lib/rate"
import type { Alert } from "@/lib/types"

const TIME = new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit" })
const NUMBER = new Intl.NumberFormat()

export function formatClock(ms: number): string {
  return TIME.format(ms)
}

const DATE_TIME = new Intl.DateTimeFormat(undefined, { year: "numeric", month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" })

/**
 * A point in time in tables and captions: "Oct 27, 2026, 08:33 PM", the same
 * shape as the time bar. `toLocaleString()` gave "10/27/2026, 8:33:00 PM",
 * seconds included, next to the rest of the app's "Oct 27, 08:30 PM".
 */
export function formatDateTime(value: string | number | Date): string {
  const d = value instanceof Date ? value : new Date(value)
  return Number.isNaN(d.getTime()) ? String(value) : DATE_TIME.format(d)
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
  if (type === "percent") return `${NUMBER.format(Math.round(value * 10) / 10)}%`
  return NUMBER.format(Math.round(value * 10) / 10)
}
