import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react"
import { useNavigate } from "react-router-dom"
import { Search, CornerDownLeft, ArrowRight, Clock, Network, Globe, Cable } from "lucide-react"
import { useSearch } from "@/hooks/useApi"
import { useFilters } from "@/hooks/useFilters"
import { useFeatureFlags } from "@/hooks/useFeatures"
import { detectRedirect } from "@/lib/detectRedirect"
import { readMRU, type MRUEntry } from "@/lib/mru"
import type { Features } from "@/lib/types"
import { cn } from "@/lib/utils"

interface RouteDef {
  label: string
  path: string
  feature?: keyof Features
}

const ROUTES: RouteDef[] = [
  { label: "Dashboard", path: "/" },
  { label: "Top AS", path: "/top/as" },
  { label: "Top IP", path: "/top/ip" },
  { label: "Prefixes", path: "/top/prefix" },
  { label: "Countries", path: "/countries" },
  { label: "Links", path: "/links" },
  { label: "Capacity", path: "/capacity" },
  { label: "Changes", path: "/changes" },
  { label: "Protocols", path: "/top/protocol", feature: "port_stats" },
  { label: "Ports", path: "/top/port", feature: "port_stats" },
  { label: "Flow Search", path: "/flows", feature: "flow_search" },
  { label: "Conversations", path: "/conversations", feature: "flow_search" },
  { label: "Live Threats", path: "/live", feature: "alerts" },
  { label: "Alerts", path: "/alerts", feature: "alerts" },
  { label: "BGP Blocks", path: "/bgp", feature: "bgp" },
  { label: "Admin", path: "/admin" },
]

// Subsequence fuzzy match: returns a score (higher = better) or null when the
// query characters don't all appear in order. Contiguous runs are rewarded.
function fuzzyScore(query: string, text: string): number | null {
  const q = query.toLowerCase()
  const t = text.toLowerCase()
  let ti = 0
  let score = 0
  let streak = 0
  for (const ch of q) {
    const idx = t.indexOf(ch, ti)
    if (idx === -1) return null
    streak = idx === ti ? streak + 1 : 0
    score += 1 + streak
    ti = idx + 1
  }
  return score - t.length * 0.01
}

interface PaletteItem {
  id: string
  label: string
  sublabel?: string
  icon: ReactNode
  onSelect: () => void
}

const MRU_ICON: Record<MRUEntry["kind"], ReactNode> = {
  as: <Network className="size-3.5" aria-hidden />,
  ip: <Globe className="size-3.5" aria-hidden />,
  link: <Cable className="size-3.5" aria-hidden />,
}

/**
 * CommandPalette (U4) — a custom accessible command menu opened with ⌘/Ctrl-K
 * or "/". It fuzzy-matches app routes, pipes free text to AS search, routes raw
 * IP/AS input via detectRedirect, and lists recently-viewed entities. The active
 * period/from/to window is preserved on every navigation via filterSearch.
 *
 * Accessibility: combobox + listbox pattern, arrow-key navigation, Enter to
 * activate, Esc / click-away to dismiss, and a focus trap around the dialog.
 */
