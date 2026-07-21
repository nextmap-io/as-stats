import { useEffect, useRef, useState } from "react"
import { Link, useLocation, useNavigate } from "react-router-dom"
import { useTheme } from "@/hooks/useTheme"
import { useUnit } from "@/hooks/useUnit"
import { useDensity } from "@/hooks/useDensity"
import { useStatus } from "@/hooks/useApi"
import { useFeatureFlags } from "@/hooks/useFeatures"
import { useQuery } from "@tanstack/react-query"
import { api } from "@/lib/api"
import { Search, Sun, Moon, Monitor, Activity, Menu, X, LogOut, Bell, ChevronDown, Rows2, Rows3 } from "lucide-react"
import { cn } from "@/lib/utils"

type NavItem = { to: string; label: string }
type NavGroup = { label: string; items: NavItem[] }

export function Header() {
  const { theme, setTheme } = useTheme()
  const { unit, toggleUnit } = useUnit()
  const { density, toggle: toggleDensity } = useDensity()
  const [searchQuery, setSearchQuery] = useState("")
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false)
  // Which top-level dropdown is open (by group label), or null. Only one at a time.
  const [openMenu, setOpenMenu] = useState<string | null>(null)
  const navRef = useRef<HTMLElement>(null)
  const navigate = useNavigate()
  const location = useLocation()

  const handleSearch = (e: React.FormEvent) => {
    e.preventDefault()
    const q = searchQuery.trim()
    if (!q) return
    // Any successful search navigates away — collapse open menus first.
    setOpenMenu(null)
    setMobileMenuOpen(false)

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
  const isAdmin = user?.role === "admin"

  // Build the grouped navigation. Each group's items are feature-gated; a group
  // with no visible items is dropped entirely so empty menus never render.
  const groups: NavGroup[] = []

  const traffic: NavItem[] = [
    { to: "/top/as", label: "Top AS" },
    { to: "/top/ip", label: "Top IP" },
    { to: "/top/prefix", label: "Prefixes" },
    { to: "/countries", label: "Countries" },
  ]
  if (features.port_stats) {
    traffic.push({ to: "/top/protocol", label: "Protocols" })
    traffic.push({ to: "/top/port", label: "Ports" })
  }
  groups.push({ label: "Traffic", items: traffic })

  groups.push({
    label: "Network",
    items: [
      { to: "/links", label: "Links" },
      { to: "/capacity", label: "Capacity" },
      { to: "/changes", label: "Changes" },
    ],
  })

  if (features.flow_search) {
    groups.push({
      label: "Forensics",
      items: [
        { to: "/flows", label: "Flow Search" },
        { to: "/conversations", label: "Conversations" },
      ],
    })
  }

  const security: NavItem[] = []
  if (features.alerts) {
    security.push({ to: "/live", label: "Live Threats" })
    security.push({ to: "/alerts", label: "Alerts" })
  }
  if (features.bgp) {
    security.push({ to: "/bgp", label: "BGP Blocks" })
  }
  if (security.length) groups.push({ label: "Security", items: security })

  const system: NavItem[] = []
  if (isAdmin) system.push({ to: "/status", label: "Status" })
  // The admin console is reachable by alert-feature viewers too (writes are
  // still server-gated), mirroring the previous shield-icon visibility.
  if (isAdmin || features.alerts) system.push({ to: "/admin", label: "Admin" })
  if (system.length) groups.push({ label: "System", items: system })

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

  // Close the open dropdown on outside click or Escape. Only wired while a menu
  // is open so we don't keep global listeners around needlessly. (Item clicks
  // close via onClose; search navigation closes in handleSearch.)
  useEffect(() => {
    if (!openMenu) return
    const onDown = (e: MouseEvent) => {
      if (navRef.current && !navRef.current.contains(e.target as Node)) setOpenMenu(null)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpenMenu(null)
    }
    document.addEventListener("mousedown", onDown)
    document.addEventListener("keydown", onKey)
    return () => {
      document.removeEventListener("mousedown", onDown)
      document.removeEventListener("keydown", onKey)
    }
  }, [openMenu])

  return (
    <header className="sticky top-0 z-50 border-b border-border bg-background/80 backdrop-blur-md">
      <div className="flex h-12 items-center gap-3 px-4 lg:px-6">
        <Link to="/" className="flex items-center gap-2 text-primary font-semibold tracking-tight shrink-0">
          <Activity className="size-4" />
          <span className="text-sm">AS-Stats</span>
        </Link>
        <span
          className={`size-2 rounded-full shrink-0 ${isHealthy ? "bg-success" : "bg-destructive"} animate-pulse`}
          title={statusTitle}
        />

        {/* Desktop nav: a Dashboard link plus grouped dropdown menus. No overflow
            container here — a scroll ancestor would clip the dropdown panels. */}
        <nav ref={navRef} className="hidden md:flex items-center gap-0.5 ml-4" aria-label="Main navigation">
          <NavLink to="/" active={isActive(location.pathname, "/")}>
            Dashboard
          </NavLink>
          {groups.map(group => (
            <NavMenu
              key={group.label}
              group={group}
              pathname={location.pathname}
              open={openMenu === group.label}
              onToggle={() => setOpenMenu(prev => (prev === group.label ? null : group.label))}
              // Hover-swap: once any menu is open, hovering another switches to it.
              onHover={() => setOpenMenu(prev => (prev !== null ? group.label : prev))}
              onClose={() => setOpenMenu(null)}
            />
          ))}
        </nav>

        <div className="ml-auto flex items-center gap-2">
          <form onSubmit={handleSearch} className="relative hidden sm:block">
            <Search className="absolute left-2 top-1/2 -translate-y-1/2 size-3.5 text-muted-foreground" />
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
                "relative inline-flex size-8 items-center justify-center rounded border transition-colors",
                criticalAlerts > 0
                  ? "border-destructive/50 bg-destructive/20 text-destructive hover:bg-destructive/30 animate-pulse"
                  : activeAlerts > 0
                    ? "border-warning/50 bg-warning/20 text-warning hover:bg-warning/30"
                    : "border-input bg-muted/50 hover:bg-accent",
              )}
              aria-label={`Alerts (${activeAlerts} active)`}
              title={`${activeAlerts} active alert${activeAlerts !== 1 ? "s" : ""}${criticalAlerts > 0 ? ` (${criticalAlerts} critical)` : ""}`}
            >
              <Bell className="size-3.5" />
              {activeAlerts > 0 && (
                <span className="absolute -top-1 -right-1 inline-flex min-w-[14px] h-[14px] items-center justify-center rounded-full bg-destructive text-destructive-foreground text-[9px] font-bold px-1 leading-none">
                  {activeAlerts > 99 ? "99+" : activeAlerts}
                </span>
              )}
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
            onClick={toggleDensity}
            className="inline-flex size-8 items-center justify-center rounded border border-input bg-muted/50 hover:bg-accent transition-colors"
            aria-label={`Table density: ${density} — click to toggle`}
            aria-pressed={density === "compact"}
            title={`Density: ${density} — click to toggle`}
          >
            {density === "compact" ? <Rows2 className="size-3.5" /> : <Rows3 className="size-3.5" />}
          </button>

          <button
            onClick={cycleTheme}
            className="inline-flex size-8 items-center justify-center rounded border border-input bg-muted/50 hover:bg-accent transition-colors"
            aria-label={`Switch theme (current: ${theme})`}
          >
            <ThemeIcon className="size-3.5" />
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
              <LogOut className="size-3.5" />
              <span className="hidden lg:inline text-xs">{user.name || user.email}</span>
            </button>
          )}

          <button
            onClick={() => setMobileMenuOpen(!mobileMenuOpen)}
            className="inline-flex md:hidden size-8 items-center justify-center rounded border border-input bg-muted/50 hover:bg-accent transition-colors"
            aria-label="Toggle navigation menu"
            aria-expanded={mobileMenuOpen}
          >
            {mobileMenuOpen ? <X className="size-3.5" /> : <Menu className="size-3.5" />}
          </button>
        </div>
      </div>

      {mobileMenuOpen && (
        <nav className="md:hidden border-t border-border bg-background p-3 animate-fade-in" aria-label="Mobile navigation">
          <form onSubmit={handleSearch} className="relative mb-3 sm:hidden">
            <Search className="absolute left-2 top-1/2 -translate-y-1/2 size-3.5 text-muted-foreground" />
            <input
              type="search"
              placeholder="Search AS, IP, prefix..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              aria-label="Search"
              className="h-8 w-full rounded border border-input bg-muted/50 pl-7 pr-3 text-xs outline-none"
            />
          </form>
          <div className="flex flex-col gap-3">
            <MobileLink to="/" label="Dashboard" pathname={location.pathname} onNavigate={() => setMobileMenuOpen(false)} />
            {groups.map(group => (
              <div key={group.label}>
                <div className="px-3 pb-1 text-[10px] font-semibold uppercase tracking-widest text-muted-foreground/70">
                  {group.label}
                </div>
                <div className="flex flex-col gap-0.5">
                  {group.items.map(item => (
                    <MobileLink
                      key={item.to}
                      to={item.to}
                      label={item.label}
                      pathname={location.pathname}
                      onNavigate={() => setMobileMenuOpen(false)}
                    />
                  ))}
                </div>
              </div>
            ))}
          </div>
        </nav>
      )}
    </header>
  )
}

