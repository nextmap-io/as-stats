import { cn } from "@/lib/utils"
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
    <div
      className="flex gap-0.5 rounded border border-input bg-muted/30 p-0.5"
      role="group"
      aria-label="Sort metric"
    >
      {METRICS.map((m) => (
        <button
          key={m}
          type="button"
          onClick={() => onChange(m === "bytes" ? undefined : m)}
          aria-pressed={value === m}
          className={cn(
            "px-2 py-0.5 text-[11px] font-medium rounded transition-colors",
            value === m
              ? "bg-primary text-primary-foreground"
              : "text-muted-foreground hover:text-foreground hover:bg-accent",
          )}
          title={`Sort by ${METRIC_LABELS[m].toLowerCase()}`}
        >
          {METRIC_LABELS[m]}
        </button>
      ))}
    </div>
  )
}
