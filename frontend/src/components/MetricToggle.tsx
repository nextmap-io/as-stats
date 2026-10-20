import { Segmented } from "@/components/ui/segmented"
import { METRICS, METRIC_LABELS, type Metric } from "@/lib/metric"

/**
 * MetricToggle — Bytes / Packets / Flows selector for the Top-N pages (F1).
 *
 * URL-synced: the caller passes the current value (from useFilters) and an
 * onChange that writes `?metric=`. "bytes" is the implicit default, so
 * selecting it clears the param (value === undefined) to keep URLs clean.
 */
export function MetricToggle({
  value,
  onChange,
}: {
  value: Metric
  onChange: (metric: string | undefined) => void
}) {
  return (
    <Segmented
      label="Sort metric"
      value={value}
      onChange={(m) => onChange(m === "bytes" ? undefined : m)}
      options={METRICS.map((m) => ({ value: m, label: METRIC_LABELS[m], title: `Sort by ${METRIC_LABELS[m].toLowerCase()}` }))}
    />
  )
}