export function CommandPalette() {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState("")
  const [active, setActive] = useState(0)
  const navigate = useNavigate()
  const { filterSearch } = useFilters()
  const features = useFeatureFlags()

  const inputRef = useRef<HTMLInputElement>(null)
  const dialogRef = useRef<HTMLDivElement>(null)
  const listRef = useRef<HTMLUListElement>(null)

  // AS search — only fires once the query is long enough (hook guards <2 chars).
  const { data: searchData } = useSearch(query.trim())

  const close = useCallback(() => {
    setOpen(false)
    setQuery("")
    setActive(0)
  }, [])

  const openPalette = useCallback(() => {
    setQuery("")
    setActive(0)
    setOpen(true)
  }, [])

  // Global open shortcuts: ⌘/Ctrl-K anywhere, and "/" when not already typing.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault()
        setOpen((o) => !o)
        setQuery("")
        setActive(0)
        return
      }
      if (e.key === "/" && !open) {
        const el = document.activeElement
        const tag = el?.tagName
        const editable = el instanceof HTMLElement && el.isContentEditable
        if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || editable) return
        e.preventDefault()
        openPalette()
      }
    }
    document.addEventListener("keydown", onKey)
    return () => document.removeEventListener("keydown", onKey)
  }, [open, openPalette])

  // Focus the input when the dialog opens.
  useEffect(() => {
    if (open) {
      const id = window.setTimeout(() => inputRef.current?.focus(), 0)
      return () => window.clearTimeout(id)
    }
  }, [open])

  const go = useCallback(
    (path: string) => {
      navigate(`${path}${path.includes("?") ? "" : filterSearch}`)
      close()
    },
    [navigate, filterSearch, close],
  )

  const items = useMemo<PaletteItem[]>(() => {
    const q = query.trim()
    const out: PaletteItem[] = []

    // 1. Direct entity jump (raw AS / IP / prefix).
    const redirect = detectRedirect(q)
    if (redirect) {
      out.push({
        id: `go:${redirect}`,
        label: `Go to ${q}`,
        sublabel: redirect,
        icon: <ArrowRight className="size-3.5" aria-hidden />,
        onSelect: () => go(redirect),
      })
    }

    // 2. Route matches (fuzzy when there's a query, else all enabled routes).
    const enabled = ROUTES.filter((r) => !r.feature || features[r.feature])
    const routeMatches = q
      ? enabled
          .map((r) => ({ r, score: fuzzyScore(q, r.label) }))
          .filter((x): x is { r: RouteDef; score: number } => x.score !== null)
          .sort((a, b) => b.score - a.score)
          .map((x) => x.r)
      : enabled
    for (const r of routeMatches) {
      out.push({
        id: `route:${r.path}`,
        label: r.label,
        sublabel: r.path,
        icon: <ArrowRight className="size-3.5" aria-hidden />,
        onSelect: () => go(r.path),
      })
    }

    // 3. Recently-viewed entities (only with no query).
    if (!q) {
      for (const e of readMRU()) {
        out.push({
          id: `mru:${e.path}`,
          label: e.label,
          sublabel: "Recently viewed",
          icon: MRU_ICON[e.kind] ?? <Clock className="size-3.5" aria-hidden />,
          onSelect: () => go(e.path),
        })
      }
    }

    // 4. AS search results.
    if (q.length >= 2 && searchData?.data) {
      for (const as of searchData.data.slice(0, 8)) {
        out.push({
          id: `as:${as.number}`,
          label: `AS${as.number} ${as.name ?? ""}`.trim(),
          sublabel: as.country || "Autonomous System",
          icon: <Network className="size-3.5" aria-hidden />,
          onSelect: () => go(`/as/${as.number}`),
        })
      }
    }

    return out
  }, [query, features, searchData, go])

  // Clamp the active index to the current item set without an effect.
  const activeIndex = active < items.length ? active : 0

  // Scroll the active option into view.
  useEffect(() => {
    if (!open || !listRef.current) return
    const el = listRef.current.querySelector<HTMLElement>(`[data-index="${activeIndex}"]`)
    el?.scrollIntoView({ block: "nearest" })
  }, [activeIndex, open])

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Escape") {
      e.preventDefault()
      close()
    } else if (e.key === "ArrowDown") {
      e.preventDefault()
      setActive((a) => (items.length ? (a + 1) % items.length : 0))
    } else if (e.key === "ArrowUp") {
      e.preventDefault()
      setActive((a) => (items.length ? (a - 1 + items.length) % items.length : 0))
    } else if (e.key === "Enter") {
      e.preventDefault()
      items[activeIndex]?.onSelect()
    } else if (e.key === "Tab") {
      // Focus trap: keep focus on the input (the only interactive control).
      e.preventDefault()
      inputRef.current?.focus()
    }
  }

  if (!open) return null

  return (
    <div
      className="fixed inset-0 z-[100] flex items-start justify-center bg-black/50 pt-[12vh] px-4 animate-fade-in"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) close()
      }}
    >
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-label="Command palette"
        className="w-full max-w-lg overflow-hidden rounded-lg border border-border bg-popover shadow-2xl"
        onKeyDown={onKeyDown}
      >
        <div className="flex items-center gap-2 border-b border-border px-3">
          <Search className="size-4 text-muted-foreground shrink-0" aria-hidden />
          <input
            ref={inputRef}
            value={query}
            onChange={(e) => {
              setQuery(e.target.value)
              setActive(0)
            }}
            role="combobox"
            aria-expanded="true"
            aria-controls="command-palette-list"
            aria-activedescendant={items[activeIndex] ? `cp-opt-${activeIndex}` : undefined}
            aria-autocomplete="list"
            aria-label="Search pages, AS numbers, IPs, prefixes"
            placeholder="Search pages, AS, IP, prefix…"
            className="h-11 w-full bg-transparent text-sm outline-none placeholder:text-muted-foreground/60"
          />
          <kbd className="hidden sm:inline text-[10px] text-muted-foreground border border-border rounded px-1 py-0.5">
            Esc
          </kbd>
        </div>

        <ul ref={listRef} id="command-palette-list" role="listbox" className="max-h-80 overflow-y-auto py-1">
          {items.length === 0 ? (
            <li className="px-3 py-6 text-center text-xs text-muted-foreground">No matches</li>
          ) : (
            items.map((item, i) => (
              <li key={item.id} data-index={i}>
                <button
                  type="button"
                  id={`cp-opt-${i}`}
                  role="option"
                  aria-selected={i === activeIndex}
                  onMouseEnter={() => setActive(i)}
                  onClick={() => item.onSelect()}
                  className={cn(
                    "flex w-full items-center gap-2.5 px-3 py-2 text-left text-sm transition-colors",
                    i === activeIndex ? "bg-accent text-accent-foreground" : "hover:bg-accent/50",
                  )}
                >
                  <span className="text-muted-foreground shrink-0">{item.icon}</span>
                  <span className="flex-1 truncate">{item.label}</span>
                  {item.sublabel && (
                    <span className="text-[10px] text-muted-foreground truncate max-w-[40%]">{item.sublabel}</span>
                  )}
                  {i === activeIndex && <CornerDownLeft className="size-3 text-muted-foreground shrink-0" aria-hidden />}
                </button>
              </li>
            ))
          )}
        </ul>
      </div>
    </div>
  )
}
