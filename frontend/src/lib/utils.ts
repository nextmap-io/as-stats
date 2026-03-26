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

export function formatNumber(n: number): string {
  return new Intl.NumberFormat().format(n)
}

export function formatPercent(pct: number): string {
  return `${pct.toFixed(1)}%`
}
