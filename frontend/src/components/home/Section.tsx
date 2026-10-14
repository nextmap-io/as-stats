import type { ReactNode } from "react"
import { cn } from "@/lib/utils"

/**
 * One block of the home report: a 2px ink rule, the section name and a line on
 * how to read it in a narrow left column, the data on the right. On phones the
 * aside collapses above the content.
 */
export function Section({
  id,
  title,
  note,
  aside,
  children,
  first = false,
}: {
  id: string
  title: string
  /** One sentence on how to read the block. */
  note?: ReactNode
  /** Extra aside content, typically a link to the full view. */
  aside?: ReactNode
  children: ReactNode
  first?: boolean
}) {
  return (
    <section
      aria-labelledby={id}
      className={cn(
        "grid grid-cols-1 gap-x-12 gap-y-4 pb-8 pt-6 lg:grid-cols-[200px_minmax(0,1fr)]",
        !first && "border-t-2 border-foreground",
      )}
    >
      <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1 lg:block">
        <h2 id={id} className="text-[15px] font-semibold leading-snug">
          {title}
        </h2>
        {note && <p className="text-[13px] leading-snug text-muted-foreground lg:mt-1.5 lg:max-w-[26ch]">{note}</p>}
        {aside && <div className="text-[13px] lg:mt-2">{aside}</div>}
      </div>
      <div className="min-w-0">{children}</div>
    </section>
  )
}

/** Small colour key used in legends and figure labels. */
export function Key({ dir }: { dir: "in" | "out" }) {
  return (
    <span
      aria-hidden
      className={cn("inline-block h-2.5 w-3.5 shrink-0 rounded-[1px]", dir === "in" ? "bg-traffic-in" : "bg-traffic-out")}
    />
  )
}
