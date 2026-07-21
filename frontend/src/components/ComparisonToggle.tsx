import { useSearchParams } from "react-router-dom"
import { GitCompareArrows } from "lucide-react"
import { cn } from "@/lib/utils"

/**
 * ComparisonToggle is a URL-synced (`?compare=prev`) button that turns the
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

  return (
    <button
      type="button"
      onClick={toggle}
      aria-pressed={on}
      title="Overlay the previous equal-length period"
      className={cn(
        "inline-flex items-center gap-1.5 px-2.5 py-1 text-xs font-medium rounded border transition-colors",
        on
          ? "border-primary/50 bg-primary/15 text-primary"
          : "border-input bg-muted/50 text-muted-foreground hover:bg-accent hover:text-foreground",
        className,
      )}
    >
      <GitCompareArrows className="size-3" />
      Compare previous
    </button>
  )
}
