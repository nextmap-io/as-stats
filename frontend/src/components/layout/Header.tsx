import { useState } from "react"
import { Link, useLocation, useNavigate } from "react-router-dom"
import { useTheme } from "@/hooks/useTheme"
import { useUnit } from "@/hooks/useUnit"
import { useStatus } from "@/hooks/useApi"
import { useFeatureFlags } from "@/hooks/useFeatures"
import { useQuery } from "@tanstack/react-query"
import { api } from "@/lib/api"
import { Search, Sun, Moon, Monitor, Activity, Menu, X, LogOut, Bell, Shield } from "lucide-react"
import { cn } from "@/lib/utils"

type NavItem = { to: string; label: string }

const baseNav: NavItem[] = [
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
    const q = searchQuery.trim()
    if (!q) return

    // Direct navigation for known patterns
    const asMatch = q.match(/^[Aa][Ss]?(\d+)$/)
    if (asMatch) {
      navigate(`/as/${asMatch[1]}`)
      setSearchQuery("")
      return
    }

    // IP address (v4 or v6)
    if (/^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}$/.test(q) || q.includes(":")) {
      // Strip /prefix if present for IP navigation
      const ip = q.split("/")[0]
      navigate(`/ip/${encodeURIComponent(ip)}`)
      setSearchQuery("")
      return
    }

    // Pure number = ASN
    if (/^\d+$/.test(q)) {
      navigate(`/as/${q}`)
      setSearchQuery("")
      return
    }

    // Text search
    navigate(`/search?q=${encodeURIComponent(q)}`)
    setSearchQuery("")
  }

  const { data: userData } = useQuery({
    queryKey: ["auth-me"],
    queryFn: () => api.me(),
    staleTime: 300_000,
    retry: false,
  })
  const user = userData?.data

  const { data: statusData } = useStatus()
  const routerCount = statusData?.data?.routers?.length || 0
  const isHealthy = routerCount > 0
  const statusTitle = statusData?.data?.routers
    ?.map(r => `${r.router_ip}: ${r.flow_count} flows`)
    .join("\n") || "No data"

  // Feature flags
  const features = useFeatureFlags()

  // Build nav items dynamically based on enabled features
  const navItems: NavItem[] = [...baseNav]
  if (features.port_stats) {
    navItems.push({ to: "/top/protocol", label: "Protocols" })
    navItems.push({ to: "/top/port", label: "Ports" })
  }
  if (features.flow_search) {
    navItems.push({ to: "/flows", label: "Flow Search" })
  }

  // Active alerts count for the badge
  const { data: alertsSummary } = useQuery({
    queryKey: ["alerts-summary"],
    queryFn: () => api.alertsSummary(),
    refetchInterval: 30_000,
    enabled: features.alerts,
    retry: false,
  })
  const activeAlerts = alertsSummary?.data?.total || 0
  const criticalAlerts = alertsSummary?.data?.by_severity?.critical || 0

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
        <span
          className={`w-2 h-2 rounded-full shrink-0 ${isHealthy ? "bg-success" : "bg-destructive"} animate-pulse`}
          title={statusTitle}
        />

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

          {features.alerts && (
            <Link
              to="/alerts"
              className={cn(
                "relative inline-flex h-8 w-8 items-center justify-center rounded border transition-colors",
                criticalAlerts > 0
                  ? "border-destructive/50 bg-destructive/20 text-destructive hover:bg-destructive/30 animate-pulse"
                  : activeAlerts > 0
                    ? "border-warning/50 bg-warning/20 text-warning hover:bg-warning/30"
                    : "border-input bg-muted/50 hover:bg-accent",
              )}
              aria-label={`Alerts (${activeAlerts} active)`}
              title={`${activeAlerts} active alert${activeAlerts !== 1 ? "s" : ""}${criticalAlerts > 0 ? ` (${criticalAlerts} critical)` : ""}`}
            >
              <Bell className="h-3.5 w-3.5" />
              {activeAlerts > 0 && (
                <span className="absolute -top-1 -right-1 inline-flex min-w-[14px] h-[14px] items-center justify-center rounded-full bg-destructive text-destructive-foreground text-[9px] font-bold px-1 leading-none">
                  {activeAlerts > 99 ? "99+" : activeAlerts}
                </span>
              )}
            </Link>
          )}

          {(features.alerts || (user && user.role === "admin")) && (
            <Link
              to="/admin"
              className="inline-flex h-8 w-8 items-center justify-center rounded border border-input bg-muted/50 hover:bg-accent transition-colors"
              aria-label="Admin"
              title="Admin console"
            >
              <Shield className="h-3.5 w-3.5" />
            </Link>
          )}

          <button
            onClick={toggleUnit}
            className="inline-flex h-8 items-center justify-center rounded border border-input bg-muted/50 hover:bg-accent transition-colors px-2 text-[10px] font-bold tabular-nums tracking-tight"
            aria-label={`Switch unit (current: ${unit})`}
            title={`Showing ${unit} — click to cycle`}
          >
            {unit}
          </button>

          <button
            onClick={cycleTheme}
            className="inline-flex h-8 w-8 items-center justify-center rounded border border-input bg-muted/50 hover:bg-accent transition-colors"
            aria-label={`Switch theme (current: ${theme})`}
          >
            <ThemeIcon className="h-3.5 w-3.5" />
          </button>

          {user && (
            <button
              onClick={() => {
                api.logout().then(() => {
                  window.location.href = "/auth/login"
                })
              }}
              className="inline-flex h-8 items-center gap-1.5 rounded border border-input bg-muted/50 hover:bg-destructive/10 hover:text-destructive hover:border-destructive/30 transition-colors px-2"
              title={`${user.name || user.email} — click to logout`}
              aria-label="Logout"
            >
              <LogOut className="h-3.5 w-3.5" />
              <span className="hidden lg:inline text-xs">{user.name || user.email}</span>
            </button>
          )}

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
