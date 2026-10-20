import { useEffect, useRef, useState } from "react"
import { Link, useLocation, useNavigate } from "react-router-dom"
import { useTheme } from "@/hooks/useTheme"
import { useUnit } from "@/hooks/useUnit"
import { useDensity } from "@/hooks/useDensity"
import { useAlertsSummary, useLiveStatus } from "@/hooks/useApi"
import { useFeatureFlags } from "@/hooks/useFeatures"
import { useCurrentUser } from "@/hooks/useAuth"
import { api } from "@/lib/api"
import { describeRouters } from "@/lib/status"
import type { LiveStatus } from "@/lib/types"
import { Search, Sun, Moon, Monitor, Menu, X, LogOut, Bell, ChevronDown, Rows2, Rows3 } from "lucide-react"
import { cn } from "@/lib/utils"

type NavItem = { to: string; label: string }
type NavGroup = { label: string; items: NavItem[] }

const UNIT_LABEL = { bps: "b/s", pps: "p/s", bytes: "bytes" } as const

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

  const features = useFeatureFlags()
  const { data: userData } = useCurrentUser(features.auth)
  const user = userData?.data

  // With AUTH_ENABLED=false the server mounts no auth middleware and no role can
  // ever resolve, yet every endpoint is open — so admin-only nav must still show.
  const isAdmin = !features.auth || user?.role === "admin"

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
        { to: "/flows", label: "Flow search" },
        { to: "/conversations", label: "Conversations" },
      ],
    })
  }

  const security: NavItem[] = []
  if (features.alerts) {
    security.push({ to: "/live", label: "Live threats" })
    security.push({ to: "/alerts", label: "Alerts" })
  }
  if (features.bgp) {
    security.push({ to: "/bgp", label: "BGP blocks" })
  }
  if (security.length) groups.push({ label: "Security", items: security })

  const system: NavItem[] = []
  if (isAdmin) system.push({ to: "/status", label: "Status" })
  // The admin console is reachable by alert-feature viewers too (writes are
  // still server-gated), mirroring the previous shield-icon visibility.
  if (isAdmin || features.alerts) system.push({ to: "/admin", label: "Admin" })
  if (system.length) groups.push({ label: "System", items: system })

  const { data: alertsSummary } = useAlertsSummary(features.alerts)
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

  const toolButton =
    "inline-flex h-8 min-w-8 items-center justify-center gap-1.5 rounded border border-input px-2 text-[13px] text-foreground-secondary hover:text-foreground hover:bg-accent"

  return (
    <header className="sticky top-0 z-50 border-b border-border bg-card">
      <div className="mx-auto flex h-14 max-w-[1400px] items-center gap-4 px-4 sm:px-6 lg:px-8">
        <Link to="/" className="flex shrink-0 items-baseline gap-2">
          <span className="text-lg font-bold tracking-tight" style={{ fontStretch: "125%" }}>
            AS-Stats
          </span>
          {features.local_as ? (
            <span className="hidden text-[13px] text-muted-foreground sm:inline">AS{features.local_as}</span>
          ) : null}
        </Link>
        <HealthDot />

        {/* Desktop nav: a Dashboard link plus grouped dropdown menus. No overflow
            container here — a scroll ancestor would clip the dropdown panels. */}
        <nav ref={navRef} className="ml-2 hidden h-full items-stretch lg:flex" aria-label="Main navigation">
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
          <form onSubmit={handleSearch} className="relative hidden sm:block" role="search">
            <Search className="absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" aria-hidden />
            <input
              type="search"
              placeholder="AS number, IP or prefix"
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              aria-label="Search AS numbers, IP addresses or prefixes"
              className="h-8 w-44 rounded border border-input bg-background pl-8 pr-3 text-[13px] placeholder:text-muted-foreground outline-none focus-visible:border-ring xl:w-60"
            />
          </form>

          {features.alerts && (
            <Link
              to="/alerts"
              className={cn(
                toolButton,
                criticalAlerts > 0 && "border-destructive text-destructive hover:text-destructive",
              )}
              aria-label={`Alerts: ${activeAlerts} active${criticalAlerts > 0 ? `, ${criticalAlerts} critical` : ""}`}
              title={`${activeAlerts} active alert${activeAlerts !== 1 ? "s" : ""}${criticalAlerts > 0 ? ` (${criticalAlerts} critical)` : ""}`}
            >
              <Bell className="size-3.5" aria-hidden />
              {activeAlerts > 0 && (
                <span
                  className={cn(
                    "rounded-full px-1.5 text-xs font-semibold leading-[18px]",
                    criticalAlerts > 0 ? "bg-destructive text-destructive-foreground" : "bg-primary text-primary-foreground",
                  )}
                >
                  {activeAlerts > 99 ? "99+" : activeAlerts}
                </span>
              )}
            </Link>
          )}

          <button
            type="button"
            onClick={toggleUnit}
            className={toolButton}
            aria-label={`Traffic unit: ${UNIT_LABEL[unit]}. Click to switch.`}
            title="Switch between bits per second, packets per second and bytes"
          >
            {UNIT_LABEL[unit]}
          </button>

          <button
            type="button"
            onClick={toggleDensity}
            className={cn(toolButton, "hidden sm:inline-flex")}
            aria-label={`Table density: ${density}. Click to switch.`}
            aria-pressed={density === "compact"}
            title={`Table density: ${density}`}
          >
            {density === "compact" ? <Rows2 className="size-3.5" aria-hidden /> : <Rows3 className="size-3.5" aria-hidden />}
          </button>

          <button
            type="button"
            onClick={cycleTheme}
            className={toolButton}
            aria-label={`Colour theme: ${theme}. Click to switch.`}
            title={`Theme: ${theme}`}
          >
            <ThemeIcon className="size-3.5" aria-hidden />
          </button>

          {user && (
            <button
              type="button"
              onClick={() => {
                void api.logout().then(() => {
                  window.location.href = "/auth/login"
                })
              }}
              className={toolButton}
              title={`Signed in as ${user.name || user.email}. Click to sign out.`}
              aria-label="Sign out"
            >
              <LogOut className="size-3.5" aria-hidden />
              <span className="hidden xl:inline">{user.name || user.email}</span>
            </button>
          )}

          <button
            type="button"
            onClick={() => setMobileMenuOpen(!mobileMenuOpen)}
            className={cn(toolButton, "lg:hidden")}
            aria-label="Navigation menu"
            aria-controls="mobile-nav"
            aria-expanded={mobileMenuOpen}
          >
            {mobileMenuOpen ? <X className="size-4" aria-hidden /> : <Menu className="size-4" aria-hidden />}
          </button>
        </div>
      </div>

      {mobileMenuOpen && (
        <nav id="mobile-nav" className="border-t border-border bg-card px-4 py-3 lg:hidden" aria-label="Mobile navigation">
          <form onSubmit={handleSearch} className="relative mb-3 sm:hidden" role="search">
            <Search className="absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" aria-hidden />
            <input
              type="search"
              placeholder="AS number, IP or prefix"
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              aria-label="Search AS numbers, IP addresses or prefixes"
              className="h-9 w-full rounded border border-input bg-background pl-8 pr-3 text-sm outline-none focus-visible:border-ring"
            />
          </form>
          <div className="grid grid-cols-2 gap-x-6 gap-y-4 sm:grid-cols-3">
            <div>
              <MobileLink to="/" label="Dashboard" pathname={location.pathname} onNavigate={() => setMobileMenuOpen(false)} />
            </div>
            {groups.map(group => (
              <div key={group.label}>
                <div className="pb-1 text-[13px] text-muted-foreground">{group.label}</div>
                <div className="flex flex-col">
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

// HealthDot: green only when every router is reporting, amber when one is
// late, red when one is silent or none report, grey while unknown. It never
// animates — a pulsing "healthy" dot trains people to ignore it.
function HealthDot() {
  const { data, dataUpdatedAt, isError } = useLiveStatus()
  const level = healthLevel(data, isError)
  const label = {
    ok: "All routers reporting",
    warn: "A router is late",
    bad: "Flow collection problem",
    unknown: "Collection status unknown",
  }[level]
  const detail = data ? describeRouters(data, dataUpdatedAt) : isError ? "Status endpoint unavailable" : "Loading"
  return (
    <Link
      to="/status"
      className="inline-flex size-6 shrink-0 items-center justify-center rounded"
      title={`${label}\n${detail}`}
      aria-label={`${label}. ${detail.replaceAll("\n", "; ")}`}
    >
      <span
        className={cn(
          "size-2 rounded-full",
          level === "ok" && "bg-success",
          level === "warn" && "bg-warning-mark",
          level === "bad" && "bg-destructive",
          level === "unknown" && "bg-muted-foreground/50",
        )}
      />
    </Link>
  )
}

function healthLevel(data: LiveStatus | undefined, isError: boolean): "ok" | "warn" | "bad" | "unknown" {
  if (!data) return isError ? "bad" : "unknown"
  if (data.healthy) return "ok"
  if (!data.routers.length || data.routers.some(r => r.state === "missing")) return "bad"
  return "warn"
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
  const panelId = `nav-${group.label.toLowerCase()}`
  // Disclosure pattern (button + list of links), not an ARIA menu: role="menu"
  // promises arrow-key navigation that plain links do not have. Tabbing out of
  // the group closes it; Escape closes it and puts focus back on the trigger.
  return (
    <div
      className="relative flex"
      onMouseEnter={onHover}
      onBlur={e => {
        if (open && !e.currentTarget.contains(e.relatedTarget as Node | null)) onClose()
      }}
      onKeyDown={e => {
        if (e.key === "Escape" && open) {
          onClose()
          e.currentTarget.querySelector("button")?.focus()
        }
      }}
    >
      <button
        type="button"
        onClick={onToggle}
        aria-controls={panelId}
        aria-expanded={open}
        className={cn(
          "inline-flex items-center gap-1 border-b-2 px-2.5 pt-0.5 text-sm",
          active
            ? "border-foreground font-semibold text-foreground"
            : "border-transparent text-foreground-secondary hover:text-foreground",
        )}
      >
        {group.label}
        <ChevronDown className={cn("size-3.5 opacity-60", open && "rotate-180")} aria-hidden />
      </button>
      {open && (
        <div
          id={panelId}
          className="absolute left-0 top-full z-50 min-w-[190px] rounded-b border border-t-0 border-border bg-popover py-1 shadow-md"
        >
          {group.items.map(item => (
            <Link
              key={item.to}
              to={item.to}
              onClick={onClose}
              className={cn(
                "block px-3.5 py-1.5 text-sm",
                isActive(pathname, item.to)
                  ? "font-semibold text-foreground"
                  : "text-foreground-secondary hover:bg-accent hover:text-foreground",
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
        "py-1.5 text-sm",
        isActive(pathname, to) ? "font-semibold text-foreground" : "text-foreground-secondary hover:text-foreground",
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
        "inline-flex items-center border-b-2 px-2.5 pt-0.5 text-sm",
        active
          ? "border-foreground font-semibold text-foreground"
          : "border-transparent text-foreground-secondary hover:text-foreground",
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
