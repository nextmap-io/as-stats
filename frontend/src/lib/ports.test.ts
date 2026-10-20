import { describe, expect, it } from "vitest"
import { flowSearchPath, portHistoryNote, portLabel } from "./ports"
import { filtersFromURL } from "./flowSearchUrl"

describe("Top Ports drill-down", () => {
  it("searches the service port on either side and keeps the page filters", () => {
    const path = flowSearchPath("?period=24h&link=transit-a", 6, 443)
    expect(path.startsWith("/flows?")).toBe(true)
    const q = new URLSearchParams(path.slice("/flows?".length))
    expect(q.get("period")).toBe("24h")
    expect(q.get("link")).toBe("transit-a")
    expect(q.get("protocol")).toBe("6")
    expect(q.get("port")).toBe("443")
    expect(q.has("dst_port") || q.has("src_port")).toBe(false)
  })
  it("has no port filter for ICMP (port 0)", () => {
    const q = new URLSearchParams(flowSearchPath("", 1, 0).split("?")[1])
    expect(q.get("protocol")).toBe("1")
    expect(q.has("port")).toBe(false)
  })
  it("is read back by Flow Search", () => {
    const f = filtersFromURL(new URLSearchParams(flowSearchPath("?period=24h", 17, 53).split("?")[1]))
    expect(f).toMatchObject({ protocol: 17, port: 53, period: "24h" })
  })
})

describe("filtersFromURL", () => {
  it("ignores links without a search key", () => {
    expect(filtersFromURL(new URLSearchParams("period=7d"))).toBeNull()
  })
  it("reads AS and port filters, and an explicit range over the period", () => {
    const f = filtersFromURL(new URLSearchParams("src_as=13335&dst_port=25&from=2026-10-01T00:00:00Z&to=2026-10-02T00:00:00Z&period=1h"))
    expect(f).toMatchObject({ src_as: 13335, dst_port: 25, from: "2026-10-01T00:00:00Z", to: "2026-10-02T00:00:00Z" })
    expect(f?.period).toBeUndefined()
  })
  it("rejects junk", () => {
    const f = filtersFromURL(new URLSearchParams("dst_ip=192.0.2.1&port=-3&protocol=999&period=1y"))
    expect(f).toEqual({ limit: 100, dst_ip: "192.0.2.1", period: "1h" })
  })
})

describe("port labels and history note", () => {
  it("renders port 0 as a dash", () => {
    expect(portLabel(0)).toBe("—")
    expect(portLabel(443)).toBe("443")
  })
  it("says nothing without a restart", () => {
    expect(portHistoryNote(undefined)).toBeNull()
    expect(portHistoryNote("not a date")).toBeNull()
  })
  it("dates the restart, in UTC", () => {
    expect(portHistoryNote("2026-10-21T08:05:00Z")).toContain("2026-10-21 08:05 UTC")
    expect(portHistoryNote("2026-10-21T08:05:00Z", true)).toContain("prior window")
  })
})
