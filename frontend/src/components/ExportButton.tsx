import { useState } from "react"
import { Download, ChevronDown, FileText, FileJson } from "lucide-react"
import { cn } from "@/lib/utils"

type CellValue = string | number | boolean | null | undefined

export interface ExportColumn<T> {
  /** Field name used as the JSON key. */
  key: string
  /** CSV header label. */
  header: string
  /** Extracts the raw (unformatted) value for this row. */
  value: (row: T) => CellValue
}

interface ExportButtonProps<T> {
  rows: T[]
  columns: ExportColumn<T>[]
  /** File name without extension. */
  filename: string
  disabled?: boolean
  className?: string
}

function escapeCSV(v: CellValue): string {
  const s = v == null ? "" : String(v)
  return /[",\n\r]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s
}

function toCSV<T>(rows: T[], columns: ExportColumn<T>[]): string {
  const header = columns.map((c) => escapeCSV(c.header)).join(",")
  const body = rows.map((r) => columns.map((c) => escapeCSV(c.value(r))).join(","))
  return [header, ...body].join("\r\n")
}

function toJSON<T>(rows: T[], columns: ExportColumn<T>[]): string {
  const out = rows.map((r) => {
    const o: Record<string, CellValue> = {}
    for (const c of columns) o[c.key] = c.value(r) ?? null
    return o
  })
  return JSON.stringify(out, null, 2)
}

function download(content: string, mime: string, filename: string) {
  const blob = new Blob([content], { type: mime })
  const url = URL.createObjectURL(blob)
  const a = document.createElement("a")
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}

/**
 * Client-side CSV / JSON export of the currently rendered, filtered rows.
 * No new endpoint — serializes the in-memory rows the caller passes in.
 */
export function ExportButton<T>({ rows, columns, filename, disabled, className }: ExportButtonProps<T>) {
  const [open, setOpen] = useState(false)
  const isDisabled = disabled || rows.length === 0

  const exportCSV = () => {
    download(toCSV(rows, columns), "text/csv;charset=utf-8", `${filename}.csv`)
    setOpen(false)
  }
  const exportJSON = () => {
    download(toJSON(rows, columns), "application/json", `${filename}.json`)
    setOpen(false)
  }

  return (
    <div className={cn("relative inline-block", className)}>
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        disabled={isDisabled}
        aria-haspopup="menu"
        aria-expanded={open}
        className="inline-flex items-center gap-1.5 px-2.5 py-1 text-xs font-medium rounded border border-input bg-muted/50 hover:bg-accent transition-colors disabled:opacity-50"
        title="Export the current rows"
      >
        <Download className="size-3" />
        Export
        <ChevronDown className="size-3 opacity-60" />
      </button>
      {open && (
        <>
          <button
            type="button"
            aria-label="Close export menu"
            className="fixed inset-0 z-40 cursor-default"
            onClick={() => setOpen(false)}
          />
          <div
            role="menu"
            className="absolute right-0 mt-1 z-50 min-w-[9rem] rounded border border-border bg-card shadow-lg py-1 animate-fade-in"
          >
            <button
              type="button"
              role="menuitem"
              onClick={exportCSV}
              className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-left hover:bg-accent transition-colors"
            >
              <FileText className="size-3.5 text-muted-foreground" />
              Export as CSV
            </button>
            <button
              type="button"
              role="menuitem"
              onClick={exportJSON}
              className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-left hover:bg-accent transition-colors"
            >
              <FileJson className="size-3.5 text-muted-foreground" />
              Export as JSON
            </button>
          </div>
        </>
      )}
    </div>
  )
}
