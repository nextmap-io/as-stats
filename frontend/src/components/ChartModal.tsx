import { useEffect, useCallback } from "react"
import { createPortal } from "react-dom"

interface ChartModalProps {
  open: boolean
  onClose: () => void
  title?: string
  children: React.ReactNode
}

export function ChartModal({ open, onClose, title, children }: ChartModalProps) {
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
      {/* Backdrop */}
      <div className="absolute inset-0 bg-black/70 backdrop-blur-sm" />

      {/* Content */}
      <div
        className="relative w-[95vw] max-w-6xl max-h-[90vh] bg-card border border-border rounded-lg shadow-2xl overflow-auto p-6"
        onClick={e => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between mb-4">
          {title && <h2 className="text-sm font-semibold">{title}</h2>}
          <button
            onClick={onClose}
            className="ml-auto text-muted-foreground hover:text-foreground transition-colors text-lg leading-none px-2"
            aria-label="Close"
          >
            &times;
          </button>
        </div>

        {/* Chart area */}
        {children}
      </div>
    </div>,
    document.body,
  )
}
