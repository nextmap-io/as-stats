import { useEffect, useRef, useState } from "react"
import { useSearchParams } from "react-router-dom"
import { Link2, Check, CalendarRange } from "lucide-react"
import { useFilters } from "@/hooks/useFilters"
import { cn } from "@/lib/utils"

const relativePresets = [
  { label: "15m", value: "15m" },
  { label: "1h", value: "1h" },
  { label: "3h", value: "3h" },
  { label: "4h", value: "4h" },
  { label: "6h", value: "6h" },
  { label: "12h", value: "12h" },
  { label: "24h", value: "24h" },
  { label: "7d", value: "7d" },
  { label: "30d", value: "30d" },
]

const windowedPresets = [
  { label: "Yesterday", value: "yesterday" },
  { label: "This week", value: "week" },
]

// Convert an epoch-ms instant to the value a <input type="datetime-local">
// expects (local wall-clock, "YYYY-MM-DDTHH:mm").
function toLocalInput(ms: number): string {
  const d = new Date(ms - new Date(ms).getTimezoneOffset() * 60000)
  return d.toISOString().slice(0, 16)
}

export function FilterBar() {
  const { period, setPeriod, setAbsolute, bounds } = useFilters()
  const [customOpen, setCustomOpen] = useState(false)
  const [copied, setCopied] = useState(false)

  return (
    <div className="border-b border-border bg-muted/20">
      <div className="mx-auto max-w-7xl px-4 sm:px-6 lg:px-8">
        <div className="flex items-center gap-3 py-1.5 overflow-x-auto scrollbar-none">
          <FilterGroup label="Period">
            {relativePresets.map((p) => (
              <FilterButton key={p.value} active={period === p.value} onClick={() => setPeriod(p.value)}>
                {p.label}
              </FilterButton>
            ))}
            {windowedPresets.map((p) => (
              <FilterButton key={p.value} active={period === p.value} onClick={() => setPeriod(p.value)}>
                {p.label}
              </FilterButton>
            ))}
          </FilterGroup>

          <div className="h-4 w-px bg-border shrink-0" aria-hidden="true" />

          <CustomRange
            open={customOpen}
            onOpenChange={setCustomOpen}
            active={period === "custom"}
            bounds={bounds}
            onApply={(from, to) => {
              setAbsolute(from, to)
              setCustomOpen(false)
            }}
          />

          <CopyLinkButton bounds={bounds} copied={copied} setCopied={setCopied} />
        </div>
      </div>
    </div>
  )
}

function CustomRange({
  open,
  onOpenChange,
  active,
  bounds,
  onApply,
}: {
  open: boolean
  onOpenChange: (o: boolean) => void
  active: boolean
  bounds: { from: number; to: number }
  onApply: (from: string, to: string) => void
}) {
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) onOpenChange(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onOpenChange(false)
    }
    document.addEventListener("mousedown", onDown)
    document.addEventListener("keydown", onKey)
    return () => {
      document.removeEventListener("mousedown", onDown)
      document.removeEventListener("keydown", onKey)
    }
  }, [open, onOpenChange])

  return (
    <div className="relative shrink-0" ref={ref}>
      <button
        type="button"
        onClick={() => onOpenChange(!open)}
        aria-haspopup="dialog"
        aria-expanded={open}
        className={cn(
          "inline-flex items-center gap-1 px-2 py-0.5 text-[11px] font-medium rounded transition-all",
          active
            ? "bg-primary text-primary-foreground shadow-sm"
            : "text-muted-foreground hover:text-foreground hover:bg-accent",
        )}
      >
        <CalendarRange className="size-3" aria-hidden />
        Custom
      </button>
      {/* Mounts fresh each open so the inputs seed from the active window via
          useState initializers — no seeding effect required. */}
      {open && <CustomRangePopover bounds={bounds} onCancel={() => onOpenChange(false)} onApply={onApply} />}
    </div>
  )
}

function CustomRangePopover({
  bounds,
  onCancel,
  onApply,
}: {
  bounds: { from: number; to: number }
  onCancel: () => void
  onApply: (from: string, to: string) => void
}) {
  const [from, setFrom] = useState(() => toLocalInput(bounds.from))
  const [to, setTo] = useState(() => toLocalInput(bounds.to))

  const apply = () => {
    const f = new Date(from)
    const t = new Date(to)
    if (Number.isNaN(f.getTime()) || Number.isNaN(t.getTime()) || t <= f) return
    onApply(f.toISOString(), t.toISOString())
  }

  return (
    <div
      role="dialog"
      aria-label="Custom time range"
      className="absolute left-0 top-full mt-1 z-50 w-64 rounded border border-border bg-card p-3 shadow-lg animate-fade-in"
    >
      <label className="block text-[10px] font-medium text-muted-foreground uppercase tracking-widest mb-1">
        From
      </label>
      <input
        type="datetime-local"
        value={from}
        onChange={(e) => setFrom(e.target.value)}
        className="mb-2 h-8 w-full rounded border border-input bg-muted/50 px-2 text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring"
      />
      <label className="block text-[10px] font-medium text-muted-foreground uppercase tracking-widest mb-1">
        To
      </label>
      <input
        type="datetime-local"
        value={to}
        onChange={(e) => setTo(e.target.value)}
        className="mb-3 h-8 w-full rounded border border-input bg-muted/50 px-2 text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring"
      />
      <div className="flex justify-end gap-2">
        <button
          type="button"
          onClick={onCancel}
          className="px-2.5 py-1 text-xs rounded border border-input hover:bg-accent transition-colors"
        >
          Cancel
        </button>
        <button
          type="button"
          onClick={apply}
          className="px-2.5 py-1 text-xs rounded bg-primary text-primary-foreground hover:opacity-90 transition-opacity"
        >
          Apply
        </button>
      </div>
    </div>
  )
}

// CopyLinkButton (U6) freezes the currently-active window into explicit
// from/to timestamps and copies the resulting URL, so the link reproduces the
// exact window later even for relative presets.
function CopyLinkButton({
  bounds,
  copied,
  setCopied,
}: {
  bounds: { from: number; to: number }
  copied: boolean
  setCopied: (v: boolean) => void
}) {
  const [searchParams] = useSearchParams()

  const copy = async () => {
    const next = new URLSearchParams(searchParams)
    next.delete("period")
    next.set("from", new Date(bounds.from).toISOString())
    next.set("to", new Date(bounds.to).toISOString())
    const url = `${window.location.origin}${window.location.pathname}?${next.toString()}`
    try {
      await navigator.clipboard.writeText(url)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1500)
    } catch {
      /* clipboard unavailable — no-op */
    }
  }

  return (
    <button
      type="button"
      onClick={copy}
      className="ml-auto inline-flex shrink-0 items-center gap-1 px-2 py-0.5 text-[11px] font-medium rounded text-muted-foreground hover:text-foreground hover:bg-accent transition-colors"
      title="Copy a shareable link with the current window frozen to absolute timestamps"
    >
      {copied ? <Check className="size-3 text-success" aria-hidden /> : <Link2 className="size-3" aria-hidden />}
      {copied ? "Copied" : "Copy link"}
    </button>
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

function FilterButton({
  active,
  onClick,
  children,
}: {
  active: boolean
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <button
      onClick={onClick}
      className={cn(
        "px-2 py-0.5 text-[11px] font-medium rounded transition-all whitespace-nowrap",
        active
          ? "bg-primary text-primary-foreground shadow-sm"
          : "text-muted-foreground hover:text-foreground hover:bg-accent",
      )}
      aria-pressed={active}
    >
      {children}
    </button>
  )
}