// NavMenu renders one top-level dropdown: a trigger button plus a panel of
// links. The panel is absolutely positioned and must not sit inside any
// overflow-clipping ancestor (see the FilterBar Custom-popover fix).
function NavMenu({
  group,
  pathname,
  open,
  onToggle,
  onHover,
  onClose,
}: {
  group: NavGroup
  pathname: string
  open: boolean
  onToggle: () => void
  onHover: () => void
  onClose: () => void
}) {
  const active = group.items.some(i => isActive(pathname, i.to))
  return (
    <div className="relative" onMouseEnter={onHover}>
      <button
        type="button"
        onClick={onToggle}
        aria-haspopup="true"
        aria-expanded={open}
        className={cn(
          "inline-flex items-center gap-1 px-2.5 py-1 text-xs font-medium rounded transition-colors",
          active || open
            ? "bg-primary/10 text-primary"
            : "text-muted-foreground hover:text-foreground hover:bg-accent",
        )}
      >
        {group.label}
        <ChevronDown className={cn("size-3 transition-transform", open && "rotate-180")} aria-hidden />
      </button>
      {open && (
        <div
          role="menu"
          className="absolute left-0 top-full mt-1 z-50 min-w-[180px] rounded border border-border bg-card p-1 shadow-lg animate-fade-in"
        >
          {group.items.map(item => (
            <Link
              key={item.to}
              to={item.to}
              role="menuitem"
              onClick={onClose}
              className={cn(
                "block px-3 py-1.5 text-xs font-medium rounded transition-colors",
                isActive(pathname, item.to)
                  ? "bg-primary/10 text-primary"
                  : "text-muted-foreground hover:text-foreground hover:bg-accent",
              )}
              aria-current={isActive(pathname, item.to) ? "page" : undefined}
            >
              {item.label}
            </Link>
          ))}
        </div>
      )}
    </div>
  )
}

function MobileLink({
  to,
  label,
  pathname,
  onNavigate,
}: {
  to: string
  label: string
  pathname: string
  onNavigate: () => void
}) {
  return (
    <Link
      to={to}
      onClick={onNavigate}
      className={cn(
        "px-3 py-2 text-xs font-medium rounded transition-colors",
        isActive(pathname, to)
          ? "bg-primary/10 text-primary"
          : "text-muted-foreground hover:text-foreground hover:bg-accent",
      )}
      aria-current={isActive(pathname, to) ? "page" : undefined}
    >
      {label}
    </Link>
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
