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

const WINDOW_FORMAT = new Intl.DateTimeFormat(undefined, {
  day: "numeric",
  month: "short",
  hour: "2-digit",
  minute: "2-digit",
})

// "12 Oct, 12:47 – 13 Oct, 12:47": the active window, so a relative preset
// still says which instants it covers.
function formatWindow(bounds: { from: number; to: number }): string {
  return `${WINDOW_FORMAT.format(bounds.from)} – ${WINDOW_FORMAT.format(bounds.to)}`
}

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
  const presetsRef = useRef<HTMLDivElement>(null)

  // On a phone the presets scroll sideways with no visible scrollbar, and the
  // default 24h sits past the right edge: bring the active one into view so the
  // current window is never hidden.
  useEffect(() => {
    const el = presetsRef.current?.querySelector<HTMLElement>('[aria-pressed="true"]')
    const box = presetsRef.current
    if (!el || !box || box.scrollWidth <= box.clientWidth) return
    box.scrollLeft = Math.max(0, el.offsetLeft - box.offsetLeft - box.clientWidth / 2 + el.offsetWidth / 2)
  }, [period])

  return (
    <div className="border-b border-border">
      <div className="mx-auto max-w-[1400px] px-4 sm:px-6 lg:px-8">
        <div className="flex h-11 items-center gap-3">
          {/* Only the presets scroll horizontally on narrow screens. The Custom
              popover must live OUTSIDE this overflow container: setting
              overflow-x also forces overflow-y to `auto`, which would clip the
              popover as it opens downward (it looked like "Custom does nothing"). */}
          {/* On a phone the strip scrolls with no scrollbar; the edges fade so a
              clipped "1h" or "30d" reads as "more this way", not as a glitch. */}
          <div
            ref={presetsRef}
            className="flex items-center gap-3 overflow-x-auto scrollbar-none min-w-0 max-md:px-2 max-md:[mask-image:linear-gradient(to_right,transparent,black_12px,black_calc(100%-12px),transparent)]"
          >
            <FilterGroup label="Time range">
              {relativePresets.map((p) => (
                <FilterButton key={p.value} active={period === p.value} onClick={() => setPeriod(p.value)}>
                  {p.label}
                </FilterButton>
              ))}
              <span className="mx-1.5 h-4 w-px shrink-0 bg-border" aria-hidden="true" />
              {windowedPresets.map((p) => (
                <FilterButton key={p.value} active={period === p.value} onClick={() => setPeriod(p.value)}>
                  {p.label}
                </FilterButton>
              ))}
            </FilterGroup>
          </div>

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

          <span className="ml-auto hidden whitespace-nowrap text-[13px] text-muted-foreground md:inline">
            {formatWindow(bounds)}
          </span>
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
          "inline-flex items-center gap-1.5 rounded px-2 py-1 text-sm whitespace-nowrap",
          active
            ? "bg-primary font-semibold text-primary-foreground"
            : "text-foreground-secondary hover:bg-accent hover:text-foreground",
        )}
      >
        <CalendarRange className="size-3.5" aria-hidden />
        Custom…
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
      className="absolute left-0 top-full mt-1 z-50 w-72 rounded border border-border bg-popover p-4 shadow-md max-sm:fixed max-sm:inset-x-4 max-sm:top-[6.5rem] max-sm:mt-0 max-sm:w-auto"
    >
      <label htmlFor="custom-range-from" className="mb-1 block text-[13px] text-foreground-secondary">
        From
      </label>
      <input
        id="custom-range-from"
        type="datetime-local"
        value={from}
        onChange={(e) => setFrom(e.target.value)}
        className="mb-3 h-8 w-full rounded border border-input bg-background px-2 text-sm outline-none focus-visible:border-ring"
      />
      <label htmlFor="custom-range-to" className="mb-1 block text-[13px] text-foreground-secondary">
        To
      </label>
      <input
        id="custom-range-to"
        type="datetime-local"
        value={to}
        onChange={(e) => setTo(e.target.value)}
        className="mb-4 h-8 w-full rounded border border-input bg-background px-2 text-sm outline-none focus-visible:border-ring"
      />
      <div className="flex justify-end gap-2">
        <button
          type="button"
          onClick={onCancel}
          className="rounded border border-input px-3 py-1 text-sm hover:bg-accent"
        >
          Cancel
        </button>
        <button
          type="button"
          onClick={apply}
          className="rounded bg-primary px-3 py-1 text-sm font-semibold text-primary-foreground hover:opacity-90"
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
      className="inline-flex shrink-0 items-center gap-1.5 rounded px-2 py-1 text-[13px] text-foreground-secondary hover:bg-accent hover:text-foreground max-md:ml-auto"
      title="Copy a shareable link with the current window frozen to absolute timestamps"
    >
      {copied ? <Check className="size-3.5 text-success" aria-hidden /> : <Link2 className="size-3.5" aria-hidden />}
      {copied ? "Copied" : "Copy link"}
    </button>
  )
}

function FilterGroup({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex shrink-0 items-center gap-0.5" role="group" aria-label={label}>
      {children}
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
      type="button"
      onClick={onClick}
      className={cn(
        "rounded px-2 py-1 text-sm whitespace-nowrap",
        active
          ? "bg-primary font-semibold text-primary-foreground"
          : "text-foreground-secondary hover:bg-accent hover:text-foreground",
      )}
      aria-pressed={active}
    >
      {children}
    </button>
  )
}
