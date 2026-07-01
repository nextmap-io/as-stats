import { useEffect } from "react"
import { Outlet, useLocation } from "react-router-dom"
import { Header } from "./Header"
import { FilterBar } from "@/components/filters/FilterBar"
import { CommandPalette } from "@/components/CommandPalette"
import { entryForPath, pushMRU } from "@/lib/mru"

export function AppLayout() {
  const location = useLocation()

  // Record entity detail visits into the MRU so the command palette can offer
  // quick jump-back navigation (U4).
  useEffect(() => {
    const entry = entryForPath(location.pathname)
    if (entry) pushMRU(entry)
  }, [location.pathname])

  return (
    <div className="min-h-screen bg-background">
      <Header />
      <FilterBar />
      <main className="mx-auto max-w-7xl px-4 py-6 sm:px-6 lg:px-8">
        <Outlet />
      </main>
      <CommandPalette />
    </div>
  )
}
