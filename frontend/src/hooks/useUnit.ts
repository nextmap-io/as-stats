import { createContext, useContext, useState, useCallback } from "react"

export type TrafficUnit = "bps" | "bytes" | "pps"

interface UnitContextType {
  unit: TrafficUnit
  toggleUnit: () => void
  formatTraffic: (bytes: number, intervalSeconds?: number) => string
  formatPackets: (packets: number, intervalSeconds?: number) => string
  formatAxis: (bytes: number, intervalSeconds?: number) => string
}

const UnitContext = createContext<UnitContextType | null>(null)

export const UnitProvider = UnitContext.Provider

const CYCLE: TrafficUnit[] = ["bps", "pps", "bytes"]

export function useUnitState(): UnitContextType {
  const [unit, setUnit] = useState<TrafficUnit>(() => {
    try {
      return (localStorage.getItem("as-stats-unit") as TrafficUnit) || "bps"
    } catch {
      return "bps"
    }
  })

  const toggleUnit = useCallback(() => {
    setUnit(prev => {
      const idx = CYCLE.indexOf(prev)
      const next = CYCLE[(idx + 1) % CYCLE.length]
      try { localStorage.setItem("as-stats-unit", next) } catch { /* noop */ }
      return next
    })
  }, [])

  const formatTraffic = useCallback((bytes: number, intervalSeconds = 300) => {
    if (unit === "bps") {
      const bps = (bytes * 8) / intervalSeconds
      if (bps < 1) return bps === 0 ? "0 bps" : "<1 bps"
      const units = ["bps", "Kbps", "Mbps", "Gbps", "Tbps"]
      const i = Math.min(Math.max(0, Math.floor(Math.log(bps) / Math.log(1000))), units.length - 1)
      const val = bps / Math.pow(1000, i)
      return `${val < 10 ? val.toFixed(1) : Math.round(val)} ${units[i]}`
    }
    if (unit === "pps") {
      const pps = bytes / intervalSeconds
      if (pps < 1) return pps === 0 ? "0 pps" : "<1 pps"
      const units = ["pps", "Kpps", "Mpps", "Gpps"]
      const i = Math.min(Math.max(0, Math.floor(Math.log(pps) / Math.log(1000))), units.length - 1)
      const val = pps / Math.pow(1000, i)
      return `${val < 10 ? val.toFixed(1) : Math.round(val)} ${units[i]}`
    }
    if (bytes < 1) return bytes === 0 ? "0 B" : "<1 B"
    const units = ["B", "KB", "MB", "GB", "TB", "PB"]
    const i = Math.min(Math.max(0, Math.floor(Math.log(bytes) / Math.log(1000))), units.length - 1)
    const val = bytes / Math.pow(1000, i)
    return `${val < 10 ? val.toFixed(1) : Math.round(val)} ${units[i]}`
  }, [unit])

  const formatPackets = useCallback((packets: number, intervalSeconds = 300) => {
    if (unit === "pps" || unit === "bps") {
      const pps = packets / intervalSeconds
      if (pps === 0) return "0 pps"
      const units = ["pps", "Kpps", "Mpps", "Gpps"]
      const i = Math.min(Math.floor(Math.log(pps) / Math.log(1000)), units.length - 1)
      const val = pps / Math.pow(1000, i)
      return `${val < 10 ? val.toFixed(1) : Math.round(val)} ${units[i]}`
    }
    return new Intl.NumberFormat().format(packets)
  }, [unit])

  // Short format for chart axis labels (no space, compact units)
  const formatAxis = useCallback((bytes: number, intervalSeconds = 300) => {
    if (unit === "bps") {
      const bps = (bytes * 8) / intervalSeconds
      if (bps < 1) return "0"
      const u = ["", "K", "M", "G", "T"]
      const i = Math.min(Math.max(0, Math.floor(Math.log(bps) / Math.log(1000))), u.length - 1)
      const val = bps / Math.pow(1000, i)
      return `${val < 10 ? val.toFixed(1) : Math.round(val)}${u[i]}`
    }
    if (unit === "pps") {
      const pps = bytes / intervalSeconds
      if (pps < 1) return "0"
      const u = ["", "K", "M", "G"]
      const i = Math.min(Math.max(0, Math.floor(Math.log(pps) / Math.log(1000))), u.length - 1)
      const val = pps / Math.pow(1000, i)
      return `${val < 10 ? val.toFixed(1) : Math.round(val)}${u[i]}`
    }
    if (bytes < 1) return "0"
    const u = ["B", "K", "M", "G", "T"]
    const i = Math.min(Math.max(0, Math.floor(Math.log(bytes) / Math.log(1000))), u.length - 1)
    const val = bytes / Math.pow(1000, i)
    return `${val < 10 ? val.toFixed(1) : Math.round(val)}${u[i]}`
  }, [unit])

  return { unit, toggleUnit, formatTraffic, formatPackets, formatAxis }
}

export function useUnit(): UnitContextType {
  const ctx = useContext(UnitContext)
  if (!ctx) throw new Error("useUnit must be used within UnitProvider")
  return ctx
}
