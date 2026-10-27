import { describe, expect, it } from "vitest"
import { mergeLinkSeries, parseIPFamily } from "./series"

describe("mergeLinkSeries", () => {
  it("adds families per link and per bucket", () => {
    const v4 = [
      { link_tag: "A", description: "Transit A", points: [{ t: "2026-10-27T10:00:00Z", bytes_in: 10, bytes_out: 1, packets_in: 2 }] },
    ]
    const v6 = [
      {
        link_tag: "A",
        description: "",
        points: [
          { t: "2026-10-27T09:55:00Z", bytes_in: 7, bytes_out: 0 },
          { t: "2026-10-27T10:00:00Z", bytes_in: 5, bytes_out: 2, packets_in: 1 },
        ],
      },
      { link_tag: "B", description: "IX", points: [{ t: "2026-10-27T10:00:00Z", bytes_in: 3, bytes_out: 3 }] },
    ]
    const merged = mergeLinkSeries(v4, v6)
    expect(merged.map((s) => s.link_tag)).toEqual(["A", "B"])
    expect(merged[0].description).toBe("Transit A")
    expect(merged[0].points).toEqual([
      { t: "2026-10-27T09:55:00Z", bytes_in: 7, bytes_out: 0 },
      { t: "2026-10-27T10:00:00Z", bytes_in: 15, bytes_out: 3, packets_in: 3 },
    ])
  })
  it("tolerates a missing family", () => {
    expect(mergeLinkSeries(undefined, [])).toEqual([])
  })
})

describe("parseIPFamily", () => {
  it("defaults to both", () => {
    expect([parseIPFamily("4"), parseIPFamily("6"), parseIPFamily(null), parseIPFamily("x")]).toEqual(["4", "6", "both", "both"])
  })
})
