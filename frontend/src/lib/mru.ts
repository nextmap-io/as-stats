// Most-recently-used entity list (U4). Persists the last few detail pages the
// operator visited (AS / IP / link) so the command palette can offer quick
// jump-back navigation. Stored in localStorage; capped and deduped by path.
export interface MRUEntry {
  /** Route to navigate to (without query string). */
  path: string
  /** Human label shown in the palette. */
  label: string
  /** Entity kind, for the icon/section. */
  kind: "as" | "ip" | "link"
}

const STORAGE_KEY = "as-stats-mru"
const MAX = 8

export function readMRU(): MRUEntry[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return []
    const parsed: unknown = JSON.parse(raw)
    if (!Array.isArray(parsed)) return []
    return parsed.filter(
      (e): e is MRUEntry =>
        !!e &&
        typeof (e as MRUEntry).path === "string" &&
        typeof (e as MRUEntry).label === "string",
    )
  } catch {
    return []
  }
}

export function pushMRU(entry: MRUEntry): void {
  try {
    const existing = readMRU().filter((e) => e.path !== entry.path)
    const next = [entry, ...existing].slice(0, MAX)
    localStorage.setItem(STORAGE_KEY, JSON.stringify(next))
  } catch {
    /* ignore quota / unavailable */
  }
}

// Derive an MRU entry from a pathname, or null if it isn't an entity detail
// route worth remembering.
export function entryForPath(pathname: string): MRUEntry | null {
  const as = pathname.match(/^\/as\/(\d+)$/)
  if (as) return { path: pathname, label: `AS${as[1]}`, kind: "as" }
  const ip = pathname.match(/^\/ip\/(.+)$/)
  if (ip) return { path: pathname, label: decodeURIComponent(ip[1]), kind: "ip" }
  const link = pathname.match(/^\/link\/(.+)$/)
  if (link) return { path: pathname, label: decodeURIComponent(link[1]), kind: "link" }
  return null
}
