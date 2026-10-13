import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"
import { formatBitRate } from "./rate"

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
  return formatBitRate(intervalSeconds > 0 ? (bytesPerInterval * 8) / intervalSeconds : 0)
}

// formatBitsPerSec formats an already-computed bits-per-second value (unlike
// formatBps, which converts bytes-per-interval). Used by capacity/load-curve
// data where the backend already returns bps.
export function formatBitsPerSec(bps: number): string {
  return formatBitRate(bps || 0)
}

const NUMBER_FORMAT = new Intl.NumberFormat()

export function formatNumber(n: number): string {
  return NUMBER_FORMAT.format(n)
}

export function formatPercent(pct: number): string {
  return `${pct.toFixed(1)}%`
}
