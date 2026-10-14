// Pure helpers behind the Live Threats page: plain-word labels for attack
// signatures, the "why is this flagged" sentence, now-vs-usual figures, and
// the normalisation of the two API shapes. Kept out of the components so the
// wording is unit-tested.
import type {
  LiveThreat,
  LiveThreatsData,
  ThreatDirection,
  ThreatLevel,
  ThreatNow,
  ThreatPortShare,
  ThreatRow,
  ThreatSignature,
} from "./types"
import { formatBitRate, formatRate } from "./rate"

/** A ratio below this reads as "within its usual range": no "×" figure. */
export const RATIO_SHOWN_FROM = 1.5

/** Floors the server applies before dividing by p95 (contract: 1 Mb/s, 1000 pps). */
const FLOOR_BPS = 1_000_000
const FLOOR_PPS = 1_000

// ─── Ports and services ────────────────────────────────────────────────────

/**
 * Services abused as UDP reflectors (the contract's amplification port list),
 * by source port. Port 0 is how flow exporters report non-initial fragments.
 */
export const REFLECTOR_SERVICES: Record<number, string> = {
  19: "CHARGEN",
  53: "DNS",
  111: "Portmap",
  123: "NTP",
  137: "NetBIOS",
  161: "SNMP",
  389: "CLDAP",
  1900: "SSDP",
  3283: "ARD",
  3702: "WS-Discovery",
  5353: "mDNS",
  11211: "Memcached",
  27015: "Steam",
}

/** Well-known ports worth naming in evidence lines (beyond the reflectors). */
const COMMON_SERVICES: Record<number, string> = {
  21: "FTP",
  22: "SSH",
  23: "Telnet",
  25: "SMTP",
  80: "HTTP",
  110: "POP3",
  143: "IMAP",
  443: "HTTPS",
  445: "SMB",
  465: "SMTPS",
  587: "Submission",
  993: "IMAPS",
  1433: "MSSQL",
  3306: "MySQL",
  3389: "RDP",
  5060: "SIP",
  5432: "PostgreSQL",
  8080: "HTTP alt",
}

/** "NTP" for 123/UDP, "SMTP" for 25, "" when the port has no common name. */
export function serviceName(port: number, protocol?: number): string {
  if (port === 0) return "fragments"
  // Reflector names only make sense for UDP (or when the protocol is unknown).
  if (protocol === undefined || protocol === 17) {
    const r = REFLECTOR_SERVICES[port]
    if (r) return r
  }
  return COMMON_SERVICES[port] ?? ""
}

/** "123 (NTP)", "0 (fragments)", "8443". */
export function portLabel(port: number, protocol?: number): string {
  const name = serviceName(port, protocol)
  return name ? `${port} (${name})` : String(port)
}

// ─── Signatures ────────────────────────────────────────────────────────────

const SIGNATURE_WORDS: Record<string, string> = {
  syn_flood: "SYN flood",
  udp_flood: "UDP flood",
  icmp_flood: "ICMP flood",
  amplification: "Amplification",
  small_packets: "Small packets",
  many_sources: "Many sources",
  port_scan: "Port scan",
  host_sweep: "Host sweep",
  smtp_burst: "SMTP burst",
  outbound_flood: "Outbound flood",
}

/** A signature in plain words: "SYN flood", "NTP amplification". */
export function signatureLabel(sig: ThreatSignature): string {
  if (sig.kind === "amplification") {
    const top = sig.top_ports?.[0]
    if (top) {
      if (top.port === 0) return "Fragment flood"
      const svc = REFLECTOR_SERVICES[top.port]
      if (svc) return `${svc} amplification`
    }
    return "Amplification"
  }
  const known = SIGNATURE_WORDS[sig.kind]
  if (known) return known
  const words = sig.kind.replace(/_/g, " ").trim()
  return words ? words[0].toUpperCase() + words.slice(1) : "Unusual traffic"
}

// ─── Figures ───────────────────────────────────────────────────────────────

export function formatPps(pps: number): string {
  return formatRate(pps, 1, "pps", "packets")
}

const NUMBER = new Intl.NumberFormat("en-US")
function count(n: number): string {
  return NUMBER.format(Math.round(n))
}

function pct(n: number): string {
  if (n > 0 && n < 1) return "<1%"
  return `${Math.round(n)}%`
}

/** "6.8", "20", "142": one decimal below 10. */
export function formatRatio(ratio: number): string {
  if (!Number.isFinite(ratio) || ratio <= 0) return "0"
  return ratio < 10 ? (Math.round(ratio * 10) / 10).toFixed(1) : String(Math.round(ratio))
}

/** "×6.8" when the ratio is worth showing, "" otherwise. */
export function ratioLabel(ratio: number): string {
  return ratio >= RATIO_SHOWN_FROM ? `×${formatRatio(ratio)}` : ""
}

/**
 * "usual 171 Mb/s (p95 815)": the unit is written once when both figures
 * share it, twice otherwise ("usual 800 kb/s (p95 1.20 Mb/s)").
 */
