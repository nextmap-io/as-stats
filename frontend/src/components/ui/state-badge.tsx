import { cn } from "@/lib/utils"
import type { Level } from "@/lib/home"

/**
 * StateBadge is the word that goes with a state colour (Ledger: a colour is
 * never the only signal). Amber "Watch" on a pale ground, white "Incident" on
 * red. A normal state stays silent: no badge at all. Pair it with
 * `levelTextClass` (lib/home) on the value it qualifies.
 */
export function StateBadge({ level, className }: { level: Level; className?: string }) {
  if (level === "normal") return null
  return (
    <span
      className={cn(
        "inline-block rounded-sm px-1.5 text-xs font-semibold leading-5 whitespace-nowrap",
        level === "watch" ? "bg-warning-muted text-warning" : "bg-destructive text-destructive-foreground",
        className,
      )}
    >
      {level === "watch" ? "Watch" : "Incident"}
    </span>
  )
}
