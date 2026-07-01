import { Link } from "react-router-dom"
import { useQuery } from "@tanstack/react-query"
import { api } from "@/lib/api"
import { useStatus, useStorageStatus } from "@/hooks/useApi"
import { useFeatureFlags } from "@/hooks/useFeatures"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { EmptyState } from "@/components/ui/error"
import { formatBytes, formatNumber } from "@/lib/utils"
import { Activity, ShieldAlert } from "lucide-react"
import { cn } from "@/lib/utils"
import type { DiskStats } from "@/lib/types"

// diskBarClass colors the disk-usage bar: green < 80, amber >= 80, red >= 90 —
// matching the disk_usage alert-rule defaults.
function diskBarClass(pct: number): string {
  if (pct >= 90) return "bg-destructive"
  if (pct >= 80) return "bg-warning"
  return "bg-success"
}

function fmtTime(s?: string): string {
  if (!s) return "—"
  const d = new Date(s)
  return isNaN(d.getTime()) ? s : d.toLocaleString()
}

// Status is an admin-only health overview aggregating the system (routers /
// flows), storage & retention (disks, table sizes, TTL lag), enabled features,
// and the active-alert summary. It reuses the existing /status, /admin/storage,
// /features and /alerts/summary endpoints — no new backend.
export function Status() {
  const { data: meRes } = useQuery({
    queryKey: ["auth-me"],
    queryFn: () => api.me(),
    staleTime: 300_000,
    retry: false,
  })
  const user = meRes?.data
  const features = useFeatureFlags()

  const { data: statusRes } = useStatus()
  const { data: storageRes } = useStorageStatus()
  const { data: alertsRes } = useQuery({
    queryKey: ["alerts-summary"],
    queryFn: () => api.alertsSummary(),
    refetchInterval: 30_000,
    enabled: features.alerts,
  })

  // Admin gate (server also enforces it on /admin/storage).
  if (user && user.role !== "admin") {
    return (
      <EmptyState
        icon={<ShieldAlert className="h-8 w-8" />}
        message="Admin only"
        hint="The system status page requires an administrator role."
      />
    )
  }

  const status = statusRes?.data
  const storage = storageRes?.data
  const routers = status?.routers ?? []
  const disks: DiskStats[] = storage?.disks ?? []
  const tables = storage?.tables ?? []
  const totalCompressed = tables.reduce((a, t) => a + (t.compressed_bytes || 0), 0)
  const ttlLagTables = tables.filter((t) => t.pending_mutations > 0).length
  const healthy = routers.length > 0
  const alerts = alertsRes?.data

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-3">
        <h1 className="text-2xl font-semibold">System Status</h1>
        <span
          className={cn(
            "inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium",
            healthy ? "bg-success/15 text-success" : "bg-destructive/15 text-destructive"
          )}
        >
          <Activity className="h-3.5 w-3.5" />
          {healthy ? "Operational" : "No flows"}
        </span>
      </div>

      {/* System */}
      <Card>
        <CardHeader>
          <CardTitle>Ingestion</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
            <Stat label="Active routers" value={formatNumber(routers.length)} />
            <Stat label="Flow rows (raw)" value={formatNumber(status?.total_rows ?? 0)} />
            <Stat label="DB size" value={formatBytes(status?.db_size ?? 0)} />
            <Stat label="Compressed (aggregates)" value={formatBytes(totalCompressed)} />
          </div>
          <div className="mt-4 overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border/50 text-left text-muted-foreground">
                  <th className="py-2 pr-4 font-medium">Router</th>
                  <th className="py-2 pr-4 font-medium">Last seen</th>
                  <th className="py-2 pr-4 text-right font-medium">Flows</th>
                </tr>
              </thead>
              <tbody>
                {routers.length === 0 && (
                  <tr>
                    <td colSpan={3} className="py-3 text-muted-foreground">
                      No routers have sent flows recently.
                    </td>
                  </tr>
                )}
                {routers.map((r) => (
                  <tr key={r.router_ip} className="border-b border-border/40">
                    <td className="py-2 pr-4 font-mono">{r.router_ip}</td>
                    <td className="py-2 pr-4 text-muted-foreground">{fmtTime(r.last_seen)}</td>
                    <td className="py-2 pr-4 text-right font-mono tabular-nums">{formatNumber(r.flow_count)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </CardContent>
      </Card>

      {/* Storage & retention */}
      <Card>
        <CardHeader>
          <CardTitle>Storage &amp; retention</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          {disks.length === 0 && (
            <p className="text-sm text-muted-foreground">Storage stats unavailable.</p>
          )}
          {disks.map((d) => (
            <div key={d.name}>
              <div className="mb-1 flex items-center justify-between text-sm">
                <span className="font-mono">{d.name}</span>
                <span className="tabular-nums text-muted-foreground">
                  {formatBytes(d.used_bytes)} / {formatBytes(d.total_bytes)} ({d.used_percent.toFixed(1)}%)
                </span>
              </div>
              <div className="h-2 w-full overflow-hidden rounded-full bg-muted">
                <div
                  className={cn("h-full rounded-full", diskBarClass(d.used_percent))}
                  style={{ width: `${Math.min(d.used_percent, 100)}%` }}
                />
              </div>
            </div>
          ))}
          <div className="flex flex-wrap items-center gap-x-6 gap-y-2 pt-2 text-sm">
            <span className="text-muted-foreground">
              Tables tracked: <span className="font-mono tabular-nums text-foreground">{tables.length}</span>
            </span>
            <span className="text-muted-foreground">
              TTL lag:{" "}
              <span className={cn("font-mono tabular-nums", ttlLagTables > 0 ? "text-warning" : "text-foreground")}>
                {ttlLagTables} table{ttlLagTables === 1 ? "" : "s"} with pending mutations
              </span>
            </span>
            <Link to="/admin" className="text-primary hover:underline">
              Manage retention →
            </Link>
          </div>
        </CardContent>
      </Card>

      <div className="grid gap-6 md:grid-cols-2">
        {/* Features */}
        <Card>
          <CardHeader>
            <CardTitle>Features</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex flex-wrap gap-2">
              <FeatureBadge label="Flow Search" on={features.flow_search} />
              <FeatureBadge label="Port Stats" on={features.port_stats} />
              <FeatureBadge label="Alerts" on={features.alerts} />
              <FeatureBadge label="BGP" on={features.bgp} />
              <FeatureBadge label="Reports" on={features.reports} />
            </div>
            {typeof features.local_as === "number" && features.local_as > 0 && (
              <p className="mt-3 text-sm text-muted-foreground">
                Local AS: <span className="font-mono text-foreground">AS{features.local_as}</span>
              </p>
            )}
          </CardContent>
        </Card>

        {/* Alerts */}
        <Card>
          <CardHeader>
            <CardTitle>Active alerts</CardTitle>
          </CardHeader>
          <CardContent>
            {!features.alerts ? (
              <p className="text-sm text-muted-foreground">Alerts feature disabled.</p>
            ) : (
              <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
                <Stat label="Total" value={formatNumber(alerts?.total ?? 0)} />
                <Stat label="Critical" value={formatNumber(alerts?.by_severity?.critical ?? 0)} tone="destructive" />
                <Stat label="Warning" value={formatNumber(alerts?.by_severity?.warning ?? 0)} tone="warning" />
                <Stat label="Info" value={formatNumber(alerts?.by_severity?.info ?? 0)} />
              </div>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  )
}

function Stat({ label, value, tone }: { label: string; value: string; tone?: "destructive" | "warning" }) {
  return (
    <div>
      <div className="text-xs text-muted-foreground">{label}</div>
      <div
        className={cn(
          "mt-0.5 text-xl font-semibold tabular-nums",
          tone === "destructive" && "text-destructive",
          tone === "warning" && "text-warning"
        )}
      >
        {value}
      </div>
    </div>
  )
}

function FeatureBadge({ label, on }: { label: string; on: boolean }) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-xs font-medium",
        on ? "bg-success/15 text-success" : "bg-muted text-muted-foreground"
      )}
    >
      <span className={cn("h-1.5 w-1.5 rounded-full", on ? "bg-success" : "bg-muted-foreground/50")} />
      {label}
    </span>
  )
}
