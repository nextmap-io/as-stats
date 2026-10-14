import type { ResponseMeta } from "./types"

/** What a raw value counts: bytes (→ b/s) or packets (→ p/s). */
export type RateKind = "bytes" | "packets"
export type RateUnit = "bps" | "pps" | "bytes"

/**
 * The bucket width older servers (< 3.1) did not report. This mirrors the
 * guess the UI used to make from the window length. It is only a fallback:
 * it is wrong whenever the backend reads a coarser table than the window
 * suggests (7d/30d read hourly buckets), which made p95 read 4–24× too low.
 */
export function legacyBucketSeconds(periodSeconds: number): number {
  if (periodSeconds <= 10800) return 60 // <= 3h: 1 min
  if (periodSeconds <= 21600) return 120 // <= 6h: 2 min
  if (periodSeconds <= 129600) return 300 // <= 36h: 5 min
  if (periodSeconds <= 604800) return 3600 // <= 7d: 1h
  return 86400 // > 7d: 24h
}

/**
 * stepSecondsOf returns the bucket width to divide per-bucket bytes by: the
 * server's meta.step_seconds when present (3.1+), the legacy guess otherwise.
 */
export function stepSecondsOf(meta: ResponseMeta | undefined, periodSeconds: number): number {
  const s = meta?.step_seconds
  return s && s > 0 ? s : legacyBucketSeconds(periodSeconds)
}

/** Bytes counted over one bucket of `stepSeconds`, as bits per second. */
export function bytesToBps(bytes: number, stepSeconds: number): number {
  return stepSeconds > 0 ? (bytes * 8) / stepSeconds : 0
}

const RATE_UNITS = ["", "k", "M", "G", "T"]

/** 1234567 → { value: "1.23", unit: "M" } (decimal SI). */
export function siParts(v: number): { value: string; prefix: string } {
  if (!Number.isFinite(v) || v <= 0) return { value: "0", prefix: "" }
  const i = Math.min(Math.max(0, Math.floor(Math.log10(v) / 3)), RATE_UNITS.length - 1)
  const n = v / Math.pow(1000, i)
  const value = n < 10 ? n.toFixed(2) : n < 100 ? n.toFixed(1) : Math.round(n).toString()
  return { value, prefix: RATE_UNITS[i] }
}

/** Formats bits per second: "860 Mb/s", "1.21 Gb/s". */
export function formatBitRate(bps: number): string {
  if (bps > 0 && bps < 1) return "<1 b/s"
  const { value, prefix } = siParts(bps)
  return `${value} ${prefix}b/s`
}

function formatPacketRate(pps: number): string {
  if (pps > 0 && pps < 1) return "<1 p/s"
  const { value, prefix } = siParts(pps)
  return `${value} ${prefix}p/s`
}

function formatVolume(bytes: number): string {
  if (bytes > 0 && bytes < 1) return "<1 B"
  const { value, prefix } = siParts(bytes)
  return `${value} ${prefix}B`
}

/**
 * formatRate renders `value` (bytes or packets counted over `intervalSeconds`)
 * in the selected unit. Packet rates need packet counts: a byte value in pps
 * mode is shown in b/s instead of being mislabelled as packets.
 */
export function formatRate(value: number, intervalSeconds: number, unit: RateUnit, kind: RateKind): string {
  const secs = intervalSeconds > 0 ? intervalSeconds : 1
  if (unit === "bytes") return kind === "packets" ? `${Math.round(value).toLocaleString()} pkts` : formatVolume(value)
  if (kind === "packets") return formatPacketRate(value / secs)
  return formatBitRate((value * 8) / secs)
}

/** Compact axis label: "600M", "1.2G". */
export function formatRateAxis(value: number, intervalSeconds: number, unit: RateUnit, kind: RateKind): string {
  const secs = intervalSeconds > 0 ? intervalSeconds : 1
  const v = unit === "bytes" ? value : kind === "packets" ? value / secs : (value * 8) / secs
  if (v < 1) return "0"
  const i = Math.min(Math.max(0, Math.floor(Math.log10(v) / 3)), RATE_UNITS.length - 1)
  const n = v / Math.pow(1000, i)
  const label = n < 10 ? (Math.round(n * 10) / 10).toString() : Math.round(n).toString()
  return `${label}${RATE_UNITS[i].toUpperCase()}`
}
