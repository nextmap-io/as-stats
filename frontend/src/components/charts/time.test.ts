import { describe, expect, it } from "vitest"
import { timeTicks } from "./time"

describe("timeTicks", () => {
  it("puts a day's ticks on round local hours", () => {
    const from = new Date(2026, 9, 26, 20, 30).getTime()
    const ticks = timeTicks(from, from + 24 * 3600_000)
    expect(ticks.map((t) => new Date(t).getHours())).toEqual([0, 4, 8, 12, 16, 20])
    expect(ticks.every((t) => new Date(t).getMinutes() === 0)).toBe(true)
  })
  it("uses minutes on short windows and stays inside the window", () => {
    const from = new Date(2026, 9, 27, 20, 7).getTime()
    const to = from + 3600_000
    const ticks = timeTicks(from, to)
    expect(ticks.map((t) => new Date(t).getMinutes())).toEqual([10, 20, 30, 40, 50, 0])
    expect(ticks.every((t) => t >= from && t <= to)).toBe(true)
  })
  it("returns nothing for an empty window", () => {
    expect(timeTicks(10, 10)).toEqual([])
  })
})
