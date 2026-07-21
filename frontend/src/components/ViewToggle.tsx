import { useSearchParams } from "react-router-dom"
import { Table2, LayoutGrid, ChartPie } from "lucide-react"
import { cn } from "@/lib/utils"
import { asView, type TopView } from "@/lib/view"

const OPTIONS: { value: TopView; label: string; Icon: typeof Table2 }[] = [
  { value: "table", label: "Table", Icon: Table2 },
  { value: "treemap", label: "Treemap", Icon: LayoutGrid },
  { value: "donut", label: "Donut", Icon: ChartPie },
]

/**
 * ViewToggle (U9) — URL-synced chart/table switcher for the Top-N pages. Writes
 * `?view=`; "table" is the implicit default so selecting it clears the param.
 */
export function ViewToggle({ param = "view" }: { param?: string }) {
  const [searchParams, setSearchParams] = useSearchParams()
  const value = asView(searchParams.get(param))

  const set = (v: TopView) => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        if (v === "table") next.delete(param)
        else next.set(param, v)
        return next
      },
      { replace: true },
    )
  }

  return (
    <div
      className="flex gap-0.5 rounded border border-input bg-muted/30 p-0.5"
      role="group"
      aria-label="View mode"
    >
      {OPTIONS.map(({ value: v, label, Icon }) => (
        <button
          key={v}
          type="button"
          onClick={() => set(v)}
          aria-pressed={value === v}
          title={label}
          className={cn(
            "inline-flex items-center gap-1 px-2 py-0.5 text-[11px] font-medium rounded transition-colors",
            value === v
              ? "bg-primary text-primary-foreground"
              : "text-muted-foreground hover:text-foreground hover:bg-accent",
          )}
        >
          <Icon className="size-3" aria-hidden />
          <span className="hidden sm:inline">{label}</span>
        </button>
      ))}
    </div>
  )
}
