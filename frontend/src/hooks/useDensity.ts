import { useCallback, useSyncExternalStore } from "react"

// Density toggle (U7). Comfortable is the default; compact tightens row padding
// and font scale across every DataTable. Backed by a tiny external store so the
// Header control and every table stay in sync without threading a provider
// through the tree. The active value is also mirrored onto the root element's
// `data-density` attribute for any CSS that wants to hook it.
export type Density = "comfortable" | "compact"

const STORAGE_KEY = "as-stats-density"

function readInitial(): Density {
  try {
    return localStorage.getItem(STORAGE_KEY) === "compact" ? "compact" : "comfortable"
  } catch {
    return "comfortable"
  }
}

let current: Density = readInitial()
const listeners = new Set<() => void>()

function apply(d: Density) {
  try {
    document.documentElement.dataset.density = d
  } catch {
    /* SSR / no document — ignore */
  }
}

// Reflect the persisted value onto the root at module load.
apply(current)

function subscribe(cb: () => void): () => void {
  listeners.add(cb)
  return () => {
    listeners.delete(cb)
  }
}

function getSnapshot(): Density {
  return current
}

export function setDensity(d: Density) {
  if (d === current) return
  current = d
  try {
    localStorage.setItem(STORAGE_KEY, d)
  } catch {
    /* ignore */
  }
  apply(d)
  listeners.forEach((l) => l())
}

export function useDensity() {
  const density = useSyncExternalStore(subscribe, getSnapshot, getSnapshot)
  const toggle = useCallback(() => {
    setDensity(current === "compact" ? "comfortable" : "compact")
  }, [])
  return { density, setDensity, toggle }
}
