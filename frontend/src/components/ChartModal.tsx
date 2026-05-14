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
        className="absolute inset-0 w-full h-full bg-black/70 backdrop-blur-sm border-0 p-0"
      />

      <div className="relative w-[95vw] max-w-6xl max-h-[90vh] bg-card border border-border rounded-lg shadow-2xl overflow-auto">
        {/* Header with title + period selector */}
        <div className="flex items-center gap-4 px-5 pt-4 pb-2 border-b border-border/50">
          {title && <h2 className="text-sm font-semibold mr-auto">{title}</h2>}

          {onPeriodChange && (
            <div className="flex gap-0.5">
              {PERIODS.map(p => (
                <button
                  key={p}
                  type="button"
                  onClick={() => onPeriodChange(p)}
                  className={`px-2 py-0.5 text-[10px] font-medium rounded transition-colors ${
                    activePeriod === p
                      ? "bg-primary text-primary-foreground"
                      : "text-muted-foreground hover:text-foreground hover:bg-accent"
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
          <div className="flex flex-wrap gap-x-5 gap-y-1 px-5 py-2 text-xs border-b border-border/30">
            {stats.map(s => (
              <div key={s.label} className="flex items-baseline gap-1.5">
                <span className="text-muted-foreground">{s.label}:</span>
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
