import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { useState, type ReactNode } from "react"
import { Link } from "react-router-dom"
import { api, isApiError } from "@/lib/api"
import { useFeatureFlags } from "@/hooks/useFeatures"
import { hostTarget } from "@/lib/home"
import { formatAlertMetric } from "@/lib/format"
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

  const features = useFeatureFlags()
  // Known link tags, so a link-scoped alert links to its link page and a disk
  // alert (also label-targeted) does not.
  const { data: linkCfg } = useQuery({ queryKey: ["admin-links"], queryFn: () => api.adminLinks(), staleTime: 30_000 })
  const linkTags = new Set((linkCfg?.data ?? []).map((l) => l.tag))
  const blockMutation = useMutation({
    mutationFn: ({ id, duration, reason }: { id: string; duration: number; reason: string }) =>
      api.blockAlert(id, duration, reason),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["alerts"] })
    },
  })
  const blockError = blockMutation.error ? blockErrorMessage(blockMutation.error) : null

  if (error) return <ErrorDisplay error={error as Error} onRetry={() => refetch()} />

  const alerts: Alert[] = data?.data || []
  const totalActive = summary?.data?.total || 0
  const bySev = summary?.data?.by_severity || {}

  return (
    <div className="space-y-4">
      <div className="flex items-baseline justify-between">
        <h1 className="flex items-center gap-2 text-2xl font-semibold">
          <Bell className="size-5" aria-hidden />
          Alerts
        </h1>
        <Link to="/admin?tab=rules" className="text-[13px] text-foreground-secondary underline underline-offset-2 hover:text-foreground">
          Manage rules
        </Link>
      </div>

      {/* Severity summary cards */}
      <div className="grid grid-cols-3 gap-6">
        <SeverityCard severity="critical" count={bySev.critical || 0} />
        <SeverityCard severity="warning" count={bySev.warning || 0} />
        <SeverityCard severity="info" count={bySev.info || 0} />
      </div>

      {totalActive === 0 && status === "active" && (
        <p className="flex items-center gap-2 text-sm text-foreground-secondary">
          <Check className="size-4 text-success" aria-hidden />
          No active alerts.
        </p>
      )}

      {blockError && (
        <div role="alert" className="border-l-2 border-destructive py-1 pl-3 text-sm">
          <span className="font-semibold">Not blocked.</span> {blockError}
        </div>
      )}

      {/* Status filter tabs */}
      <div className="flex gap-1 border-b border-border">
        {STATUS_TABS.map((tab) => (
          <button
            key={tab.value}
            type="button"
            onClick={() => setStatus(tab.value)}
            aria-pressed={status === tab.value}
            className={cn(
              "-mb-px border-b-2 px-3 py-1.5 text-sm",
              status === tab.value
                ? "border-foreground font-semibold text-foreground"
                : "border-transparent text-foreground-secondary hover:text-foreground"
            )}
          >
            {tab.label}
          </button>
        ))}
      </div>

      <Card>
        <CardHeader>
          <CardTitle>{status === "active" ? "Active alerts" : status === "" ? "All alerts" : `${status} alerts`}</CardTitle>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <TableSkeleton rows={5} cols={6} />
          ) : alerts.length === 0 ? (
            <EmptyState message="No alerts match this filter" />
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-[13px]">
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
                      // Blackholing needs BGP enabled server-side AND a real host to
                      // announce. Link-scoped alerts carry a synthetic address.
                      canBlock={features.bgp && hostTarget(a) !== null}
                      linkTags={linkTags}
                      onBlock={() => {
                        const host = hostTarget(a)
                        if (host && confirm(`Blackhole ${host} through BGP for 60 minutes?`)) {
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

// Dark-cockpit counts: a severity lights up only when it has active alerts.
function SeverityCard({ severity, count }: { severity: AlertSeverity; count: number }) {
  const config = {
    critical: { label: "Critical", color: "text-destructive", icon: ShieldAlert },
    warning: { label: "Warning", color: "text-warning", icon: AlertTriangle },
    info: { label: "Info", color: "text-foreground", icon: Info },
  }[severity]
  const Icon = config.icon
  const lit = count > 0
  return (
    <div className="border-t border-foreground pt-3">
      <div className="flex items-center gap-1.5 text-[13px] text-muted-foreground">
        <Icon className={cn("size-3.5", lit && config.color)} aria-hidden />
        {config.label}
      </div>
      <p className={cn("mt-1 font-display text-4xl leading-none", lit ? config.color : "text-muted-foreground")}>{count}</p>
    </div>
  )
}

function blockErrorMessage(err: Error): string {
  if (isApiError(err, 409)) return "BGP blackholing is not enabled on this server."
  if (isApiError(err, 422)) return "This alert does not target a single IP address, so there is nothing to blackhole."
  if (isApiError(err, 403)) return "Only administrators can blackhole an address."
  return err.message
}

function SeverityBadge({ severity }: { severity: AlertSeverity }) {
  const styles = {
    critical: "bg-destructive text-destructive-foreground border-destructive",
    warning: "bg-warning-muted text-warning border-transparent",
    info: "border-border text-foreground-secondary",
  }[severity]
  return (
    <span className={cn("rounded-sm border px-1.5 py-0.5 text-xs font-semibold capitalize", styles)}>
      {severity}
    </span>
  )
}

interface AlertRowProps {
  alert: Alert
  onAck: () => void
  onResolve: () => void
  onBlock: () => void
  canBlock: boolean
  linkTags: Set<string>
  ackPending: boolean
  resolvePending: boolean
  blockPending: boolean
}

const ALERT_COLSPAN = 7

function AlertRow({ alert: a, onAck, onResolve, onBlock, canBlock, linkTags, ackPending, resolvePending, blockPending }: AlertRowProps) {
  const [expanded, setExpanded] = useState(false)
  const [explainOpen, setExplainOpen] = useState(false)

  const details = parseAlertDetails(a.details)
  const extra = details?.extra
  // Anomaly alerts are link-scoped (no IP) and carry baseline stats / a stored
  // contributor explanation in details.extra. Detect them from that payload
  // since the Alert record itself doesn't carry the rule type.
  const isAnomaly = !!extra && (extra.explanation !== undefined || extra.baseline !== undefined)
  // Link-scoped alerts (anomaly, link_capacity, disk_usage) store a synthetic
  // 0100::/64 address in target_ip; the real target is details.extra.target.
  const host = hostTarget(a)
  const target = (extra?.target || "").trim()
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
      <tr className="border-b border-border last:border-0 hover:bg-accent/50">
        <td className="py-1.5">
          <SeverityBadge severity={a.severity} />
        </td>
        <td className="py-1.5 font-medium">{a.rule_name}</td>
        <td className="py-1.5">
          {host ? (
            <Link to={`/ip/${encodeURIComponent(host)}`} className="font-ip font-medium hover:underline">
              {host}
            </Link>
          ) : target && linkTags.has(target) ? (
            <Link to={`/link/${encodeURIComponent(target)}`} className="font-medium hover:underline" title="Link">
              {target}
            </Link>
          ) : target ? (
            <span className="font-medium">{target}</span>
          ) : (
            <span className="text-muted-foreground">No single host</span>
          )}
        </td>
        <td className="py-1.5 text-right hidden sm:table-cell">
          {formatMetric(a.metric_type, a.metric_value)}
        </td>
        <td className="py-1.5 pr-3 text-right text-muted-foreground hidden sm:table-cell">
          {formatMetric(a.metric_type, a.threshold)}
        </td>
        <td className="py-1.5 pl-3 text-muted-foreground text-xs hidden md:table-cell">
          {new Date(a.triggered_at).toLocaleString()}
        </td>
        <td className="py-1.5 text-right">
          <div className="flex justify-end gap-1 items-center">
            {hasDetails && (
              <button
                onClick={() => setExpanded((s) => !s)}
                className="inline-flex items-center gap-0.5 rounded border border-input px-1.5 py-0.5 text-xs hover:bg-accent"
                title={expanded ? "Hide details" : "Show details"}
                aria-expanded={expanded}
              >
                {expanded ? <ChevronDown className="size-3" aria-hidden /> : <ChevronRight className="size-3" aria-hidden />}
                Details
              </button>
            )}
            {a.status === "active" && (
              <>
                <button
                  onClick={onAck}
                  disabled={ackPending}
                  className="rounded border border-input px-1.5 py-0.5 text-xs hover:bg-accent"
                  title="Acknowledge"
                >
                  Ack
                </button>
                {canBlock && (
                  <button
                    type="button"
                    onClick={onBlock}
                    disabled={blockPending}
                    className="rounded border border-destructive px-1.5 py-0.5 text-xs text-destructive hover:bg-destructive hover:text-destructive-foreground"
                    title="Blackhole this address through BGP (admins only)"
                  >
                    Block
                  </button>
                )}
              </>
            )}
            {a.status !== "resolved" && (
              <button
                onClick={onResolve}
                disabled={resolvePending}
                className="rounded border border-input px-1.5 py-0.5 text-xs hover:bg-accent"
                title="Resolve"
              >
                <X className="size-3" aria-label="Resolve" />
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
                  <div className="text-xs text-muted-foreground">
                    Contributors during the flagged hour (stored)
                  </div>
                  <ExplanationGrid expl={extra.explanation} />
                </div>
              )}

              {topSources.length > 0 && (
                <div className="space-y-1">
                  <div className="text-xs text-muted-foreground">
                    Top sources
                  </div>
                  <div className="flex flex-wrap gap-1.5">
                    {topSources.map((ip) => (
                      <Link
                        key={ip}
                        to={`/ip/${ip}`}
                        className="px-1.5 py-0.5 rounded border border-border bg-background text-xs text-primary hover:bg-accent"
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
                    className="inline-flex items-center gap-1 px-2 py-0.5 text-[13px] font-medium rounded border border-primary/40 bg-primary/10 text-primary hover:bg-primary/20 transition-colors"
                  >
                    <Sparkles className="size-3" />
                    {explainOpen ? "Hide live explain" : "Explain (live)"}
                  </button>

                  {explainOpen && (
                    <div className="rounded border border-border bg-background/60 p-2">
                      {explainQuery.isLoading ? (
                        <div className="flex items-center gap-2 text-[13px] text-muted-foreground">
                          <Loader2 className="size-3 animate-spin" />
                          Analysing {target} over the last hour…
                        </div>
                      ) : explainQuery.isError ? (
                        <p className="text-[13px] text-destructive">
                          {(explainQuery.error as Error)?.message || "Failed to load explanation"}
                        </p>
                      ) : liveExpl && hasContributors(liveExpl) ? (
                        <ExplanationGrid expl={liveExpl} />
                      ) : (
                        <p className="text-[13px] text-muted-foreground">
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
          <span className="text-xs text-muted-foreground">{s.label}</span>
          <span className="text-[13px] tabular-nums">{s.value}</span>
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
        <div className="text-xs text-muted-foreground mb-1">{title}</div>
        <p className="text-xs text-muted-foreground">—</p>
      </div>
    )
  }
  return (
    <div className="min-w-0">
      <div className="text-xs text-muted-foreground mb-1">{title}</div>
      <table className="w-full text-[13px]">
        <tbody>
          {rows.map((r, i) => (
            <tr key={i} className="border-b border-border/30 last:border-0">
              <td className="py-0.5 pr-2 truncate max-w-[10rem]">{r.label}</td>
              <td className="py-0.5 pr-2 text-right tabular-nums text-muted-foreground whitespace-nowrap">
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
            <Link to={`/ip/${s.ip}`} className="text-primary hover:underline font-ip">
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
            <span className="">
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


// Alert values carry their own unit (metric_type).
function formatMetric(type: Alert["metric_type"], value: number): string {
  return formatAlertMetric(value, type)
}
