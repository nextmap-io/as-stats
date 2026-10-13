import { describe, expect, it } from "vitest"
import { formatBitRate, formatRate, legacyBucketSeconds, stepSecondsOf } from "./rate"
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

describe("ourView", () => {
  it("flips /top/as totals to your side", () => {
    expect(ourView({ bytes_in: 10, bytes_out: 3 })).toEqual({ inBytes: 3, outBytes: 10 })
  })
})
