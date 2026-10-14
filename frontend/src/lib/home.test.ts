import { describe, expect, it } from "vitest"
import {
  alertTargetLabel,
  alertsLevel,
  buildLinkRows,
  completeBuckets,
  directionStats,
  hostTarget,
  seriesByLink,
  sumSeries,
  topAlert,
  utilLevel,
} from "./home"
import type { Alert, LinkCapacity, LinkTimeSeries } from "./types"

const T0 = Date.parse("2026-10-13T10:00:00Z")
const iso = (minutes: number) => new Date(T0 + minutes * 60_000).toISOString()

function series(tag: string, values: [number, number][], step = 5): LinkTimeSeries {
  return {
    link_tag: tag,
    description: tag,
    points: values.map(([i, o], k) => ({ t: iso(k * step), bytes_in: i, bytes_out: o })),
  }
}

describe("sumSeries / seriesByLink", () => {
  it("sums IPv4 and IPv6 series of the same link per timestamp", () => {
    const v4 = series("A", [[100, 10], [200, 20]])
    const v6 = series("A", [[1, 1], [2, 2]])
    const other = series("B", [[5, 5]])
    expect(sumSeries([v4, v6])).toEqual([
      { t: iso(0), bytes_in: 101, bytes_out: 11, packets_in: 0, packets_out: 0 },
      { t: iso(5), bytes_in: 202, bytes_out: 22, packets_in: 0, packets_out: 0 },
    ])
    const byLink = seriesByLink([v4, other, v6])
    expect(byLink.get("A")?.[1].bytes_in).toBe(202)
    expect(byLink.get("B")?.length).toBe(1)
  })
})

describe("directionStats", () => {
  // 5-minute buckets; 375 MB per bucket = 10 Mb/s.
  const MB375 = 375_000_000
  const pts = series("A", [[MB375, 0], [2 * MB375, 0], [MB375, 0]]).points

  it("uses the step for rates and skips a trailing partial bucket for now/peak", () => {
    // Window ends 2 minutes into the third bucket: it is partial.
    const end = T0 + 12 * 60_000
    const s = directionStats(pts, "in", 300, end, 900)
    expect(s.now).toBeCloseTo(20_000_000) // second bucket, the last complete one
    expect(s.peak).toBeCloseTo(20_000_000)
    expect(s.peakAt).toBe(T0 + 5 * 60_000)
    expect(s.volume).toBe(4 * MB375) // volume still counts every bucket
    expect(s.avg).toBeCloseTo((4 * MB375 * 8) / 900)
  })

  it("an hourly step makes the same bytes a 12x lower rate than a 5-minute one", () => {
    const end = T0 + 3 * 3_600_000
    const five = directionStats(pts, "in", 300, end, 900).peak
    const hour = directionStats(pts, "in", 3600, end, 900).peak
    expect(five / hour).toBeCloseTo(12)
  })

  it("keeps a complete trailing bucket", () => {
    expect(completeBuckets(pts, 300, T0 + 15 * 60_000)).toHaveLength(3)
    expect(completeBuckets(pts, 300, T0 + 14 * 60_000)).toHaveLength(2)
  })
})

describe("buildLinkRows", () => {
  const cap = (c: Partial<LinkCapacity>): LinkCapacity => ({
    tag: "A",
    description: "Link A",
    capacity_mbps: 1000,
    current_bps: 0,
    p95_bps: 0,
    utilization_pct: null,
    ...c,
  })

  it("prefers the server's busier-direction fields when present", () => {
    const rows = buildLinkRows(
      [cap({ current_in_bps: 100e6, current_out_bps: 300e6, peak_in_bps: 200e6, peak_out_bps: 750e6, peak_utilization_pct: 75 })],
      new Map(),
      300,
      T0,
      86400,
    )
    expect(rows[0].nowPct).toBeCloseTo(30)
    expect(rows[0].peakPct).toBe(75)
    expect(rows[0].name).toBe("Link A")
  })

  it("derives now and peak from the series on older servers, never in+out", () => {
    const MB375 = 375_000_000 // 10 Mb/s over 5 minutes
    const s = new Map([["A", series("A", [[MB375 * 30, MB375 * 50], [MB375 * 20, MB375 * 40]]).points]])
    const rows = buildLinkRows([cap({ capacity_mbps: 1000 })], s, 300, T0 + 10 * 60_000, 600)
    expect(rows[0].peakPct).toBeCloseTo(50) // busier direction (out) 500 Mb/s of 1 Gb/s
    expect(rows[0].nowPct).toBeCloseTo(40)
  })

  it("sorts worst peak first and puts links without capacity last", () => {
    const rows = buildLinkRows(
      [
        cap({ tag: "low", peak_utilization_pct: 10 }),
        cap({ tag: "none", capacity_mbps: 0, peak_utilization_pct: null }),
        cap({ tag: "high", peak_utilization_pct: 80 }),
      ],
      new Map(),
      300,
      T0,
      86400,
    )
    expect(rows.map((r) => r.tag)).toEqual(["high", "low", "none"])
    expect(utilLevel(rows[0].peakPct)).toBe("watch")
    expect(utilLevel(95)).toBe("critical")
    expect(utilLevel(null)).toBe("normal")
  })
})

describe("alerts", () => {
  const alert = (a: Partial<Alert>): Alert => ({
    id: "1",
    rule_id: "r",
    rule_name: "rule",
    severity: "info",
    triggered_at: iso(0),
    last_seen_at: iso(0),
    target_ip: "::ffff:85.208.145.157",
    metric_value: 1,
    threshold: 1,
    metric_type: "bps",
    status: "active",
    ...a,
  })

  it("picks the most severe, then most recent alert", () => {
    const a = alert({ id: "a", severity: "info", last_seen_at: iso(10) })
    const b = alert({ id: "b", severity: "warning", last_seen_at: iso(1) })
    const c = alert({ id: "c", severity: "warning", last_seen_at: iso(5) })
    expect(topAlert([a, b, c])?.id).toBe("c")
  })

  it("keeps info alerts quiet", () => {
    expect(alertsLevel({ info: 3 })).toBe("normal")
    expect(alertsLevel({ info: 1, warning: 1 })).toBe("watch")
    expect(alertsLevel({ critical: 1 })).toBe("critical")
  })

  it("never treats a synthetic link-alert address as a host", () => {
    expect(hostTarget(alert({}))).toBe("85.208.145.157")
    expect(hostTarget(alert({ target_ip: "100::a1b2:c3d4:e5f6:789" }))).toBeNull()
    expect(alertTargetLabel(alert({ target_ip: "100::1", details: '{"extra":{"target":"ORANGE-PA6"}}' }))).toBe("ORANGE-PA6")
    expect(alertTargetLabel(alert({ target_ip: "100::1", details: "not json" }))).toBe("no single host")
  })
})