export function usualLabel(p50: number, p95: number, format: (v: number) => string): string {
  const a = format(p50)
  const b = format(p95)
  const unit = (s: string) => s.slice(s.lastIndexOf(" ") + 1)
  const p95Text = unit(a) === unit(b) ? b.slice(0, b.lastIndexOf(" ")) : b
  return `usual ${a} (p95 ${p95Text})`
}

/** Which axis is further above its usual peak: the one the row should lead with. */
export function deviatingAxis(row: Pick<ThreatRow, "now" | "baseline">): "bps" | "pps" {
  const b = row.now.bps / Math.max(row.baseline.bps_p95, FLOOR_BPS)
  const p = row.now.pps / Math.max(row.baseline.pps_p95, FLOOR_PPS)
  return p > b ? "pps" : "bps"
}

export interface ProtocolShare {
  label: "TCP" | "UDP" | "ICMP" | "Other"
  pct: number
}

/** Packet share by protocol, largest first, dropping shares under 1%. */
export function protocolMix(now: ThreatNow): ProtocolShare[] {
  const known = now.tcp_pps + now.udp_pps + now.icmp_pps
  // No per-protocol counts (pre-baseline servers): say nothing rather than "Other 100%".
  if (now.pps <= 0 || known <= 0) return []
  const shares: ProtocolShare[] = [
    { label: "TCP", pct: (now.tcp_pps / now.pps) * 100 },
    { label: "UDP", pct: (now.udp_pps / now.pps) * 100 },
    { label: "ICMP", pct: (now.icmp_pps / now.pps) * 100 },
    { label: "Other", pct: (Math.max(0, now.pps - known) / now.pps) * 100 },
  ]
  return shares.filter((s) => s.pct >= 1).sort((a, b) => b.pct - a.pct)
}

/** "UDP" when one protocol carries at least 80% of the packets, else null. */
export function dominantProtocol(now: ThreatNow): "TCP" | "UDP" | "ICMP" | null {
  const top = protocolMix(now)[0]
  return top && top.label !== "Other" && top.pct >= 80 ? top.label : null
}

function peerWord(dir: ThreatDirection, n: number): string {
  const w = dir === "in" ? "source" : "destination"
  return `${count(n)} ${w}${Math.round(n) === 1 ? "" : "s"}`
}

/** The most useful port breakdown on the row: the first signature that carries one. */
export function topPorts(row: Pick<ThreatRow, "signatures">): ThreatPortShare[] {
  for (const s of row.signatures) if (s.top_ports?.length) return s.top_ports
  return []
}

/**
 * Evidence, one fact per item: peers, peer ports, average packet size,
 * protocol mix, top ports.
 */
export function evidence(row: ThreatRow, dir: ThreatDirection): string[] {
  const n = row.now
  const out: string[] = []
  if (n.peers > 0) out.push(peerWord(dir, n.peers))
  if (n.peer_ports > 0) {
    out.push(`${count(n.peer_ports)} ${dir === "in" ? "source" : "destination"} port${Math.round(n.peer_ports) === 1 ? "" : "s"}`)
  }
  if (n.avg_packet_bytes > 0) out.push(`${count(n.avg_packet_bytes)} B per packet`)
  const mix = protocolMix(n)
  if (mix.length) out.push(mix.map((m) => `${m.label} ${pct(m.pct)}`).join(", "))
  const ports = topPorts(row)
  if (ports.length) {
    out.push(`port ${ports.slice(0, 3).map((p) => `${portLabel(p.port, p.protocol)} ${pct(p.pct)}`).join(", ")}`)
  }
  return out
}

// ─── The sentence ──────────────────────────────────────────────────────────

/**
 * One sentence on why the row is flagged, built from the figures the row
 * already shows: what the traffic is, the decisive detail of its signature,
 * and how far it sits above the host's usual peak.
 *
 *   "UDP from 980 sources, 92% from port 123 (NTP), 6.8× its usual peak."
 *
 * `legacy` rows (servers before the baseline contract) have no history, so
 * the comparison clause is left out rather than claiming "no history".
 */
