import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

export function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B"
  const units = ["B", "KB", "MB", "GB", "TB", "PB"]
  const i = Math.floor(Math.log(bytes) / Math.log(1000))
  const val = bytes / Math.pow(1000, i)
  return `${val.toFixed(val < 10 ? 2 : 1)} ${units[i]}`
}

export function formatBps(bytesPerInterval: number, intervalSeconds: number): string {
  const bps = (bytesPerInterval * 8) / intervalSeconds
  if (bps === 0) return "0 bps"
  const units = ["bps", "Kbps", "Mbps", "Gbps", "Tbps"]
  const i = Math.floor(Math.log(bps) / Math.log(1000))
  const val = bps / Math.pow(1000, i)
  return `${val.toFixed(val < 10 ? 2 : 1)} ${units[i]}`
}

// formatBitsPerSec formats an already-computed bits-per-second value (unlike
// formatBps, which converts bytes-per-interval). Used by capacity/load-curve
// data where the backend already returns bps.
export function formatBitsPerSec(bps: number): string {
  if (!bps || bps < 1) return "0 bps"
  const units = ["bps", "Kbps", "Mbps", "Gbps", "Tbps"]
  const i = Math.min(Math.max(0, Math.floor(Math.log(bps) / Math.log(1000))), units.length - 1)
  const val = bps / Math.pow(1000, i)
  return `${val < 10 ? val.toFixed(2) : val < 100 ? val.toFixed(1) : Math.round(val)} ${units[i]}`
}

const NUMBER_FORMAT = new Intl.NumberFormat()

export function formatNumber(n: number): string {
  return NUMBER_FORMAT.format(n)
}

export function formatPercent(pct: number): string {
  return `${pct.toFixed(1)}%`
}
