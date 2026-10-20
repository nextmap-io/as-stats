import type { ReactNode } from "react"
import { cn } from "@/lib/utils"

export interface SegmentOption<T extends string> {
  value: T
  label: ReactNode
  /** Tooltip / longer description. */
  title?: string
}

/**
 * The one segmented selector (Ledger): a quiet track, the active segment on the
 * surface with ink text, the others in secondary ink (AA on the track in both
 * themes). Used for every "pick one of a few" switch — dimension, metric, view,
 * scope, window — so they all look and behave the same.
 */
export function Segmented<T extends string>({
  label,
  options,
  value,
  onChange,
  className,
}: {
  /** Accessible name of the group, e.g. "IP version". */
  label: string
  options: readonly SegmentOption<T>[]
  value: T
  onChange: (value: T) => void
  className?: string
}) {
  return (
    <div
      role="group"
      aria-label={label}
      className={cn("inline-flex max-w-full shrink-0 rounded border border-input bg-muted p-0.5", className)}
    >
      {options.map((o) => {
        const active = o.value === value
        return (
          <button
            key={o.value}
            type="button"
            aria-pressed={active}
            title={o.title}
            onClick={() => onChange(o.value)}
            className={cn(
              "inline-flex min-h-7 items-center gap-1.5 whitespace-nowrap rounded-[3px] px-2.5 text-[13px] transition-colors",
              active
                ? "bg-card font-semibold text-foreground ring-1 ring-border"
                : "text-foreground-secondary hover:text-foreground",
            )}
          >
            {o.label}
          </button>
        )
      })}
    </div>
  )
}