export function explainThreat(row: ThreatRow, dir: ThreatDirection, legacy = false): string {
  const n = row.now
  const kinds = new Set(row.signatures.map((s) => s.kind))
  const clauses: string[] = []

  // What the traffic is.
  if (kinds.has("port_scan")) {
    clauses.push(`Probing ${count(n.peer_ports)} ports on ${count(n.peers)} host${Math.round(n.peers) === 1 ? "" : "s"}`)
  } else if (kinds.has("host_sweep")) {
    clauses.push(`Reaching ${count(n.peers)} hosts with ${count(n.avg_packet_bytes)}-byte packets`)
  } else if (kinds.has("syn_flood")) {
    const lead = `SYN packets at ${formatPps(n.syn_pps)}`
    clauses.push(n.peers > 0 ? `${lead} from ${peerWord(dir, n.peers)}` : lead)
    if (n.tcp_pps > 0) clauses.push(`${pct((n.syn_pps / n.tcp_pps) * 100)} of its TCP packets`)
  } else {
    const proto = dominantProtocol(n)
    const what = proto ?? "Traffic"
    if (n.peers > 0) clauses.push(`${what} ${dir === "in" ? "from" : "to"} ${peerWord(dir, n.peers)}`)
    else clauses.push(proto ? `${proto} traffic` : "Traffic")
  }

  // The decisive detail.
  const ports = topPorts(row)
  if (ports.length) {
    const p = ports[0]
    clauses.push(`${pct(p.pct)} ${dir === "in" ? "from" : "to"} port ${portLabel(p.port, p.protocol)}`)
  }
  if (kinds.has("small_packets") && !kinds.has("host_sweep")) {
    clauses.push(`${count(n.avg_packet_bytes)}-byte packets on average`)
  }

  // Against its usual level.
  if (!legacy) {
    if (row.baseline.samples === 0) clauses.push("no history for this host at this hour")
    else if (row.ratio >= RATIO_SHOWN_FROM) clauses.push(`${formatRatio(row.ratio)}× its usual peak`)
    else clauses.push("within its usual range")
  }

  return `${clauses.join(", ")}.`
}

// ─── Ordering and API shapes ───────────────────────────────────────────────

const LEVEL_RANK: Record<ThreatLevel, number> = { incident: 2, watch: 1, normal: 0 }

/** Most severe first, then by score. */
export function sortThreats(rows: ThreatRow[]): ThreatRow[] {
  return [...rows].sort((a, b) => LEVEL_RANK[b.level] - LEVEL_RANK[a.level] || b.score - a.score)
}

export interface ThreatsView {
  data: LiveThreatsData
  /** The server predates the baseline contract (bare LiveThreat[] response). */
  legacy: boolean
}

function fromLegacy(t: LiveThreat): ThreatRow {
  const level: ThreatLevel = t.status === "critical" ? "incident" : t.status === "warn" ? "watch" : "normal"
  return {
    ip: t.target_ip.startsWith("::ffff:") && t.target_ip.includes(".") ? t.target_ip.slice(7) : t.target_ip,
    level,
    score: t.worst_pct,
    now: {
      bps: t.bps,
      pps: t.pps,
      flows_per_sec: 0,
      tcp_pps: 0,
      udp_pps: 0,
      icmp_pps: 0,
      syn_pps: t.syn_pps,
      avg_packet_bytes: 0,
      peers: t.unique_src_ips,
      peer_ports: 0,
    },
    baseline: { samples: 0, bps_p50: 0, bps_p95: 0, pps_p50: 0, pps_p95: 0 },
    ratio: 0,
    since: "",
    series: [],
    signatures: [],
    closest_rule: t.worst_rule ?? "",
    rule_pct: t.worst_pct,
  }
}

/**
 * normalizeThreats accepts both response shapes. The legacy array is mapped
 * onto rows without baselines; when `all` is off its normal rows are dropped
 * here, as the new server does itself.
 */
export function normalizeThreats(
  raw: LiveThreatsData | LiveThreat[] | null | undefined,
  fallback: { direction: ThreatDirection; windowSeconds: number; all: boolean },
): ThreatsView {
  if (Array.isArray(raw)) {
    const rows = raw.map(fromLegacy)
    const normal = rows.filter((r) => r.level === "normal").length
    return {
      legacy: true,
      data: {
        direction: "in",
        window_seconds: fallback.windowSeconds,
        checked: rows.length,
        normal,
        rows: sortThreats(fallback.all ? rows : rows.filter((r) => r.level !== "normal")),
      },
    }
  }
  const d = raw ?? { direction: fallback.direction, window_seconds: fallback.windowSeconds, checked: 0, normal: 0, rows: [] }
  return {
    legacy: false,
    data: {
      ...d,
      rows: sortThreats(
        (d.rows ?? []).map((r) => ({ ...r, signatures: r.signatures ?? [], series: r.series ?? [] })),
      ),
    },
  }
}

// ─── Page copy ─────────────────────────────────────────────────────────────

/** "minute", "5 minutes", "hour". */
export function windowPhrase(seconds: number): string {
  if (seconds === 60) return "minute"
  if (seconds === 3600) return "hour"
  if (seconds % 3600 === 0) return `${seconds / 3600} hours`
  return `${Math.round(seconds / 60)} minutes`
}

/** The calm sentence shown when nothing is abnormal. */
export function quietSentence(checked: number, windowSeconds: number): string {
  const span = windowPhrase(windowSeconds)
  if (checked === 0) return `No traffic seen in the last ${span}.`
  return `Nothing unusual in the last ${span} — ${count(checked)} host${checked === 1 ? "" : "s"} checked against their usual level.`
}

/** "2 hosts need attention. 146 of 148 are at their usual level." */
export function attentionSentence(flagged: number, checked: number, normal: number): string {
  const head = flagged === 1 ? "1 host needs attention." : `${count(flagged)} hosts need attention.`
  if (checked <= 0) return head
  return `${head} ${count(normal)} of ${count(checked)} checked are at their usual level.`
}

/** Bits per second, re-exported so the page formats both rates from one place. */
export const formatBps = formatBitRate
