import { useEffect } from "react"
import { Outlet, useLocation } from "react-router-dom"
import { Header } from "./Header"
import { FilterBar } from "@/components/filters/FilterBar"
import { CommandPalette } from "@/components/CommandPalette"
import { entryForPath, pushMRU } from "@/lib/mru"

// Pages that keep their own clock (Live threats: the last minutes; Flow search:
// its Period field; Alerts, BGP, Admin and search: no time window at all). The
// global time bar said "24h" above them while their numbers covered something
// else, so it is not shown there.
const OWN_CLOCK = ["/live", "/flows", "/alerts", "/bgp", "/admin", "/search"]

export function AppLayout() {
  const location = useLocation()
  const showTimeBar = !OWN_CLOCK.includes(location.pathname)

  // Record entity detail visits into the MRU so the command palette can offer
  // quick jump-back navigation (U4).
  useEffect(() => {
    const entry = entryForPath(location.pathname)
    if (entry) pushMRU(entry)
  }, [location.pathname])

  return (
    <div className="min-h-screen bg-background">
      <Header />
      {showTimeBar && <FilterBar />}
      <main className="mx-auto max-w-[1400px] px-4 pb-16 pt-6 sm:px-6 lg:px-8">
        <Outlet />
      </main>
      <CommandPalette />
    </div>
  )
}
