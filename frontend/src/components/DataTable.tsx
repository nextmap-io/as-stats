import { useMemo, useState, type ReactNode } from "react"
import { useSearchParams } from "react-router-dom"
import { ArrowDown, ArrowUp, ChevronsUpDown } from "lucide-react"
import { cn, formatPercent } from "@/lib/utils"
import { useDensity } from "@/hooks/useDensity"

export type ColumnAlign = "left" | "right" | "center"

export interface Column<T> {
  /** Stable identifier — also the URL sort key and default field accessor. */
  key: string
  /** Header content. */
  header: ReactNode
  /** Cell alignment. Numeric columns should use "right". */
  align?: ColumnAlign
  /** Render as monospaced tabular-nums (numbers). */
  numeric?: boolean
  /** Enable client-side sorting on this column. */
  sortable?: boolean
  /** Hide on small screens (mirrors the existing `hidden sm:table-cell`). */
  hideOnMobile?: boolean
  /** Extra classes for the header cell. */
  headerClassName?: string
  /** Extra classes for the body cell. */
  className?: string
  /** Custom cell renderer. Receives the row and its absolute index. */
  render?: (row: T, index: number) => ReactNode
  /** Value used for sorting/derived from the row (defaults to `row[key]`). */
  sortValue?: (row: T) => number | string | null | undefined
}

interface DataTableProps<T> {
  rows: T[]
  columns: Column<T>[]
  /** Stable React key for each row. */
  rowKey: (row: T, index: number) => string | number
  /** Extra classes on the `<table>`. Defaults to `w-full text-xs`. */
  tableClassName?: string
  /** Extra classes on each body `<tr>`. */
  rowClassName?: string
  /**
   * Cap the scroll container height (px). Enables a sticky header + vertical
   * scroll. When omitted, large lists auto-cap to enable virtualization.
   */
  maxHeight?: number
  /** Estimated row height (px) used for virtualization math. */
  rowHeight?: number
  /** Row count above which windowing kicks in. */
  virtualizeThreshold?: number
  /** URL search-param name for the sort column. */
  sortParam?: string
  /** URL search-param name for the sort direction. */
  dirParam?: string
}

const OVERSCAN = 12
const DEFAULT_VIRTUAL_HEIGHT = 600

type SortDir = "asc" | "desc"

