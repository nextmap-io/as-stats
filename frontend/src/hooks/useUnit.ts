import { createContext, useContext, useState, useCallback } from "react"
import { formatRate, formatRateAxis, type RateKind } from "@/lib/rate"

const PACKET_NUM_FORMAT = new Intl.NumberFormat()

export type TrafficUnit = "bps" | "bytes" | "pps"

interface UnitContextType {
  unit: TrafficUnit
  toggleUnit: () => void
  /**
   * Formats a byte volume in the selected unit: a rate over `intervalSeconds`
   * (b/s), a packet rate (p/s), or a plain volume (bytes).
   *
   * Packets-per-second needs PACKET counts. Pass them as `packets` wherever the
   * data has them; without them the value is shown in b/s rather than dividing
   * bytes by seconds and calling it pps.
   */
  formatTraffic: (bytes: number, intervalSeconds?: number, packets?: number) => string
  formatPackets: (packets: number, intervalSeconds?: number) => string
  /**
   * Short axis label. `kind` says what `value` counts: charts plot packets when
   * the unit is pps and the series carries packet counts, bytes otherwise.
   */
  formatAxis: (value: number, intervalSeconds?: number, kind?: RateKind) => string
  /** Tooltip / label value for a chart point (see formatAxis for `kind`). */
  formatValue: (value: number, intervalSeconds?: number, kind?: RateKind) => string
}

const UnitContext = createContext<UnitContextType | null>(null)

export const UnitProvider = UnitContext.Provider

const CYCLE: TrafficUnit[] = ["bps", "pps", "bytes"]

function readUnit(): TrafficUnit {
  try {
    const v = localStorage.getItem("as-stats-unit")
    return v === "pps" || v === "bytes" ? v : "bps"
  } catch {
    return "bps"
  }
}

export function useUnitState(): UnitContextType {
  const [unit, setUnit] = useState<TrafficUnit>(readUnit)

  const toggleUnit = useCallback(() => {
    const next = CYCLE[(CYCLE.indexOf(unit) + 1) % CYCLE.length]
    try {
      localStorage.setItem("as-stats-unit", next)
    } catch {
      /* noop */
    }
    setUnit(next)
  }, [unit])

  const formatValue = useCallback(
    (value: number, intervalSeconds = 300, kind: RateKind = "bytes") => formatRate(value, intervalSeconds, unit, kind),
    [unit],
  )

  const formatTraffic = useCallback(
    (bytes: number, intervalSeconds = 300, packets?: number) =>
      unit === "pps" && packets !== undefined
        ? formatRate(packets, intervalSeconds, unit, "packets")
        : formatRate(bytes, intervalSeconds, unit, "bytes"),
    [unit],
  )

  const formatPackets = useCallback(
    (packets: number, intervalSeconds = 300) => {
      if (unit === "bytes") return PACKET_NUM_FORMAT.format(packets)
      return formatRate(packets, intervalSeconds, "pps", "packets")
    },
    [unit],
  )

  const formatAxis = useCallback(
    (value: number, intervalSeconds = 300, kind: RateKind = "bytes") => formatRateAxis(value, intervalSeconds, unit, kind),
    [unit],
  )

  return { unit, toggleUnit, formatTraffic, formatPackets, formatAxis, formatValue }
}

export function useUnit(): UnitContextType {
  const ctx = useContext(UnitContext)
  if (!ctx) throw new Error("useUnit must be used within UnitProvider")
  return ctx
}
