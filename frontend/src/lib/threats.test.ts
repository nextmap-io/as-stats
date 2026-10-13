import { describe, expect, it } from "vitest"
import {
  attentionSentence,
  deviatingAxis,
  dominantProtocol,
  evidence,
  explainThreat,
  formatBps,
  normalizeThreats,
  portLabel,
  protocolMix,
  quietSentence,
  ratioLabel,
  signatureLabel,
  sortThreats,
  usualLabel,
} from "./threats"
import type { LiveThreat, ThreatRow } from "./types"

function row(over: Partial<Omit<ThreatRow, "now">> & { now?: Partial<ThreatRow["now"]> } = {}): ThreatRow {
  const { now, ...rest } = over
  return {
    ip: "192.0.2.10",
    level: "watch",
    score: 1,
    ratio: 1,
    since: "",
    series: [],
    signatures: [],
    closest_rule: "",
    rule_pct: 0,
    baseline: { samples: 420, bps_p50: 20e6, bps_p95: 50e6, pps_p50: 2000, pps_p95: 5000 },
    ...rest,
    now: {
      bps: 34.1e6,
      pps: 3726,
      flows_per_sec: 120,
      tcp_pps: 3590,
      udp_pps: 131,
      icmp_pps: 2,
      syn_pps: 0,
      avg_packet_bytes: 1144,
      peers: 980,
      peer_ports: 55,
      ...now,
    },
  }
}

describe("signatureLabel", () => {
  it("names the reflector service of an amplification", () => {
    expect(signatureLabel({ kind: "amplification", top_ports: [{ port: 123, protocol: 17, pct: 92.4 }] })).toBe(
      "NTP amplification",
    )
    expect(signatureLabel({ kind: "amplification", top_ports: [{ port: 11211, protocol: 17, pct: 80 }] })).toBe(
      "Memcached amplification",
    )
    expect(signatureLabel({ kind: "amplification", top_ports: [{ port: 0, protocol: 17, pct: 70 }] })).toBe(
      "Fragment flood",
    )
    expect(signatureLabel({ kind: "amplification" })).toBe("Amplification")
  })
  it("uses plain words for every contract kind", () => {
    expect(signatureLabel({ kind: "syn_flood" })).toBe("SYN flood")
    expect(signatureLabel({ kind: "udp_flood" })).toBe("UDP flood")
    expect(signatureLabel({ kind: "port_scan" })).toBe("Port scan")
    expect(signatureLabel({ kind: "smtp_burst" })).toBe("SMTP burst")
    expect(signatureLabel({ kind: "host_sweep" })).toBe("Host sweep")
  })
  it("falls back to the kind itself for kinds it does not know", () => {
    expect(signatureLabel({ kind: "gre_flood" })).toBe("Gre flood")
  })
})

describe("portLabel", () => {
  it("names well-known ports", () => {
    expect(portLabel(123, 17)).toBe("123 (NTP)")
    expect(portLabel(25, 6)).toBe("25 (SMTP)")
    expect(portLabel(0)).toBe("0 (fragments)")
    expect(portLabel(8443, 6)).toBe("8443")
  })
  it("does not call a TCP port by its UDP reflector name", () => {
    expect(portLabel(1900, 6)).toBe("1900")
  })
})

describe("ratioLabel / usualLabel", () => {
  it("shows the ratio only from 1.5", () => {
    expect(ratioLabel(1.4)).toBe("")
    expect(ratioLabel(6.84)).toBe("×6.8")
    expect(ratioLabel(20.4)).toBe("×20")
  })
  it("writes the unit once when both figures share it", () => {
    expect(usualLabel(171.4e6, 815.2e6, formatBps)).toBe("usual 171 Mb/s (p95 815)")
    expect(usualLabel(800e3, 1.2e6, formatBps)).toBe("usual 800 kb/s (p95 1.20 Mb/s)")
  })
})

describe("deviatingAxis", () => {
  it("leads with packets when packets are further above their usual peak", () => {
    expect(deviatingAxis(row({ now: { bps: 30e6, pps: 80_000 } }))).toBe("pps")
    expect(deviatingAxis(row({ now: { bps: 500e6, pps: 4000 } }))).toBe("bps")
  })
})

describe("protocolMix", () => {
  it("orders shares and drops the negligible ones", () => {
    const mix = protocolMix(row({ now: { pps: 1000, tcp_pps: 20, udp_pps: 975, icmp_pps: 2 } }).now)
    expect(mix.map((m) => m.label)).toEqual(["UDP", "TCP"])
    expect(dominantProtocol(row({ now: { pps: 1000, tcp_pps: 20, udp_pps: 975, icmp_pps: 0 } }).now)).toBe("UDP")
    expect(dominantProtocol(row({ now: { pps: 1000, tcp_pps: 500, udp_pps: 500, icmp_pps: 0 } }).now)).toBeNull()
  })
  it("says nothing without per-protocol counts", () => {
    expect(protocolMix(row({ now: { pps: 1000, tcp_pps: 0, udp_pps: 0, icmp_pps: 0 } }).now)).toEqual([])
  })
})

