import { describe, expect, it } from "vitest"
import { formatBitRate, formatRate, formatRateAxis, legacyBucketSeconds, niceStep, rateAxisTicks, stepSecondsOf } from "./rate"
import { ourView } from "./asn"

describe("stepSecondsOf", () => {
  it("uses the server's step when present", () => {
    expect(stepSecondsOf({ from: "", to: "", step_seconds: 3600 }, 7 * 86400)).toBe(3600)
  })
  it("falls back to the legacy guess on older servers", () => {
    expect(stepSecondsOf(undefined, 86400)).toBe(300)
    expect(stepSecondsOf({ from: "", to: "" }, 30 * 86400)).toBe(legacyBucketSeconds(30 * 86400))
  })
})

describe("formatRate", () => {
  it("formats bits per second", () => {
    expect(formatBitRate(860_000_000)).toBe("860 Mb/s")
    expect(formatBitRate(2_096_000_000)).toBe("2.10 Gb/s")
    expect(formatBitRate(0)).toBe("0 b/s")
  })
  it("shows packet rates only from packet counts", () => {
    // 3000 packets over 60 s = 50 p/s
    expect(formatRate(3000, 60, "pps", "packets")).toBe("50.0 p/s")
    // A byte count in pps mode stays a bit rate instead of being mislabelled.
    expect(formatRate(7_500_000, 60, "pps", "bytes")).toBe("1.00 Mb/s")
  })
})

describe("rateAxisTicks", () => {
  const label = (t: number, secs: number) => (t < 0 ? "-" : "") + formatRateAxis(Math.abs(t), secs, "bps", "bytes")
  it("puts ticks on round rates, not round byte counts", () => {
    // 5-min buckets, peak 920 Mb/s
    const { ticks, domain } = rateAxisTicks(0, (920e6 / 8) * 300, 300, "bps", "bytes")
    expect(ticks.map((t) => label(t, 300))).toEqual(["0", "250M", "500M", "750M", "1G"])
    expect(domain[1]).toBe(ticks[ticks.length - 1])
  })
  it("uses one step on both sides of a mirrored chart", () => {
    const { ticks } = rateAxisTicks(-(300e6 / 8) * 60, (900e6 / 8) * 60, 60, "bps", "bytes")
    expect(ticks.map((t) => label(t, 60))).toEqual(["-500M", "0", "500M", "1G"])
  })
  it("survives an empty series", () => {
    expect(rateAxisTicks(0, 0, 60, "bps", "bytes")).toEqual({ ticks: [0], domain: [0, 1] })
  })
  it("rounds steps to 1, 2, 2.5 or 5", () => {
    expect([0.7, 1.4, 2.2, 3, 7].map(niceStep)).toEqual([1, 2, 2.5, 5, 10])
  })
})

describe("ourView", () => {
  it("flips /top/as totals to your side", () => {
    expect(ourView({ bytes_in: 10, bytes_out: 3 })).toEqual({ inBytes: 3, outBytes: 10 })
  })
})
