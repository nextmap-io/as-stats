import { useEffect, useCallback } from "react"
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
  const handleKey = useCallback((e: KeyboardEvent) => {
    if (e.key === "Escape") onClose()
  }, [onClose])

  useEffect(() => {
    if (open) {
      document.addEventListener("keydown", handleKey)
      document.body.style.overflow = "hidden"
      return () => {
        document.removeEventListener("keydown", handleKey)
        document.body.style.overflow = ""
      }
    }
  }, [open, handleKey])

  if (!open) return null

  return createPortal(
    <div
      className="fixed inset-0 z-[100] flex items-center justify-center"
      onClick={onClose}
    >
      <div className="absolute inset-0 bg-black/70 backdrop-blur-sm" />

      <div
        className="relative w-[95vw] max-w-6xl max-h-[90vh] bg-card border border-border rounded-lg shadow-2xl overflow-auto"
        onClick={e => e.stopPropagation()}
      >
        {/* Header with title + period selector */}
        <div className="flex items-center gap-4 px-5 pt-4 pb-2 border-b border-border/50">
          {title && <h2 className="text-sm font-semibold mr-auto">{title}</h2>}

          {onPeriodChange && (
            <div className="flex gap-0.5">
              {PERIODS.map(p => (
                <button
                  key={p}
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
