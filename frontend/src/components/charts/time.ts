import type { TrafficPoint } from "@/lib/types"

const TIME = new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit" })
const DAY_TIME = new Intl.DateTimeFormat(undefined, { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" })
const DAY = new Intl.DateTimeFormat(undefined, { day: "numeric", month: "short" })

/** Axis / tooltip time label: "20:00", or "12 Oct, 20:00" on multi-day windows. */
export function formatTimeShort(ts: number, multiDay: boolean): string {
  return multiDay ? DAY_TIME.format(ts) : TIME.format(ts)
}

const MIN = 60_000
const HOUR = 60 * MIN
const DAY_MS = 24 * HOUR
const TIME_STEPS = [MIN, 2 * MIN, 5 * MIN, 10 * MIN, 15 * MIN, 30 * MIN, HOUR, 2 * HOUR, 3 * HOUR, 4 * HOUR, 6 * HOUR, 12 * HOUR, DAY_MS, 2 * DAY_MS, 7 * DAY_MS]

/**
 * X-axis ticks on round local times (every 4 h on a day: 00:00, 04:00…), at
 * most `max` of them. Splitting the window in equal parts instead put ticks at
 * 08:35, 11:40, 02:45 — times nobody reads a clock by.
 */
export function timeTicks(from: number, to: number, max = 7): number[] {
  if (!(to > from)) return []
  const step = TIME_STEPS.find((s) => (to - from) / s <= max - 1) ?? Math.ceil((to - from) / (max - 1) / DAY_MS) * DAY_MS
  // Align on local wall-clock time, not UTC.
  const offset = -new Date(from).getTimezoneOffset() * MIN
  const ticks: number[] = []
  for (let t = Math.ceil((from + offset) / step) * step - offset; t <= to; t += step) ticks.push(t)
  return ticks
}

/** Day-only label for wide windows: "12 Oct". */
export function formatDay(ts: number): string {
  return DAY.format(ts)
}

/**
 * Smallest spacing between consecutive points, in seconds. Only a fallback for
 * servers that do not report meta.step_seconds: sparse data (an AS that only
 * appears every other bucket) makes it overestimate the step.
 */
export function inferStepSeconds(points: TrafficPoint[][] | TrafficPoint[]): number {
  const lists: TrafficPoint[][] = Array.isArray(points[0]) ? (points as TrafficPoint[][]) : [points as TrafficPoint[]]
  let minDiff = Infinity
  for (const list of lists) {
    for (let i = 1; i < list.length; i++) {
      const diff = (Date.parse(list[i].t) - Date.parse(list[i - 1].t)) / 1000
      if (diff > 0 && diff < minDiff) minDiff = diff
    }
  }
  return minDiff === Infinity ? 300 : minDiff
}
