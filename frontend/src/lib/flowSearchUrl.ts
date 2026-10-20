import type { FlowSearchFilters } from "@/lib/types"

/** Periods the Flow Search form offers (and accepts from a link). */
export const FLOW_SEARCH_PERIODS = ["1h", "6h", "24h", "7d", "30d"]

/**
 * Filters passed in the URL by other pages (Live Threats links here with
 * ?dst_ip=…&from=…&to=…, Top Ports with ?protocol=…&port=…&period=…). When
 * present, the search runs straight away.
 */
export function filtersFromURL(params: URLSearchParams): FlowSearchFilters | null {
  const out: FlowSearchFilters = { limit: 100 }
  let any = false
  for (const key of ["src_ip", "dst_ip"] as const) {
    const v = params.get(key)?.trim()
    if (v) {
      out[key] = v
      any = true
    }
  }
  for (const key of ["src_as", "dst_as", "src_port", "dst_port", "port"] as const) {
    const n = Number(params.get(key))
    if (Number.isInteger(n) && n > 0) {
      out[key] = n
      any = true
    }
  }
  const protocol = Number(params.get("protocol"))
  if (Number.isInteger(protocol) && protocol > 0 && protocol < 256) out.protocol = protocol
  const from = params.get("from")
  const to = params.get("to")
  const period = params.get("period")
  if (from && to && !Number.isNaN(Date.parse(from)) && !Number.isNaN(Date.parse(to))) {
    out.from = from
    out.to = to
  } else if (period && FLOW_SEARCH_PERIODS.includes(period)) {
    out.period = period
  } else {
    out.period = "1h"
  }
  return any ? out : null
}
