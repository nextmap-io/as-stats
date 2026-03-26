import { useFilters } from "@/hooks/useFilters"
import { cn } from "@/lib/utils"

const periods = [
  { label: "1h", value: "1h" },
  { label: "6h", value: "6h" },
  { label: "24h", value: "24h" },
  { label: "7d", value: "7d" },
  { label: "30d", value: "30d" },
]

const directions = [
  { label: "Both", value: "" },
  { label: "In", value: "in" },
  { label: "Out", value: "out" },
]

export function FilterBar() {
  const { filters, setFilter } = useFilters()

  return (
    <div className="border-b border-border bg-muted/30">
      <div className="mx-auto max-w-7xl px-4 sm:px-6 lg:px-8">
        <div className="flex items-center gap-4 py-2">
          <span className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Period</span>
          <div className="flex gap-1">
            {periods.map(p => (
              <button
                key={p.value}
                onClick={() => setFilter("period", p.value)}
                className={cn(
                  "px-2.5 py-1 text-xs font-medium rounded-md transition-colors",
                  filters.period === p.value
                    ? "bg-primary text-primary-foreground"
                    : "text-muted-foreground hover:text-foreground hover:bg-accent"
                )}
              >
                {p.label}
              </button>
            ))}
          </div>

          <div className="h-4 w-px bg-border" />

          <span className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Direction</span>
          <div className="flex gap-1">
            {directions.map(d => (
              <button
                key={d.value}
                onClick={() => setFilter("direction", d.value || undefined)}
                className={cn(
                  "px-2.5 py-1 text-xs font-medium rounded-md transition-colors",
                  (filters.direction || "") === d.value
                    ? "bg-primary text-primary-foreground"
                    : "text-muted-foreground hover:text-foreground hover:bg-accent"
                )}
              >
                {d.label}
              </button>
            ))}
          </div>
        </div>
      </div>
    </div>
  )
}
