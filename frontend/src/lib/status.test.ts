import { describe, expect, it } from "vitest"
import { liveStatusFromLegacy, routerStateFromAge } from "./status"

describe("status", () => {
  const now = Date.parse("2026-10-13T12:00:00Z")
  it("classifies router age", () => {
    expect(routerStateFromAge("2026-10-13T11:59:30Z", now)).toBe("ok")
    expect(routerStateFromAge("2026-10-13T11:55:00Z", now)).toBe("stale")
    expect(routerStateFromAge("2026-10-13T11:00:00Z", now)).toBe("missing")
    expect(routerStateFromAge("", now)).toBe("missing")
  })
  it("derives a live status from the legacy windowed /status", () => {
    const s = liveStatusFromLegacy(
      {
        routers: [
          { router_ip: "::ffff:10.0.0.1", last_seen: "2026-10-13T11:59:50Z", flow_count: 600 },
          { router_ip: "2a09:8740::4", last_seen: "2026-10-13T11:50:00Z", flow_count: 60 },
        ],
        total_rows: 0,
        db_size: 0,
      },
      now,
    )
    expect(s.routers[0].router_ip).toBe("10.0.0.1")
    expect(s.routers[1].state).toBe("missing")
    expect(s.healthy).toBe(false)
    expect(s.disk_used_pct).toBeNull()
  })
})
