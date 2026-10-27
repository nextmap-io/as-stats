import { useSearchParams } from "react-router-dom"
import { cn } from "@/lib/utils"

/**
 * ComparisonToggle is a URL-synced (`?compare=prev`) checkbox that turns the
 * previous-period overlay on time-series charts on/off (Module D). It writes
 * the `compare` search param so the state survives navigation and shareable
 * links, matching the app's URL-synced-filters convention.
 */
export function ComparisonToggle({ className }: { className?: string }) {
  const [searchParams, setSearchParams] = useSearchParams()
  const on = searchParams.get("compare") === "prev"

  const toggle = () => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        if (on) {
          next.delete("compare")
        } else {
          next.set("compare", "prev")
        }
        return next
      },
      { replace: true },
    )
  }

  // A checkbox, as on the dashboard (Ledger: the comparison overlay is a dashed
  // line switched by a checkbox, not a button with an on-state fill).
  return (
    <label
      title="Overlay the previous equal-length period as a dashed line"
      className={cn("inline-flex min-h-7 cursor-pointer items-center gap-2 text-[13px] text-foreground-secondary", className)}
    >
      <input type="checkbox" checked={on} onChange={toggle} className="size-4 accent-[var(--color-ring)]" />
      Compare with the previous period
    </label>
  )
}
