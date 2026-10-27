import { describe, expect, it } from "vitest"
import { ApiError } from "./api"
import { blockDurations, blockErrorMessage, blockSource, blockTarget, durationPhrase, relativeTime } from "./block"

describe("block helpers", () => {
  it("explains refusals", () => {
    expect(blockErrorMessage(new ApiError("BGP blackholing is not enabled", 409))).toMatch(/not enabled/)
    expect(blockErrorMessage(new ApiError("this alert does not target a single IP", 422))).toMatch(/does not target a single IP/)
    expect(blockErrorMessage(new ApiError("203.0.113.1/32 is outside the allowed prefixes (BGP_ALLOWED_PREFIXES)", 422))).toBe(
      "Refused by the safety rules: 203.0.113.1/32 is outside the allowed prefixes (BGP_ALLOWED_PREFIXES).",
    )
    expect(blockErrorMessage(new ApiError("50 blocks are already active (BGP_MAX_ACTIVE_BLOCKS=50)", 429))).toMatch(/^Limit reached: 50 blocks/)
    expect(blockErrorMessage(new ApiError("BGP daemon command failed: gobgp failed: exit status 1 (details in the API log)", 502))).toBe(
      "The BGP daemon refused the command (gobgp failed: exit status 1 (details in the API log)). Nothing was blackholed.",
    )
    expect(blockErrorMessage(new ApiError("x", 503))).toMatch(/not loaded/)
    expect(blockErrorMessage(new ApiError("forbidden", 403))).toMatch(/administrators/)
  })

  it("cuts durations at the server maximum", () => {
    expect(blockDurations(1440).map((d) => d.value)).toEqual([15, 60, 240, 1440])
    expect(blockDurations(240).map((d) => d.value)).toEqual([15, 60, 240])
    expect(blockDurations(5)).toEqual([{ value: 5, label: "5 min" }])
  })

  it("names targets and sources", () => {
    expect(blockTarget({ ip: "198.51.100.10", prefix_len: 32 })).toBe("198.51.100.10")
    expect(blockTarget({ ip: "198.51.100.16", prefix_len: 28 })).toBe("198.51.100.16/28")
    expect(blockTarget({ ip: "2001:db8::1", prefix_len: 128 })).toBe("2001:db8::1")
    expect(blockTarget({ ip: "2001:db8::", prefix_len: 64 })).toBe("2001:db8::/64")
    expect(blockSource({ reason: "auto_block", rule_name: "UDP flood" })).toBe("Automatic, rule “UDP flood”")
    expect(blockSource({ reason: "alert", rule_name: "" })).toBe("From an alert")
    expect(blockSource({ reason: "manual" })).toBe("Manual")
  })

  it("formats times", () => {
    const now = Date.parse("2026-10-27T12:00:00Z")
    expect(relativeTime("2026-10-27T12:42:00Z", now)).toBe("in 42 min")
    expect(relativeTime("2026-10-27T09:00:00Z", now)).toBe("3 h ago")
    expect(relativeTime("2026-10-27T12:00:20Z", now)).toBe("in <1 min")
    expect(durationPhrase(60)).toBe("1 hour")
    expect(durationPhrase(240)).toBe("4 hours")
    expect(durationPhrase(15)).toBe("15 minutes")
  })
})
