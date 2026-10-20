import { useSearchParams } from "react-router-dom"
import { Table2, LayoutGrid, ChartPie } from "lucide-react"
import { Segmented } from "@/components/ui/segmented"
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
    <Segmented
      label="View mode"
      value={value}
      onChange={set}
      options={OPTIONS.map(({ value: v, label, Icon }) => ({
        value: v,
        title: label,
        label: (
          <>
            <Icon className="size-3.5" aria-hidden />
            <span className="max-sm:sr-only">{label}</span>
          </>
        ),
      }))}
    />
  )
}
