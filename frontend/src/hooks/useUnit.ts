import { createContext, useContext, useState, useCallback } from "react"

export type TrafficUnit = "bytes" | "bps"

interface UnitContextType {
  unit: TrafficUnit
  toggleUnit: () => void
  formatTraffic: (bytes: number, intervalSeconds?: number) => string
}

const UnitContext = createContext<UnitContextType | null>(null)

export const UnitProvider = UnitContext.Provider

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
      const next = prev === "bytes" ? "bps" : "bytes"
      try { localStorage.setItem("as-stats-unit", next) } catch { /* noop */ }
      return next
    })
  }, [])

  const formatTraffic = useCallback((bytes: number, intervalSeconds = 300) => {
    if (unit === "bps") {
      const bps = (bytes * 8) / intervalSeconds
      if (bps === 0) return "0 bps"
      const units = ["bps", "Kbps", "Mbps", "Gbps", "Tbps"]
      const i = Math.min(Math.floor(Math.log(bps) / Math.log(1000)), units.length - 1)
      const val = bps / Math.pow(1000, i)
      return `${val.toFixed(val < 10 ? 2 : 1)} ${units[i]}`
    }
    if (bytes === 0) return "0 B"
    const units = ["B", "KB", "MB", "GB", "TB", "PB"]
    const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1000)), units.length - 1)
    const val = bytes / Math.pow(1000, i)
    return `${val.toFixed(val < 10 ? 2 : 1)} ${units[i]}`
  }, [unit])

  return { unit, toggleUnit, formatTraffic }
}

export function useUnit(): UnitContextType {
  const ctx = useContext(UnitContext)
  if (!ctx) throw new Error("useUnit must be used within UnitProvider")
  return ctx
}
