import { useState } from "react"
import { Link, useLocation, useNavigate } from "react-router-dom"
import { useTheme } from "@/hooks/useTheme"
import { useUnit } from "@/hooks/useUnit"
import { Search, Sun, Moon, Monitor, Activity, Menu, X } from "lucide-react"
import { cn } from "@/lib/utils"

const navItems = [
  { to: "/", label: "Dashboard" },
  { to: "/top/as", label: "Top AS" },
  { to: "/top/ip", label: "Top IP" },
  { to: "/top/prefix", label: "Prefixes" },
  { to: "/links", label: "Links" },
]

export function Header() {
  const { theme, setTheme } = useTheme()
  const { unit, toggleUnit } = useUnit()
  const [searchQuery, setSearchQuery] = useState("")
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false)
  const navigate = useNavigate()
  const location = useLocation()

  const handleSearch = (e: React.FormEvent) => {
    e.preventDefault()
    if (searchQuery.trim()) {
      navigate(`/search?q=${encodeURIComponent(searchQuery.trim())}`)
      setSearchQuery("")
    }
  }

  const cycleTheme = () => {
    const next = theme === "light" ? "dark" : theme === "dark" ? "system" : "light"
    setTheme(next)
  }

  const ThemeIcon = theme === "dark" ? Moon : theme === "light" ? Sun : Monitor

  return (
    <header className="sticky top-0 z-50 border-b border-border bg-background/80 backdrop-blur-md">
      <div className="flex h-12 items-center gap-3 px-4 lg:px-6">
        <Link to="/" className="flex items-center gap-2 text-primary font-semibold tracking-tight shrink-0">
          <Activity className="h-4 w-4" />
          <span className="text-sm">AS-Stats</span>
        </Link>

        <nav className="hidden md:flex items-center gap-0.5 ml-4" aria-label="Main navigation">
          {navItems.map(item => (
            <NavLink key={item.to} to={item.to} active={isActive(location.pathname, item.to)}>
              {item.label}
            </NavLink>
          ))}
        </nav>

        <div className="ml-auto flex items-center gap-2">
          <form onSubmit={handleSearch} className="relative hidden sm:block">
            <Search className="absolute left-2 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground" />
            <input
              type="search"
              placeholder="AS, IP, prefix..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              aria-label="Search AS numbers, IP addresses, or prefixes"
              className="h-8 w-40 lg:w-56 rounded border border-input bg-muted/50 pl-7 pr-3 text-xs placeholder:text-muted-foreground/60 outline-none focus-visible:ring-1 focus-visible:ring-ring transition-all"
            />
          </form>

          <button
            onClick={toggleUnit}
            className="inline-flex h-8 items-center justify-center rounded border border-input bg-muted/50 hover:bg-accent transition-colors px-2 text-[10px] font-bold tabular-nums tracking-tight"
            aria-label={`Switch unit (current: ${unit})`}
            title={unit === "bps" ? "Showing bit rate — click for bytes" : "Showing bytes — click for bit rate"}
          >
            {unit === "bps" ? "bps" : "B"}
          </button>

          <button
            onClick={cycleTheme}
            className="inline-flex h-8 w-8 items-center justify-center rounded border border-input bg-muted/50 hover:bg-accent transition-colors"
            aria-label={`Switch theme (current: ${theme})`}
          >
            <ThemeIcon className="h-3.5 w-3.5" />
          </button>

          <button
            onClick={() => setMobileMenuOpen(!mobileMenuOpen)}
            className="inline-flex md:hidden h-8 w-8 items-center justify-center rounded border border-input bg-muted/50 hover:bg-accent transition-colors"
            aria-label="Toggle navigation menu"
            aria-expanded={mobileMenuOpen}
          >
            {mobileMenuOpen ? <X className="h-3.5 w-3.5" /> : <Menu className="h-3.5 w-3.5" />}
          </button>
        </div>
      </div>

      {mobileMenuOpen && (
        <nav className="md:hidden border-t border-border bg-background p-3 animate-fade-in" aria-label="Mobile navigation">
          <form onSubmit={handleSearch} className="relative mb-3 sm:hidden">
            <Search className="absolute left-2 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground" />
            <input
              type="search"
              placeholder="Search AS, IP, prefix..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              aria-label="Search"
              className="h-8 w-full rounded border border-input bg-muted/50 pl-7 pr-3 text-xs outline-none"
            />
          </form>
          <div className="flex flex-col gap-0.5">
            {navItems.map(item => (
              <Link
                key={item.to}
                to={item.to}
                onClick={() => setMobileMenuOpen(false)}
                className={cn(
                  "px-3 py-2 text-xs font-medium rounded transition-colors",
                  isActive(location.pathname, item.to)
                    ? "bg-primary/10 text-primary"
                    : "text-muted-foreground hover:text-foreground hover:bg-accent"
                )}
                aria-current={isActive(location.pathname, item.to) ? "page" : undefined}
              >
                {item.label}
              </Link>
            ))}
          </div>
        </nav>
      )}
    </header>
  )
}

function NavLink({ to, active, children }: { to: string; active: boolean; children: React.ReactNode }) {
  return (
    <Link
      to={to}
      className={cn(
        "px-2.5 py-1 text-xs font-medium rounded transition-colors",
        active
          ? "bg-primary/10 text-primary"
          : "text-muted-foreground hover:text-foreground hover:bg-accent"
      )}
      aria-current={active ? "page" : undefined}
    >
      {children}
    </Link>
  )
}

function isActive(pathname: string, to: string): boolean {
  if (to === "/") return pathname === "/"
  return pathname.startsWith(to)
}
