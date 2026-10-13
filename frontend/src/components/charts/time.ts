import type { TrafficPoint } from "@/lib/types"

const TIME = new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit" })
const DAY_TIME = new Intl.DateTimeFormat(undefined, { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" })
const DAY = new Intl.DateTimeFormat(undefined, { day: "numeric", month: "short" })

/** Axis / tooltip time label: "20:00", or "12 Oct, 20:00" on multi-day windows. */
export function formatTimeShort(ts: number, multiDay: boolean): string {
  return multiDay ? DAY_TIME.format(ts) : TIME.format(ts)
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
