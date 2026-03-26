import { useState } from "react"
import { Link, useNavigate } from "react-router-dom"
import { useTheme } from "@/providers/ThemeProvider"
import { Search, Sun, Moon, Monitor, Activity } from "lucide-react"

export function Header() {
  const { theme, setTheme } = useTheme()
  const [searchQuery, setSearchQuery] = useState("")
  const navigate = useNavigate()

  const handleSearch = (e: React.FormEvent) => {
    e.preventDefault()
    if (searchQuery.trim()) {
      navigate(`/search?q=${encodeURIComponent(searchQuery.trim())}`)
    }
  }

  const cycleTheme = () => {
    const next = theme === "light" ? "dark" : theme === "dark" ? "system" : "light"
    setTheme(next)
  }

  const ThemeIcon = theme === "dark" ? Moon : theme === "light" ? Sun : Monitor

  return (
    <header className="sticky top-0 z-50 border-b border-border bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/60">
      <div className="flex h-14 items-center gap-4 px-6">
        <Link to="/" className="flex items-center gap-2 font-semibold text-foreground">
          <Activity className="h-5 w-5" />
          <span>AS-Stats</span>
        </Link>

        <nav className="hidden md:flex items-center gap-1 ml-4">
          <NavLink to="/">Dashboard</NavLink>
          <NavLink to="/top/as">Top AS</NavLink>
          <NavLink to="/top/ip">Top IP</NavLink>
          <NavLink to="/top/prefix">Top Prefix</NavLink>
          <NavLink to="/links">Links</NavLink>
        </nav>

        <div className="ml-auto flex items-center gap-2">
          <form onSubmit={handleSearch} className="relative">
            <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
            <input
              type="search"
              placeholder="Search AS, IP, prefix..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="h-9 w-64 rounded-md border border-input bg-background pl-8 pr-3 text-sm outline-none focus:ring-1 focus:ring-ring"
            />
          </form>

          <button
            onClick={cycleTheme}
            className="inline-flex h-9 w-9 items-center justify-center rounded-md border border-input bg-background hover:bg-accent"
            title={`Theme: ${theme}`}
          >
            <ThemeIcon className="h-4 w-4" />
          </button>
        </div>
      </div>
    </header>
  )
}

function NavLink({ to, children }: { to: string; children: React.ReactNode }) {
  return (
    <Link
      to={to}
      className="px-3 py-1.5 text-sm font-medium text-muted-foreground hover:text-foreground rounded-md hover:bg-accent transition-colors"
    >
      {children}
    </Link>
  )
}