export function DataTable<T>({
  rows,
  columns,
  rowKey,
  tableClassName,
  rowClassName,
  maxHeight,
  rowHeight = 33,
  virtualizeThreshold = 150,
  sortParam = "sort",
  dirParam = "dir",
}: DataTableProps<T>) {
  const [searchParams, setSearchParams] = useSearchParams()
  const [scrollTop, setScrollTop] = useState(0)
  const { density } = useDensity()
  const compact = density === "compact"
  // Compact mode tightens vertical rhythm and shrinks the virtualization row
  // estimate so windowing math stays accurate.
  const cellPad = compact ? "py-0.5" : "py-1.5"
  const effectiveRowHeight = compact ? Math.max(20, rowHeight - 9) : rowHeight

  const sortKey = searchParams.get(sortParam)
  const sortDir: SortDir = searchParams.get(dirParam) === "asc" ? "asc" : "desc"

  const sortedRows = useMemo(() => {
    if (!sortKey) return rows
    const col = columns.find((c) => c.key === sortKey && c.sortable)
    if (!col) return rows
    const getVal =
      col.sortValue ?? ((r: T) => (r as Record<string, unknown>)[col.key] as number | string | null | undefined)
    const copy = [...rows]
    copy.sort((a, b) => {
      const va = getVal(a)
      const vb = getVal(b)
      let cmp: number
      if (typeof va === "number" && typeof vb === "number") {
        cmp = va - vb
      } else {
        cmp = String(va ?? "").localeCompare(String(vb ?? ""), undefined, { numeric: true })
      }
      return sortDir === "asc" ? cmp : -cmp
    })
    return copy
  }, [rows, columns, sortKey, sortDir])

  const toggleSort = (col: Column<T>) => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        const curKey = next.get(sortParam)
        const curDir = next.get(dirParam)
        if (curKey !== col.key) {
          next.set(sortParam, col.key)
          next.set(dirParam, "desc")
        } else if (curDir !== "asc") {
          next.set(dirParam, "asc")
        } else {
          next.delete(sortParam)
          next.delete(dirParam)
        }
        return next
      },
      { replace: true },
    )
  }

  const shouldVirtualize = sortedRows.length > virtualizeThreshold
  const viewport = maxHeight ?? (shouldVirtualize ? DEFAULT_VIRTUAL_HEIGHT : undefined)
  const virtualize = shouldVirtualize && viewport != null

  let start = 0
  let end = sortedRows.length
  let topPad = 0
  let bottomPad = 0
  if (virtualize && viewport != null) {
    start = Math.max(0, Math.floor(scrollTop / effectiveRowHeight) - OVERSCAN)
    const count = Math.ceil(viewport / effectiveRowHeight) + OVERSCAN * 2
    end = Math.min(sortedRows.length, start + count)
    topPad = start * effectiveRowHeight
    bottomPad = (sortedRows.length - end) * effectiveRowHeight
  }
  const visibleRows = sortedRows.slice(start, end)

  return (
    <div
      className={cn("overflow-x-auto -mx-4 px-4 sm:-mx-5 sm:px-5", viewport != null && "overflow-y-auto")}
      style={viewport != null ? { maxHeight: viewport } : undefined}
      onScroll={virtualize ? (e) => setScrollTop(e.currentTarget.scrollTop) : undefined}
    >
      <table className={cn("w-full", compact ? "text-[11px]" : "text-xs", tableClassName)}>
        <thead className="sticky top-0 z-10 bg-card">
          <tr className="border-b border-border">
            {columns.map((col) => {
              const active = col.sortable && sortKey === col.key
              return (
                <th
                  key={col.key}
                  scope="col"
                  aria-sort={
                    col.sortable ? (active ? (sortDir === "asc" ? "ascending" : "descending") : "none") : undefined
                  }
                  onClick={col.sortable ? () => toggleSort(col) : undefined}
                  className={cn(
                    "bg-card pb-2 pt-0.5 align-bottom font-medium text-muted-foreground whitespace-nowrap",
                    col.align === "right" ? "text-right" : col.align === "center" ? "text-center" : "text-left",
                    col.sortable && "cursor-pointer select-none hover:text-foreground transition-colors",
                    col.hideOnMobile && "hidden sm:table-cell",
                    col.headerClassName,
                  )}
                >
                  <span
                    className={cn(
                      "inline-flex items-center gap-1",
                      col.align === "right" && "flex-row-reverse",
                    )}
                  >
                    {col.header}
                    {col.sortable &&
                      (active ? (
                        sortDir === "asc" ? (
                          <ArrowUp className="size-3 shrink-0" aria-hidden />
                        ) : (
                          <ArrowDown className="size-3 shrink-0" aria-hidden />
                        )
                      ) : (
                        <ChevronsUpDown className="size-3 shrink-0 opacity-40" aria-hidden />
                      ))}
                  </span>
                </th>
              )
            })}
          </tr>
        </thead>
        <tbody>
          {topPad > 0 && (
            <tr aria-hidden style={{ height: topPad }}>
              <td colSpan={columns.length} className="p-0" />
            </tr>
          )}
          {visibleRows.map((row, i) => {
            const index = start + i
            return (
              <tr
                key={rowKey(row, index)}
                className={cn(
                  "border-b border-border/50 last:border-0 hover:bg-muted/50 transition-colors",
                  rowClassName,
                )}
              >
                {columns.map((col) => (
                  <td
                    key={col.key}
                    className={cn(
                      cellPad,
                      col.align === "right" ? "text-right" : col.align === "center" ? "text-center" : "text-left",
                      col.numeric && "font-mono tabular-nums",
                      col.hideOnMobile && "hidden sm:table-cell",
                      col.className,
                    )}
                  >
                    {col.render
                      ? col.render(row, index)
                      : String((row as Record<string, unknown>)[col.key] ?? "")}
                  </td>
                ))}
              </tr>
            )
          })}
          {bottomPad > 0 && (
            <tr aria-hidden style={{ height: bottomPad }}>
              <td colSpan={columns.length} className="p-0" />
            </tr>
          )}
        </tbody>
      </table>
    </div>
  )
}

/**
 * Percent-of-total bar cell — extracted from the hand-rolled Top-N tables.
 * Renders a small track + fill and the numeric percentage (tabular-nums).
 */
export function PercentBar({
  pct,
  barClassName = "bg-primary",
}: {
  pct: number
  barClassName?: string
}) {
  const clamped = Math.min(Math.max(pct || 0, 0), 100)
  return (
    <div className="flex items-center justify-end gap-2">
      <div className="w-16 bg-muted rounded-full h-1.5">
        <div className={cn("h-1.5 rounded-full", barClassName)} style={{ width: `${clamped}%` }} />
      </div>
      <span className="w-12 text-right tabular-nums">{formatPercent(pct || 0)}</span>
    </div>
  )
}
