import { useState, cloneElement, isValidElement } from "react"
import { ChartModal } from "./ChartModal"

interface ExpandableChartProps {
  title?: string
  children: React.ReactElement<{ height?: number }>
  expandedHeight?: number
}

export function ExpandableChart({ title, children, expandedHeight = 500 }: ExpandableChartProps) {
  const [open, setOpen] = useState(false)

  // Clone the chart element with a larger height when in modal
  const expandedChart = isValidElement(children)
    ? cloneElement(children, { height: expandedHeight } as Record<string, unknown>)
    : children

  return (
    <>
      <div
        onClick={() => setOpen(true)}
        className="cursor-zoom-in"
        title="Click to enlarge"
      >
        {children}
      </div>
      <ChartModal open={open} onClose={() => setOpen(false)} title={title}>
        {expandedChart}
      </ChartModal>
    </>
  )
}
