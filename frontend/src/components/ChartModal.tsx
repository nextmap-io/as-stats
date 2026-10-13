import { useEffect, useRef } from "react"
import { createPortal } from "react-dom"

const PERIODS = ["1h", "3h", "6h", "24h", "7d", "30d"]

interface ChartModalProps {
  open: boolean
  onClose: () => void
  title?: string
  activePeriod?: string
  onPeriodChange?: (period: string) => void
  stats?: { label: string; value: string; color?: string }[]
  children: React.ReactNode
}

export function ChartModal({ open, onClose, title, activePeriod, onPeriodChange, stats, children }: ChartModalProps) {
  // Keep a ref to the latest onClose so the keydown listener doesn't need to
  // resubscribe on every parent render.
  const onCloseRef = useRef(onClose)
  useEffect(() => { onCloseRef.current = onClose }, [onClose])

  useEffect(() => {
    if (!open) return
    const handler = (e: KeyboardEvent) => {
      if (e.key === "Escape") onCloseRef.current()
    }
    document.addEventListener("keydown", handler)
    document.body.style.overflow = "hidden"
    return () => {
      document.removeEventListener("keydown", handler)
      document.body.style.overflow = ""
    }
  }, [open])

  if (!open) return null

  return createPortal(
    <div
      className="fixed inset-0 z-[100] flex items-center justify-center"
      role="dialog"
      aria-modal="true"
      aria-label={title || "Chart"}
    >
      <button
        type="button"
        aria-label="Close modal"
        onClick={onClose}
        className="absolute inset-0 w-full h-full bg-black/50 border-0 p-0"
      />

      <div className="relative w-[95vw] max-w-6xl max-h-[90vh] overflow-auto rounded border border-border bg-card shadow-md">
        {/* Header with title + period selector */}
        <div className="flex flex-wrap items-center gap-4 border-b border-border px-5 pb-3 pt-4">
          {title && <h2 className="mr-auto text-[15px] font-semibold">{title}</h2>}

          {onPeriodChange && (
            <div className="flex gap-0.5" role="group" aria-label="Time range">
              {PERIODS.map(p => (
                <button
                  key={p}
                  type="button"
                  onClick={() => onPeriodChange(p)}
                  aria-pressed={activePeriod === p}
                  className={`rounded px-2 py-1 text-sm ${
                    activePeriod === p
                      ? "bg-primary font-semibold text-primary-foreground"
                      : "text-foreground-secondary hover:bg-accent hover:text-foreground"
                  }`}
                >
                  {p}
                </button>
              ))}
            </div>
          )}

          <button
            type="button"
            onClick={onClose}
            className="text-muted-foreground hover:text-foreground transition-colors text-lg leading-none px-2"
            aria-label="Close"
          >
            &times;
          </button>
        </div>

        {/* Stats bar */}
        {stats && stats.length > 0 && (
          <div className="flex flex-wrap gap-x-6 gap-y-1 border-b border-border px-5 py-2 text-sm">
            {stats.map(s => (
              <div key={s.label} className="flex items-baseline gap-1.5">
                <span className="text-muted-foreground">{s.label}</span>
                <span className={`font-semibold ${s.color || ""}`}>{s.value}</span>
              </div>
            ))}
          </div>
        )}

        {/* Chart area */}
        <div className="p-5">
          {children}
        </div>
      </div>
    </div>,
    document.body,
  )
}
