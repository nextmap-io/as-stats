import { useQuery } from "@tanstack/react-query"
import { useState } from "react"
import { Link } from "react-router-dom"
import { api } from "@/lib/api"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { ErrorDisplay, EmptyState } from "@/components/ui/error"
import { TableSkeleton } from "@/components/ui/skeleton"
import { IPWithPTR } from "@/components/PTR"
import { Activity, ShieldAlert, AlertTriangle, CheckCircle2 } from "lucide-react"
import { cn } from "@/lib/utils"
import type { LiveThreat, ThreatStatus } from "@/lib/types"

const WINDOWS: { value: number; label: string }[] = [
  { value: 60, label: "1m" },
  { value: 300, label: "5m" },
  { value: 900, label: "15m" },
  { value: 3600, label: "1h" },
]

export function LiveThreats() {
  const [windowSec, setWindowSec] = useState<number>(300)
  const [hideOk, setHideOk] = useState<boolean>(false)

  const { data, isLoading, error, refetch, isFetching } = useQuery({
    queryKey: ["live-threats", windowSec],
    queryFn: () => api.liveThreats(windowSec, 100),
    refetchInterval: 10_000,
  })

  if (error) return <ErrorDisplay error={error as Error} onRetry={() => refetch()} />

  const all: LiveThreat[] = data?.data || []
  const threats = hideOk ? all.filter((t) => t.status !== "ok") : all
  const counts = {
    critical: all.filter((t) => t.status === "critical").length,
    warn: all.filter((t) => t.status === "warn").length,
    ok: all.filter((t) => t.status === "ok").length,
  }

  return (
    <div className="space-y-4 animate-fade-in">
      <div className="flex items-baseline justify-between flex-wrap gap-2">
        <h1 className="text-lg font-semibold tracking-tight flex items-center gap-2">
          <Activity className="h-4 w-4" />
          Live Threats
          {isFetching && (
            <span className="h-1.5 w-1.5 rounded-full bg-primary animate-pulse" aria-label="refreshing" />
          )}
        </h1>
        <div className="flex items-center gap-3">
          <Link
            to="/alerts"
            className="text-[10px] text-muted-foreground hover:text-foreground uppercase tracking-widest"
          >
            View triggered alerts →
          </Link>
        </div>
      </div>

      <p className="text-xs text-muted-foreground">
        Real-time snapshot of the top inbound destinations from the alert engine's hot tables.
        Rows are evaluated against active rule thresholds — anything ≥50% of any threshold is flagged.
        Auto-refresh every 10s.
      </p>

      {/* Status summary cards */}
      <div className="grid gap-3 grid-cols-3">
        <StatusCard status="critical" count={counts.critical} />
        <StatusCard status="warn" count={counts.warn} />
        <StatusCard status="ok" count={counts.ok} />
      </div>

      {/* Window selector + filter */}
      <div className="flex items-center justify-between flex-wrap gap-2">
        <div className="flex gap-1 border border-border rounded p-0.5 bg-muted/30">
          {WINDOWS.map((w) => (
            <button
              key={w.value}
              onClick={() => setWindowSec(w.value)}
              className={cn(
                "px-2 py-1 text-[10px] font-medium rounded transition-colors",
                windowSec === w.value
                  ? "bg-primary text-primary-foreground"
                  : "text-muted-foreground hover:text-foreground"
              )}
            >
              {w.label}
            </button>
          ))}
        </div>
        <label className="flex items-center gap-1.5 text-[10px] text-muted-foreground cursor-pointer">
          <input
            type="checkbox"
            checked={hideOk}
            onChange={(e) => setHideOk(e.target.checked)}
            className="h-3 w-3 accent-primary"
          />
          Hide quiet rows
        </label>
      </div>

      <Card>
        <CardHeader className="pb-2">
          <CardTitle>
            Top destinations · last {WINDOWS.find((w) => w.value === windowSec)?.label}
          </CardTitle>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <TableSkeleton rows={10} cols={7} />
          ) : threats.length === 0 ? (
            <EmptyState
              message={
                hideOk
                  ? "No threats above the OK threshold"
                  : "No traffic recorded for the selected window"
              }
            />
          ) : (
            <div className="overflow-x-auto -mx-4 px-4 sm:-mx-5 sm:px-5">
              <table className="w-full text-xs" role="table">
                <thead>
                  <tr className="border-b border-border">
                    <th scope="col" className="pb-1.5 text-left font-medium text-muted-foreground">Status</th>
                    <th scope="col" className="pb-1.5 text-left font-medium text-muted-foreground">Destination</th>
                    <th scope="col" className="pb-1.5 text-right font-medium text-muted-foreground">bps</th>
                    <th scope="col" className="pb-1.5 text-right font-medium text-muted-foreground">pps</th>
                    <th scope="col" className="pb-1.5 text-right font-medium text-muted-foreground hidden sm:table-cell">SYN/s</th>
                    <th scope="col" className="pb-1.5 text-right font-medium text-muted-foreground hidden sm:table-cell">Unique src</th>
                    <th scope="col" className="pb-1.5 text-right font-medium text-muted-foreground">Worst rule</th>
                  </tr>
                </thead>
                <tbody>
                  {threats.map((t) => (
                    <tr
                      key={t.target_ip}
                      className={cn(
                        "border-b border-border/40 last:border-0 hover:bg-accent/50 transition-colors",
                        t.status === "critical" && "bg-destructive/5"
                      )}
                    >
                      <td className="py-1.5">
                        <StatusBadge status={t.status} pct={t.worst_pct} />
                      </td>
                      <td className="py-1.5">
                        <Link to={`/ip/${t.target_ip}`} className="text-primary hover:underline font-mono text-[11px]">
                          <IPWithPTR ip={t.target_ip} />
                        </Link>
                      </td>
                      <td className="py-1.5 text-right font-mono">{formatBps(t.bps)}</td>
                      <td className="py-1.5 text-right font-mono">{formatPps(t.pps)}</td>
                      <td className="py-1.5 text-right font-mono hidden sm:table-cell">
                        {t.syn_pps > 0 ? formatPps(t.syn_pps) : <span className="text-muted-foreground">—</span>}
                      </td>
                      <td className="py-1.5 text-right font-mono hidden sm:table-cell">
                        {t.unique_src_ips > 0 ? t.unique_src_ips.toLocaleString() : <span className="text-muted-foreground">—</span>}
                      </td>
                      <td className="py-1.5 text-right text-[10px] text-muted-foreground">
                        {t.worst_rule || <span className="opacity-50">—</span>}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

function StatusCard({ status, count }: { status: ThreatStatus; count: number }) {
  const config = {
    critical: {
      title: "CRITICAL",
      label: "≥ threshold",
      color: "text-destructive",
      bg: "bg-destructive/10 border-destructive/30",
      icon: ShieldAlert,
    },
    warn: {
      title: "WARN",
      label: "≥ 50% threshold",
      color: "text-warning",
      bg: "bg-warning/10 border-warning/30",
      icon: AlertTriangle,
    },
    ok: {
      title: "OK",
      label: "below 50%",
      color: "text-success",
      bg: "bg-success/10 border-success/30",
      icon: CheckCircle2,
    },
  }[status]
  const Icon = config.icon

  // Use the standard CardHeader + CardContent pattern (same as TopProtocols)
  // so the title baseline lines up across cards on the same row. The previous
  // implementation packed everything into a single CardContent with custom
  // padding which produced inconsistent vertical positioning.
  return (
    <Card className={cn(config.bg, count > 0 && status === "critical" && "animate-pulse")}>
      <CardHeader className="pb-1">
        <div className="flex items-center justify-between">
          <CardTitle className={config.color}>{config.title}</CardTitle>
          <Icon className={cn("h-4 w-4", config.color)} />
        </div>
      </CardHeader>
      <CardContent>
        <p className={cn("text-2xl font-bold tabular-nums leading-none", config.color)}>{count}</p>
        <p className="text-[9px] text-muted-foreground mt-1.5">{config.label}</p>
      </CardContent>
    </Card>
  )
}

function StatusBadge({ status, pct }: { status: ThreatStatus; pct: number }) {
  const styles = {
    critical: "bg-destructive/20 text-destructive border-destructive/40",
    warn: "bg-warning/20 text-warning border-warning/40",
    ok: "bg-success/20 text-success border-success/40",
  }[status]
  const label = status === "ok" ? "OK" : `${Math.round(pct)}%`
  return (
    <span
      className={cn(
        "px-1.5 py-0.5 text-[9px] font-medium rounded border uppercase tracking-wide tabular-nums",
        styles
      )}
    >
      {label}
    </span>
  )
}

function formatBps(value: number): string {
  const units = ["bps", "Kbps", "Mbps", "Gbps", "Tbps"]
  let v = value
  let i = 0
  while (v >= 1000 && i < units.length - 1) {
    v /= 1000
    i++
  }
  return `${v.toFixed(v >= 100 ? 0 : v >= 10 ? 1 : 2)} ${units[i]}`
}

function formatPps(value: number): string {
  const units = ["pps", "Kpps", "Mpps", "Gpps"]
  let v = value
  let i = 0
  while (v >= 1000 && i < units.length - 1) {
    v /= 1000
    i++
  }
  return `${v.toFixed(v >= 100 ? 0 : v >= 10 ? 1 : 2)} ${units[i]}`
}
