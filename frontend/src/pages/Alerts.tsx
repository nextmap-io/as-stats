import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { useState } from "react"
import { Link } from "react-router-dom"
import { api } from "@/lib/api"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { ErrorDisplay, EmptyState } from "@/components/ui/error"
import { TableSkeleton } from "@/components/ui/skeleton"
import { Bell, Check, X, AlertTriangle, ShieldAlert, Info } from "lucide-react"
import { cn } from "@/lib/utils"
import type { Alert, AlertSeverity, AlertStatus } from "@/lib/types"

const STATUS_TABS: { value: AlertStatus | ""; label: string }[] = [
  { value: "active", label: "Active" },
  { value: "acknowledged", label: "Acknowledged" },
  { value: "resolved", label: "Resolved" },
  { value: "", label: "All" },
]

export function Alerts() {
  const [status, setStatus] = useState<AlertStatus | "">("active")
  const queryClient = useQueryClient()

  const { data: summary } = useQuery({
    queryKey: ["alerts-summary"],
    queryFn: () => api.alertsSummary(),
    refetchInterval: 15_000,
  })

  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ["alerts", status],
    queryFn: () => api.alerts(status || undefined),
    refetchInterval: 30_000,
  })

  const ackMutation = useMutation({
    mutationFn: (id: string) => api.ackAlert(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["alerts"] })
      queryClient.invalidateQueries({ queryKey: ["alerts-summary"] })
    },
  })

  const resolveMutation = useMutation({
    mutationFn: (id: string) => api.resolveAlert(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["alerts"] })
      queryClient.invalidateQueries({ queryKey: ["alerts-summary"] })
    },
  })

  const blockMutation = useMutation({
    mutationFn: ({ id, duration, reason }: { id: string; duration: number; reason: string }) =>
      api.blockAlert(id, duration, reason),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["alerts"] })
    },
  })

  if (error) return <ErrorDisplay error={error as Error} onRetry={() => refetch()} />

  const alerts: Alert[] = data?.data || []
  const totalActive = summary?.data?.total || 0
  const bySev = summary?.data?.by_severity || {}

  return (
    <div className="space-y-4 animate-fade-in">
      <div className="flex items-baseline justify-between">
        <h1 className="text-lg font-semibold tracking-tight flex items-center gap-2">
          <Bell className="h-4 w-4" />
          Alerts
        </h1>
        <Link
          to="/admin?tab=rules"
          className="text-[10px] text-muted-foreground hover:text-foreground uppercase tracking-widest"
        >
          Manage rules →
        </Link>
      </div>

      {/* Severity summary cards */}
      <div className="grid gap-3 grid-cols-3">
        <SeverityCard severity="critical" count={bySev.critical || 0} />
        <SeverityCard severity="warning" count={bySev.warning || 0} />
        <SeverityCard severity="info" count={bySev.info || 0} />
      </div>

      {totalActive === 0 && status === "active" && (
        <Card>
          <CardContent className="p-6 text-center">
            <Check className="h-8 w-8 mx-auto mb-2 text-success" />
            <p className="text-sm font-medium text-foreground">All clear</p>
            <p className="text-xs text-muted-foreground mt-1">No active alerts</p>
          </CardContent>
        </Card>
      )}

      {/* Status filter tabs */}
      <div className="flex gap-1 border-b border-border">
        {STATUS_TABS.map((tab) => (
          <button
            key={tab.value}
            onClick={() => setStatus(tab.value)}
            className={cn(
              "px-3 py-1.5 text-xs font-medium border-b-2 -mb-px transition-colors",
              status === tab.value
                ? "border-primary text-primary"
                : "border-transparent text-muted-foreground hover:text-foreground"
            )}
          >
            {tab.label}
          </button>
        ))}
      </div>

      <Card>
        <CardHeader className="pb-2">
          <CardTitle>{status === "active" ? "Active alerts" : status === "" ? "All alerts" : `${status} alerts`}</CardTitle>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <TableSkeleton rows={5} cols={6} />
          ) : alerts.length === 0 ? (
            <EmptyState message="No alerts match this filter" />
          ) : (
            <div className="overflow-x-auto -mx-4 px-4 sm:-mx-5 sm:px-5">
              <table className="w-full text-xs" role="table">
                <thead>
                  <tr className="border-b border-border">
                    <th scope="col" className="pb-1.5 text-left font-medium text-muted-foreground">Severity</th>
                    <th scope="col" className="pb-1.5 text-left font-medium text-muted-foreground">Rule</th>
                    <th scope="col" className="pb-1.5 text-left font-medium text-muted-foreground">Target</th>
                    <th scope="col" className="pb-1.5 text-right font-medium text-muted-foreground hidden sm:table-cell">Value</th>
                    <th scope="col" className="pb-1.5 text-right font-medium text-muted-foreground hidden sm:table-cell">Threshold</th>
                    <th scope="col" className="pb-1.5 text-left font-medium text-muted-foreground hidden md:table-cell">Triggered</th>
                    <th scope="col" className="pb-1.5 text-right font-medium text-muted-foreground">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {alerts.map((a) => (
                    <tr key={a.id} className="border-b border-border/40 last:border-0 hover:bg-accent/50 transition-colors">
                      <td className="py-1.5">
                        <SeverityBadge severity={a.severity} />
                      </td>
                      <td className="py-1.5 font-medium">{a.rule_name}</td>
                      <td className="py-1.5">
                        <Link to={`/ip/${a.target_ip}`} className="text-primary hover:underline font-mono text-[11px]">
                          {a.target_ip}
                        </Link>
                      </td>
                      <td className="py-1.5 text-right font-mono hidden sm:table-cell">
                        {formatMetric(a.metric_type, a.metric_value)}
                      </td>
                      <td className="py-1.5 text-right font-mono text-muted-foreground hidden sm:table-cell">
                        {formatMetric(a.metric_type, a.threshold)}
                      </td>
                      <td className="py-1.5 text-muted-foreground text-[10px] hidden md:table-cell">
                        {new Date(a.triggered_at).toLocaleString()}
                      </td>
                      <td className="py-1.5 text-right">
                        <div className="flex justify-end gap-1">
                          {a.status === "active" && (
                            <>
                              <button
                                onClick={() => ackMutation.mutate(a.id)}
                                disabled={ackMutation.isPending}
                                className="px-1.5 py-0.5 text-[10px] rounded border border-input bg-muted/50 hover:bg-accent transition-colors"
                                title="Acknowledge"
                              >
                                Ack
                              </button>
                              <button
                                onClick={() => {
                                  if (confirm(`BGP blackhole ${a.target_ip} for 60 minutes?`)) {
                                    blockMutation.mutate({ id: a.id, duration: 60, reason: a.rule_name })
                                  }
                                }}
                                disabled={blockMutation.isPending}
                                className="px-1.5 py-0.5 text-[10px] rounded border border-destructive/40 text-destructive bg-destructive/10 hover:bg-destructive/20 transition-colors"
                                title="BGP blackhole (admin only)"
                              >
                                Block
                              </button>
                            </>
                          )}
                          {a.status !== "resolved" && (
                            <button
                              onClick={() => resolveMutation.mutate(a.id)}
                              disabled={resolveMutation.isPending}
                              className="px-1.5 py-0.5 text-[10px] rounded border border-input bg-muted/50 hover:bg-accent transition-colors"
                              title="Resolve"
                            >
                              <X className="h-2.5 w-2.5" />
                            </button>
                          )}
                        </div>
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

function SeverityCard({ severity, count }: { severity: AlertSeverity; count: number }) {
  const config = {
    critical: { color: "text-destructive", bg: "bg-destructive/10 border-destructive/30", icon: ShieldAlert },
    warning: { color: "text-warning", bg: "bg-warning/10 border-warning/30", icon: AlertTriangle },
    info: { color: "text-primary", bg: "bg-primary/10 border-primary/30", icon: Info },
  }[severity]
  const Icon = config.icon

  return (
    <Card className={cn(config.bg, count > 0 && severity === "critical" && "animate-pulse")}>
      <CardContent className="p-4">
        <div className="flex items-center justify-between mb-1">
          <p className="text-[10px] font-medium text-muted-foreground uppercase tracking-widest">{severity}</p>
          <Icon className={cn("h-4 w-4", config.color)} />
        </div>
        <p className={cn("text-2xl font-bold tabular-nums", config.color)}>{count}</p>
      </CardContent>
    </Card>
  )
}

function SeverityBadge({ severity }: { severity: AlertSeverity }) {
  const styles = {
    critical: "bg-destructive/20 text-destructive border-destructive/40",
    warning: "bg-warning/20 text-warning border-warning/40",
    info: "bg-primary/20 text-primary border-primary/40",
  }[severity]
  return (
    <span className={cn("px-1.5 py-0.5 text-[9px] font-medium rounded border uppercase tracking-wide", styles)}>
      {severity}
    </span>
  )
}

function formatMetric(type: string, value: number): string {
  if (type === "bps") {
    const units = ["bps", "Kbps", "Mbps", "Gbps", "Tbps"]
    let v = value
    let i = 0
    while (v >= 1000 && i < units.length - 1) {
      v /= 1000
      i++
    }
    return `${v.toFixed(2)} ${units[i]}`
  }
  if (type === "pps") {
    const units = ["pps", "Kpps", "Mpps", "Gpps"]
    let v = value
    let i = 0
    while (v >= 1000 && i < units.length - 1) {
      v /= 1000
      i++
    }
    return `${v.toFixed(2)} ${units[i]}`
  }
  return value.toLocaleString()
}
