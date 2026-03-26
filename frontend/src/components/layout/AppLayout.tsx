import { Outlet } from "react-router-dom"
import { Header } from "./Header"
import { FilterBar } from "@/components/filters/FilterBar"

export function AppLayout() {
  return (
    <div className="min-h-screen bg-background">
      <Header />
      <FilterBar />
      <main className="mx-auto max-w-7xl px-4 py-6 sm:px-6 lg:px-8">
        <Outlet />
      </main>
    </div>
  )
}
