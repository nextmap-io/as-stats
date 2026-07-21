import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { useState, type ReactNode } from "react"
import { Link } from "react-router-dom"
import { api } from "@/lib/api"
import { useAnomalyExplain } from "@/hooks/useApi"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { ErrorDisplay, EmptyState } from "@/components/ui/error"
import { TableSkeleton } from "@/components/ui/skeleton"
import { PercentBar } from "@/components/DataTable"
import { Bell, Check, X, AlertTriangle, ShieldAlert, Info, ChevronRight, ChevronDown, Sparkles, Loader2 } from "lucide-react"
import { cn, formatBytes } from "@/lib/utils"
import type {
  Alert,
  AlertDetailsExtra,
  AlertDetailsPayload,
  AlertSeverity,
  AlertStatus,
  AnomalyExplanation,
} from "@/lib/types"

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
          <Bell className="size-4" />
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
            <Check className="size-8 mx-auto mb-2 text-success" />
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
              <table className="w-full text-xs">
                <thead>
                  <tr className="border-b border-border">
                    <th scope="col" className="pb-1.5 text-left font-medium text-muted-foreground">Severity</th>
                    <th scope="col" className="pb-1.5 text-left font-medium text-muted-foreground">Rule</th>
                    <th scope="col" className="pb-1.5 text-left font-medium text-muted-foreground">Target</th>
                    <th scope="col" className="pb-1.5 text-right font-medium text-muted-foreground hidden sm:table-cell">Value</th>
                    <th scope="col" className="pb-1.5 pr-3 text-right font-medium text-muted-foreground hidden sm:table-cell">Threshold</th>
                    <th scope="col" className="pb-1.5 pl-3 text-left font-medium text-muted-foreground hidden md:table-cell">Triggered</th>
                    <th scope="col" className="pb-1.5 text-right font-medium text-muted-foreground">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {alerts.map((a) => (
                    <AlertRow
                      key={a.id}
                      alert={a}
                      onAck={() => ackMutation.mutate(a.id)}
                      onResolve={() => resolveMutation.mutate(a.id)}
                      onBlock={() => {
                        if (confirm(`BGP blackhole ${a.target_ip} for 60 minutes?`)) {
                          blockMutation.mutate({ id: a.id, duration: 60, reason: a.rule_name })
                        }
                      }}
                      ackPending={ackMutation.isPending}
                      resolvePending={resolveMutation.isPending}
                      blockPending={blockMutation.isPending}
                    />
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
      <CardHeader className="pb-1">
        <div className="flex items-center justify-between">
          <CardTitle className={config.color}>{severity.toUpperCase()}</CardTitle>
          <Icon className={cn("size-4", config.color)} />
        </div>
      </CardHeader>
      <CardContent>
        <p className={cn("text-2xl font-bold tabular-nums leading-none", config.color)}>{count}</p>
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

interface AlertRowProps {
  alert: Alert
  onAck: () => void
  onResolve: () => void
  onBlock: () => void
  ackPending: boolean
  resolvePending: boolean
  blockPending: boolean
}

const ALERT_COLSPAN = 7

function AlertRow({ alert: a, onAck, onResolve, onBlock, ackPending, resolvePending, blockPending }: AlertRowProps) {
  const [expanded, setExpanded] = useState(false)
  const [explainOpen, setExplainOpen] = useState(false)

  const details = parseAlertDetails(a.details)
  const extra = details?.extra
  // Anomaly alerts are link-scoped (no IP) and carry baseline stats / a stored
  // contributor explanation in details.extra. Detect them from that payload
  // since the Alert record itself doesn't carry the rule type.
  const isAnomaly = !!extra && (extra.explanation !== undefined || extra.baseline !== undefined)
  // For anomaly alerts target_ip actually holds the link tag; extra.target is
  // the authoritative source.
  const target = (extra?.target || a.target_ip || "").trim()
  const topSources = details?.top_sources ?? []
  const hasDetails = isAnomaly || topSources.length > 0

  // On-demand live explain: window is the alert's last complete hour.
  const explainTo = a.last_seen_at
  const explainFrom = new Date(new Date(a.last_seen_at).getTime() - 3600_000).toISOString()
  const explainQuery = useAnomalyExplain(explainOpen && target ? target : "", {
    from: explainFrom,
    to: explainTo,
  })
  const liveExpl = explainQuery.data?.data

  return (
    <>
      <tr className="border-b border-border/40 last:border-0 hover:bg-accent/50 transition-colors">
        <td className="py-1.5">
          <SeverityBadge severity={a.severity} />
        </td>
        <td className="py-1.5 font-medium">{a.rule_name}</td>
        <td className="py-1.5">
          {isAnomaly && target ? (
            <Link to={`/link/${target}`} className="text-primary hover:underline font-mono text-[11px]">
              {target}
            </Link>
          ) : (
            <Link to={`/ip/${a.target_ip}`} className="text-primary hover:underline font-mono text-[11px]">
              {a.target_ip}
            </Link>
          )}
        </td>
        <td className="py-1.5 text-right font-mono hidden sm:table-cell">
          {formatMetric(a.metric_type, a.metric_value)}
        </td>
        <td className="py-1.5 pr-3 text-right font-mono text-muted-foreground hidden sm:table-cell">
          {formatMetric(a.metric_type, a.threshold)}
        </td>
        <td className="py-1.5 pl-3 text-muted-foreground text-[10px] hidden md:table-cell">
          {new Date(a.triggered_at).toLocaleString()}
        </td>
        <td className="py-1.5 text-right">
          <div className="flex justify-end gap-1 items-center">
            {hasDetails && (
              <button
                onClick={() => setExpanded((s) => !s)}
                className="px-1 py-0.5 text-[10px] rounded border border-input bg-muted/50 hover:bg-accent transition-colors inline-flex items-center gap-0.5"
                title={expanded ? "Hide details" : "Show details"}
                aria-expanded={expanded}
              >
                {expanded ? <ChevronDown className="size-2.5" /> : <ChevronRight className="size-2.5" />}
                Details
              </button>
            )}
            {a.status === "active" && (
              <>
                <button
                  onClick={onAck}
                  disabled={ackPending}
                  className="px-1.5 py-0.5 text-[10px] rounded border border-input bg-muted/50 hover:bg-accent transition-colors"
                  title="Acknowledge"
                >
                  Ack
                </button>
                <button
                  onClick={onBlock}
                  disabled={blockPending}
                  className="px-1.5 py-0.5 text-[10px] rounded border border-destructive/40 text-destructive bg-destructive/10 hover:bg-destructive/20 transition-colors"
                  title="BGP blackhole (admin only)"
                >
                  Block
                </button>
              </>
            )}
            {a.status !== "resolved" && (
              <button
                onClick={onResolve}
                disabled={resolvePending}
                className="px-1.5 py-0.5 text-[10px] rounded border border-input bg-muted/50 hover:bg-accent transition-colors"
                title="Resolve"
              >
                <X className="size-2.5" />
              </button>
            )}
          </div>
        </td>
      </tr>

      {expanded && hasDetails && (
        <tr className="bg-muted/20">
          <td colSpan={ALERT_COLSPAN} className="px-3 py-3">
            <div className="space-y-3">
              {isAnomaly && extra && <AnomalyStats extra={extra} />}

              {isAnomaly && extra?.explanation && (
                <div className="space-y-1">
                  <div className="text-[10px] uppercase tracking-widest text-muted-foreground">
                    Contributors during the flagged hour (stored)
                  </div>
                  <ExplanationGrid expl={extra.explanation} />
                </div>
              )}

              {topSources.length > 0 && (
                <div className="space-y-1">
                  <div className="text-[10px] uppercase tracking-widest text-muted-foreground">
                    Top sources
                  </div>
                  <div className="flex flex-wrap gap-1.5">
                    {topSources.map((ip) => (
                      <Link
                        key={ip}
                        to={`/ip/${ip}`}
                        className="px-1.5 py-0.5 rounded border border-border bg-background font-mono text-[10px] text-primary hover:bg-accent"
                      >
                        {ip}
                      </Link>
                    ))}
                  </div>
                </div>
              )}

              {isAnomaly && target && (
                <div className="space-y-2">
                  <button
                    onClick={() => setExplainOpen((s) => !s)}
                    className="inline-flex items-center gap-1 px-2 py-0.5 text-[11px] font-medium rounded border border-primary/40 bg-primary/10 text-primary hover:bg-primary/20 transition-colors"
                  >
                    <Sparkles className="size-3" />
                    {explainOpen ? "Hide live explain" : "Explain (live)"}
                  </button>

                  {explainOpen && (
                    <div className="rounded border border-border bg-background/60 p-2">
                      {explainQuery.isLoading ? (
                        <div className="flex items-center gap-2 text-[11px] text-muted-foreground">
                          <Loader2 className="size-3 animate-spin" />
                          Analysing {target} over the last hour…
                        </div>
                      ) : explainQuery.isError ? (
                        <p className="text-[11px] text-destructive">
                          {(explainQuery.error as Error)?.message || "Failed to load explanation"}
                        </p>
                      ) : liveExpl && hasContributors(liveExpl) ? (
                        <ExplanationGrid expl={liveExpl} />
                      ) : (
                        <p className="text-[11px] text-muted-foreground">
                          No contributor data for this window.
                        </p>
                      )}
                    </div>
                  )}
                </div>
              )}
            </div>
          </td>
        </tr>
      )}
    </>
  )
}

function AnomalyStats({ extra }: { extra: AlertDetailsExtra }) {
  const stats: { label: string; value: string }[] = []
  if (extra.current !== undefined) stats.push({ label: "Current", value: formatMetric("bps", extra.current) })
  if (extra.baseline !== undefined) stats.push({ label: "Baseline", value: formatMetric("bps", extra.baseline) })
  if (extra.deviation !== undefined) stats.push({ label: "Deviation", value: `${extra.deviation.toFixed(2)}×` })
  if (extra.sensitivity_k !== undefined) stats.push({ label: "Sensitivity", value: `k=${extra.sensitivity_k.toFixed(1)}` })
  if (extra.samples_count !== undefined) stats.push({ label: "Samples", value: String(extra.samples_count) })
  if (stats.length === 0) return null
  return (
    <div className="flex flex-wrap gap-x-6 gap-y-1">
      {stats.map((s) => (
        <div key={s.label} className="flex flex-col">
          <span className="text-[9px] uppercase tracking-widest text-muted-foreground">{s.label}</span>
          <span className="text-[11px] font-mono tabular-nums">{s.value}</span>
        </div>
      ))}
    </div>
  )
}

interface ContribRow {
  label: ReactNode
  bytes: number
  pct: number
}

function ContribTable({ title, rows }: { title: string; rows: ContribRow[] }) {
  if (rows.length === 0) {
    return (
      <div className="min-w-0">
        <div className="text-[9px] uppercase tracking-widest text-muted-foreground mb-1">{title}</div>
        <p className="text-[10px] text-muted-foreground">—</p>
      </div>
    )
  }
  return (
    <div className="min-w-0">
      <div className="text-[9px] uppercase tracking-widest text-muted-foreground mb-1">{title}</div>
      <table className="w-full text-[11px]">
        <tbody>
          {rows.map((r, i) => (
            <tr key={i} className="border-b border-border/30 last:border-0">
              <td className="py-0.5 pr-2 truncate max-w-[10rem]">{r.label}</td>
              <td className="py-0.5 pr-2 text-right font-mono tabular-nums text-muted-foreground whitespace-nowrap">
                {formatBytes(r.bytes)}
              </td>
              <td className="py-0.5 w-24">
                <PercentBar pct={r.pct} />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function ExplanationGrid({ expl }: { expl: AnomalyExplanation }) {
  return (
    <div className="grid gap-4 sm:grid-cols-3">
      <ContribTable
        title="Top source ASes"
        rows={expl.top_ases.map((a) => ({
          label: (
            <Link to={`/as/${a.as_number}`} className="text-primary hover:underline">
              AS{a.as_number}
              {a.as_name ? <span className="text-muted-foreground"> · {a.as_name}</span> : null}
            </Link>
          ),
          bytes: a.bytes,
          pct: a.pct,
        }))}
      />
      <ContribTable
        title="Top source IPs"
        rows={expl.top_sources.map((s) => ({
          label: (
            <Link to={`/ip/${s.ip}`} className="text-primary hover:underline font-mono">
              {s.ip}
            </Link>
          ),
          bytes: s.bytes,
          pct: s.pct,
        }))}
      />
      <ContribTable
        title="Top dst ports"
        rows={expl.top_ports.map((p) => ({
          label: (
            <span className="font-mono">
              {p.service || p.port}
              <span className="text-muted-foreground">/{p.protocol_name || p.protocol}</span>
            </span>
          ),
          bytes: p.bytes,
          pct: p.pct,
        }))}
      />
    </div>
  )
}

function hasContributors(e: AnomalyExplanation): boolean {
  return e.top_ases.length > 0 || e.top_sources.length > 0 || e.top_ports.length > 0
}

function parseAlertDetails(raw?: string): AlertDetailsPayload | null {
  if (!raw) return null
  try {
    return JSON.parse(raw) as AlertDetailsPayload
  } catch {
    return null
  }
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
