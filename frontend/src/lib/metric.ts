/** The whitelisted Top-N sort metrics (F1). Mirrors store.metricColumns. */
export type Metric = "bytes" | "packets" | "flows"

export const METRICS: Metric[] = ["bytes", "packets", "flows"]

export const METRIC_LABELS: Record<Metric, string> = {
  bytes: "Bytes",
  packets: "Packets",
  flows: "Flows",
}

/** Narrow an arbitrary URL param to a valid Metric, defaulting to "bytes". */
export function asMetric(v: string | undefined): Metric {
  return v === "packets" || v === "flows" ? v : "bytes"
}

/** Pull the metric value off any row that carries bytes/packets/flows counters. */
export function metricValue(
  row: { bytes: number; packets: number; flows: number },
  metric: Metric,
): number {
  return metric === "packets" ? row.packets : metric === "flows" ? row.flows : row.bytes
}
