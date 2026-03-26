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
    <div className="border-b border-border bg-muted/20">
      <div className="mx-auto max-w-7xl px-4 sm:px-6 lg:px-8">
        <div className="flex items-center gap-3 py-1.5 overflow-x-auto scrollbar-none">
          <FilterGroup label="Period">
            {periods.map(p => (
              <FilterButton
                key={p.value}
                active={filters.period === p.value}
                onClick={() => setFilter("period", p.value)}
              >
                {p.label}
              </FilterButton>
            ))}
          </FilterGroup>

          <div className="h-4 w-px bg-border shrink-0" aria-hidden="true" />

          <FilterGroup label="Direction">
            {directions.map(d => (
              <FilterButton
                key={d.value}
                active={(filters.direction || "") === d.value}
                onClick={() => setFilter("direction", d.value || undefined)}
              >
                {d.label}
              </FilterButton>
            ))}
          </FilterGroup>
        </div>
      </div>
    </div>
  )
}

function FilterGroup({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center gap-1.5 shrink-0" role="group" aria-label={label}>
      <span className="text-[10px] font-medium text-muted-foreground uppercase tracking-widest">{label}</span>
      <div className="flex gap-0.5">{children}</div>
    </div>
  )
}

function FilterButton({ active, onClick, children }: { active: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      onClick={onClick}
      className={cn(
        "px-2 py-0.5 text-[11px] font-medium rounded transition-all",
        active
          ? "bg-primary text-primary-foreground shadow-sm"
          : "text-muted-foreground hover:text-foreground hover:bg-accent"
      )}
      aria-pressed={active}
    >
      {children}
    </button>
  )
}