describe("explainThreat", () => {
  it("explains an NTP amplification", () => {
    const r = row({
      ratio: 6.8,
      now: { pps: 30_000, udp_pps: 29_900, tcp_pps: 100, icmp_pps: 0, peers: 980 },
      signatures: [{ kind: "amplification", top_ports: [{ port: 123, protocol: 17, pct: 92.4 }] }],
    })
    expect(explainThreat(r, "in")).toBe("UDP from 980 sources, 92% from port 123 (NTP), 6.8× its usual peak.")
  })
  it("explains a SYN flood", () => {
    const r = row({
      ratio: 22,
      now: { pps: 52_000, tcp_pps: 51_000, udp_pps: 0, syn_pps: 49_000, peers: 12_000 },
      signatures: [{ kind: "syn_flood" }],
    })
    expect(explainThreat(r, "in")).toBe(
      "SYN packets at 49.0 kp/s from 12,000 sources, 96% of its TCP packets, 22× its usual peak.",
    )
  })
  it("explains an outbound port scan", () => {
    const r = row({ ratio: 14, now: { peers: 37, peer_ports: 1450 }, signatures: [{ kind: "port_scan" }] })
    expect(explainThreat(r, "out")).toBe("Probing 1,450 ports on 37 hosts, 14× its usual peak.")
  })
  it("explains a host with no signature", () => {
    const r = row({ ratio: 20, now: { pps: 1000, tcp_pps: 990, udp_pps: 10, icmp_pps: 0, peers: 1 } })
    expect(explainThreat(r, "in")).toBe("TCP from 1 source, 20× its usual peak.")
  })
  it("says when a host has no history", () => {
    const r = row({ baseline: { samples: 0, bps_p50: 0, bps_p95: 0, pps_p50: 0, pps_p95: 0 } })
    expect(explainThreat(r, "out")).toBe("TCP to 980 destinations, no history for this host at this hour.")
  })
  it("leaves the comparison out for servers without baselines", () => {
    const r = row({ now: { pps: 0, peers: 0 } })
    expect(explainThreat(r, "in", true)).toBe("Traffic.")
  })
  it("keeps a busy host at its usual level calm", () => {
    expect(explainThreat(row({ ratio: 0.4, level: "normal" }), "in")).toBe(
      "TCP from 980 sources, within its usual range.",
    )
  })
})

describe("evidence", () => {
  it("lists peers, ports, packet size, mix and top ports", () => {
    const r = row({ signatures: [{ kind: "smtp_burst", top_ports: [{ port: 25, protocol: 6, pct: 97 }] }] })
    expect(evidence(r, "out")).toEqual([
      "980 destinations",
      "55 destination ports",
      "1,144 B per packet",
      "TCP 96%, UDP 4%",
      "port 25 (SMTP) 97%",
    ])
  })
})

describe("normalizeThreats", () => {
  const legacy: LiveThreat[] = [
    { target_ip: "::ffff:192.0.2.1", bps: 1e6, pps: 100, syn_pps: 0, unique_src_ips: 3, worst_pct: 10, status: "ok" },
    { target_ip: "192.0.2.2", bps: 9e8, pps: 9e4, syn_pps: 0, unique_src_ips: 30, worst_pct: 120, worst_rule: "UDP flood", status: "critical" },
    { target_ip: "192.0.2.3", bps: 5e8, pps: 5e4, syn_pps: 0, unique_src_ips: 30, worst_pct: 60, status: "warn" },
  ]
  it("maps the pre-baseline array and drops normal rows unless asked", () => {
    const v = normalizeThreats(legacy, { direction: "in", windowSeconds: 300, all: false })
    expect(v.legacy).toBe(true)
    expect(v.data.checked).toBe(3)
    expect(v.data.normal).toBe(1)
    expect(v.data.rows.map((r) => [r.ip, r.level])).toEqual([
      ["192.0.2.2", "incident"],
      ["192.0.2.3", "watch"],
    ])
    expect(v.data.rows[0].closest_rule).toBe("UDP flood")
    const all = normalizeThreats(legacy, { direction: "in", windowSeconds: 300, all: true })
    expect(all.data.rows[2].ip).toBe("192.0.2.1")
  })
  it("passes the contract shape through, most severe first", () => {
    const v = normalizeThreats(
      {
        direction: "out",
        window_seconds: 60,
        checked: 2,
        normal: 0,
        rows: [row({ ip: "a", level: "watch", score: 9 }), row({ ip: "b", level: "incident", score: 2 })],
      },
      { direction: "out", windowSeconds: 60, all: false },
    )
    expect(v.legacy).toBe(false)
    expect(v.data.rows.map((r) => r.ip)).toEqual(["b", "a"])
  })
  it("survives an empty response", () => {
    expect(normalizeThreats(undefined, { direction: "in", windowSeconds: 300, all: false }).data.rows).toEqual([])
  })
})

describe("page sentences", () => {
  it("reads calmly when nothing is wrong", () => {
    expect(quietSentence(148, 300)).toBe(
      "Nothing unusual in the last 5 minutes — 148 hosts checked against their usual level.",
    )
    expect(quietSentence(0, 60)).toBe("No traffic seen in the last minute.")
  })
  it("counts what needs attention", () => {
    expect(attentionSentence(2, 148, 146)).toBe("2 hosts need attention. 146 of 148 checked are at their usual level.")
  })
  it("sorts by level then score", () => {
    const rows = sortThreats([
      row({ ip: "n", level: "normal", score: 50 }),
      row({ ip: "w", level: "watch", score: 3 }),
      row({ ip: "i", level: "incident", score: 1 }),
    ])
    expect(rows.map((r) => r.ip)).toEqual(["i", "w", "n"])
  })
})
