import { useSearchParams } from "react-router-dom"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { useState } from "react"
import { api } from "@/lib/api"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { ErrorDisplay, EmptyState } from "@/components/ui/error"
import { TableSkeleton } from "@/components/ui/skeleton"
import { useFeatureFlags } from "@/hooks/useFeatures"
import { Shield, Trash2, Plus, FileText, Pencil } from "lucide-react"
import { cn } from "@/lib/utils"
import type { AlertRule, Hostgroup, WebhookConfig, AuditLogEntry } from "@/lib/types"

type Tab = "links" | "rules" | "hostgroups" | "webhooks" | "audit"

const TABS: { value: Tab; label: string; requiresFeature?: keyof ReturnType<typeof useFeatureFlags> }[] = [
  { value: "links", label: "Links" },
  { value: "rules", label: "Alert Rules", requiresFeature: "alerts" },
  { value: "hostgroups", label: "Hostgroups", requiresFeature: "alerts" },
  { value: "webhooks", label: "Webhooks", requiresFeature: "alerts" },
  { value: "audit", label: "Audit Log", requiresFeature: "alerts" },
]

export function Admin() {
  const [searchParams, setSearchParams] = useSearchParams()
  const features = useFeatureFlags()
  const tab = (searchParams.get("tab") as Tab) || "links"

  const setTab = (t: Tab) => {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev)
      next.set("tab", t)
      return next
    })
  }

  const visibleTabs = TABS.filter((t) => !t.requiresFeature || features[t.requiresFeature])

  return (
    <div className="space-y-4 animate-fade-in">
      <div className="flex items-baseline justify-between">
        <h1 className="text-lg font-semibold tracking-tight flex items-center gap-2">
          <Shield className="h-4 w-4" />
          Admin
        </h1>
      </div>

      {/* Tabs */}
      <div className="flex gap-1 border-b border-border overflow-x-auto">
        {visibleTabs.map((t) => (
          <button
            key={t.value}
            onClick={() => setTab(t.value)}
            className={cn(
              "px-3 py-1.5 text-xs font-medium border-b-2 -mb-px transition-colors whitespace-nowrap",
              tab === t.value ? "border-primary text-primary" : "border-transparent text-muted-foreground hover:text-foreground"
            )}
          >
            {t.label}
          </button>
        ))}
      </div>

      {tab === "links" && <LinksTab />}
      {tab === "rules" && features.alerts && <RulesTab />}
      {tab === "hostgroups" && features.alerts && <HostgroupsTab />}
      {tab === "webhooks" && features.alerts && <WebhooksTab />}
      {tab === "audit" && features.alerts && <AuditTab />}
    </div>
  )
}

// =============================================================================
// Links tab — read-only listing for now (existing /links page handles details)
// =============================================================================

function LinksTab() {
  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ["admin-links"],
    queryFn: () => api.adminLinks(),
  })

  if (error) return <ErrorDisplay error={error as Error} onRetry={() => refetch()} />

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle>Configured links</CardTitle>
      </CardHeader>
      <CardContent>
        {isLoading ? (
          <TableSkeleton rows={4} cols={5} />
        ) : !data?.data || data.data.length === 0 ? (
          <EmptyState message="No links configured" />
        ) : (
          <table className="w-full text-xs">
            <thead>
              <tr className="border-b border-border">
                <th className="pb-1.5 text-left font-medium text-muted-foreground">Tag</th>
                <th className="pb-1.5 text-left font-medium text-muted-foreground">Router IP</th>
                <th className="pb-1.5 text-right font-medium text-muted-foreground">SNMP idx</th>
                <th className="pb-1.5 text-left font-medium text-muted-foreground">Description</th>
                <th className="pb-1.5 text-right font-medium text-muted-foreground">Capacity</th>
              </tr>
            </thead>
            <tbody>
              {data.data.map((l) => (
                <tr key={l.tag} className="border-b border-border/40 last:border-0 hover:bg-accent/50">
                  <td className="py-1.5 font-mono">{l.tag}</td>
                  <td className="py-1.5 font-mono text-[11px]">{l.router_ip}</td>
                  <td className="py-1.5 text-right font-mono">{l.snmp_index}</td>
                  <td className="py-1.5 text-muted-foreground">{l.description || "-"}</td>
                  <td className="py-1.5 text-right font-mono text-muted-foreground">
                    {l.capacity_mbps ? `${l.capacity_mbps} Mbps` : "-"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </CardContent>
    </Card>
  )
}

// =============================================================================
// Rules tab
// =============================================================================

// Rule type metadata: which threshold fields are meaningful, how to label them.
// Used both by the create form and the threshold cell formatter.
const RULE_TYPE_META: Record<string, {
  label: string
  description: string
  fields: ("bps" | "pps" | "count")[]
  fieldLabels?: Partial<Record<"bps" | "pps" | "count", string>>
}> = {
  volume_in:        { label: "Inbound volume",       description: "Bandwidth/packet rate received by a single destination", fields: ["bps", "pps"] },
  volume_out:       { label: "Outbound volume",      description: "Bandwidth/packet rate sent by a single source",         fields: ["bps", "pps"] },
  syn_flood:        { label: "TCP SYN flood",        description: "TCP SYN-only packet rate to a destination",             fields: ["pps"] },
  amplification:    { label: "Reflection / amp",     description: "Many unique sources hitting one destination (with optional sustained-bps floor to filter scanners)", fields: ["count", "bps"], fieldLabels: { count: "Min unique sources", bps: "Min sustained bps" } },
  port_scan:        { label: "Port scan (outbound)", description: "An internal source touching many distinct destination ports", fields: ["count"], fieldLabels: { count: "Min unique ports" } },
  icmp_flood:       { label: "ICMP flood",           description: "ICMP packet rate to a destination",                     fields: ["pps"] },
  udp_flood:        { label: "UDP flood",            description: "UDP packet rate to a destination (DNS query flood, NTP query flood, ...)", fields: ["pps"] },
  connection_flood: { label: "Connection flood",     description: "Distinct flow count per destination — Slowloris/half-open scan signature", fields: ["count"], fieldLabels: { count: "Min flow count" } },
  subnet_flood:    { label: "Carpet bomb (subnet)", description: "Aggregate traffic to a /N subnet level before thresholding — detects distributed attacks that stay below per-host limits", fields: ["bps", "pps"] },
  smtp_abuse:      { label: "SMTP abuse (spam relay)", description: "Detects internal hosts sending traffic to SMTP ports (25/465/587) above normal levels — compromised spam relay indicator", fields: ["pps", "count"], fieldLabels: { pps: "Max pps to SMTP", count: "Max connections" } },
}

function RulesTab() {
  const queryClient = useQueryClient()
  const [showForm, setShowForm] = useState(false)
  const [draft, setDraft] = useState<Partial<AlertRule>>(emptyRule())

  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ["alert-rules"],
    queryFn: () => api.listRules(),
  })

  const toggleMutation = useMutation({
    mutationFn: (rule: AlertRule) => api.updateRule(rule.id, { ...rule, enabled: !rule.enabled }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["alert-rules"] }),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.deleteRule(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["alert-rules"] }),
  })

  const createMutation = useMutation({
    mutationFn: (r: Partial<AlertRule>) => api.createRule(r),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["alert-rules"] })
      setShowForm(false)
      setDraft(emptyRule())
    },
  })

  if (error) return <ErrorDisplay error={error as Error} onRetry={() => refetch()} />

  const rules: AlertRule[] = data?.data || []
  const meta = draft.rule_type ? RULE_TYPE_META[draft.rule_type] : null

  return (
    <Card>
      <CardHeader className="pb-2">
        <div className="flex items-center justify-between">
          <CardTitle>Alert rules ({rules.length})</CardTitle>
          <button
            onClick={() => { setShowForm((s) => !s); if (!showForm) setDraft(emptyRule()) }}
            className="inline-flex items-center gap-1 px-2 py-0.5 text-[11px] font-medium rounded border border-input bg-muted/50 hover:bg-accent transition-colors"
          >
            <Plus className="h-3 w-3" />
            {showForm ? "Cancel" : "Add rule"}
          </button>
        </div>
      </CardHeader>
      <CardContent>
        {showForm && (
          <form
            onSubmit={(e) => {
              e.preventDefault()
              createMutation.mutate(draft)
            }}
            className="space-y-2 mb-4 p-3 border border-border rounded bg-muted/20"
          >
            <div className="grid gap-2 sm:grid-cols-2">
              <Field label="Name">
                <input
                  type="text"
                  required
                  value={draft.name || ""}
                  onChange={(e) => setDraft((d) => ({ ...d, name: e.target.value }))}
                  placeholder="High volume on edge router"
                  className="flex-1 h-7 px-2 rounded border border-input bg-background text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring"
                />
              </Field>
              <Field label="Type">
                <select
                  value={draft.rule_type || ""}
                  onChange={(e) => setDraft((d) => ({ ...d, rule_type: e.target.value as AlertRule["rule_type"], threshold_bps: 0, threshold_pps: 0, threshold_count: 0 }))}
                  className="flex-1 h-7 px-2 rounded border border-input bg-background text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring"
                  required
                >
                  <option value="">— select —</option>
                  {Object.entries(RULE_TYPE_META).map(([k, v]) => (
                    <option key={k} value={k}>{v.label} ({k})</option>
                  ))}
                </select>
              </Field>
            </div>
            {meta && (
              <p className="text-[10px] text-muted-foreground italic px-1">{meta.description}</p>
            )}
            <Field label="Description">
              <input
                type="text"
                value={draft.description || ""}
                onChange={(e) => setDraft((d) => ({ ...d, description: e.target.value }))}
                placeholder="Optional — shown alongside the rule in dashboards"
                className="flex-1 h-7 px-2 rounded border border-input bg-background text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring"
              />
            </Field>
            {meta && (
              <div className="grid gap-2 sm:grid-cols-2">
                {meta.fields.includes("bps") && (
                  <Field label={meta.fieldLabels?.bps || "Threshold bps"}>
                    <input
                      type="number"
                      min={0}
                      value={draft.threshold_bps || ""}
                      onChange={(e) => setDraft((d) => ({ ...d, threshold_bps: Number(e.target.value) }))}
                      placeholder="500000000"
                      className="flex-1 h-7 px-2 rounded border border-input bg-background text-xs font-mono outline-none focus-visible:ring-1 focus-visible:ring-ring"
                    />
                  </Field>
                )}
                {meta.fields.includes("pps") && (
                  <Field label={meta.fieldLabels?.pps || "Threshold pps"}>
                    <input
                      type="number"
                      min={0}
                      value={draft.threshold_pps || ""}
                      onChange={(e) => setDraft((d) => ({ ...d, threshold_pps: Number(e.target.value) }))}
                      placeholder="50000"
                      className="flex-1 h-7 px-2 rounded border border-input bg-background text-xs font-mono outline-none focus-visible:ring-1 focus-visible:ring-ring"
                    />
                  </Field>
                )}
                {meta.fields.includes("count") && (
                  <Field label={meta.fieldLabels?.count || "Threshold count"}>
                    <input
                      type="number"
                      min={0}
                      value={draft.threshold_count || ""}
                      onChange={(e) => setDraft((d) => ({ ...d, threshold_count: Number(e.target.value) }))}
                      placeholder="10000"
                      className="flex-1 h-7 px-2 rounded border border-input bg-background text-xs font-mono outline-none focus-visible:ring-1 focus-visible:ring-ring"
                    />
                  </Field>
                )}
              </div>
            )}
            {draft.rule_type === "subnet_flood" && (
              <Field label="Subnet prefix">
                <input
                  type="number"
                  min={8}
                  max={32}
                  value={draft.subnet_prefix_len || 24}
                  onChange={(e) => setDraft((d) => ({ ...d, subnet_prefix_len: Number(e.target.value) }))}
                  className="w-20 h-7 px-2 rounded border border-input bg-background text-xs font-mono outline-none focus-visible:ring-1 focus-visible:ring-ring"
                />
                <span className="text-[10px] text-muted-foreground ml-1">/{draft.subnet_prefix_len || 24} IPv4 aggregation</span>
              </Field>
            )}
            <HostgroupSelect value={draft.hostgroup_id} onChange={(id) => setDraft((d) => ({ ...d, hostgroup_id: id }))} />
            <div className="grid gap-2 sm:grid-cols-3">
              <Field label="Window (s)">
                <input
                  type="number"
                  min={10}
                  value={draft.window_seconds || 60}
                  onChange={(e) => setDraft((d) => ({ ...d, window_seconds: Number(e.target.value) }))}
                  className="flex-1 h-7 px-2 rounded border border-input bg-background text-xs font-mono outline-none focus-visible:ring-1 focus-visible:ring-ring"
                />
              </Field>
              <Field label="Cooldown (s)">
                <input
                  type="number"
                  min={0}
                  value={draft.cooldown_seconds || 300}
                  onChange={(e) => setDraft((d) => ({ ...d, cooldown_seconds: Number(e.target.value) }))}
                  className="flex-1 h-7 px-2 rounded border border-input bg-background text-xs font-mono outline-none focus-visible:ring-1 focus-visible:ring-ring"
                />
              </Field>
              <Field label="Severity">
                <select
                  value={draft.severity || "warning"}
                  onChange={(e) => setDraft((d) => ({ ...d, severity: e.target.value as AlertRule["severity"] }))}
                  className="flex-1 h-7 px-2 rounded border border-input bg-background text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring"
                >
                  <option value="info">info</option>
                  <option value="warning">warning</option>
                  <option value="critical">critical</option>
                </select>
              </Field>
            </div>
            <button
              type="submit"
              disabled={createMutation.isPending || !draft.name || !draft.rule_type}
              className="px-3 py-1 text-xs font-medium rounded bg-primary text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
            >
              Create rule
            </button>
          </form>
        )}

        {isLoading ? (
          <TableSkeleton rows={5} cols={5} />
        ) : rules.length === 0 ? (
          <EmptyState message="No rules configured" />
        ) : (
          <div className="overflow-x-auto -mx-4 px-4 sm:-mx-5 sm:px-5">
            <table className="w-full text-xs">
              <thead>
                <tr className="border-b border-border">
                  <th className="pb-1.5 text-left font-medium text-muted-foreground">Enabled</th>
                  <th className="pb-1.5 text-left font-medium text-muted-foreground">Name</th>
                  <th className="pb-1.5 text-left font-medium text-muted-foreground">Type</th>
                  <th className="pb-1.5 text-left font-medium text-muted-foreground">Severity</th>
                  <th className="pb-1.5 text-right font-medium text-muted-foreground">Threshold</th>
                  <th className="pb-1.5 text-right font-medium text-muted-foreground">Window</th>
                  <th className="pb-1.5 text-right font-medium text-muted-foreground">Actions</th>
                </tr>
              </thead>
              <tbody>
                {rules.map((r) => (
                  <tr key={r.id} className="border-b border-border/40 last:border-0 hover:bg-accent/50">
                    <td className="py-1.5">
                      <button
                        onClick={() => toggleMutation.mutate(r)}
                        className={cn(
                          "px-1.5 py-0.5 text-[10px] rounded border font-medium",
                          r.enabled ? "border-success/40 bg-success/10 text-success" : "border-input bg-muted/50 text-muted-foreground"
                        )}
                      >
                        {r.enabled ? "ON" : "OFF"}
                      </button>
                    </td>
                    <td className="py-1.5 font-medium">
                      {r.name}
                      {r.description && <div className="text-[10px] text-muted-foreground font-normal">{r.description}</div>}
                    </td>
                    <td className="py-1.5 font-mono text-[10px] text-muted-foreground">{r.rule_type}</td>
                    <td className="py-1.5">
                      <span
                        className={cn(
                          "px-1.5 py-0.5 text-[9px] font-medium rounded border uppercase",
                          r.severity === "critical" && "border-destructive/40 bg-destructive/10 text-destructive",
                          r.severity === "warning" && "border-warning/40 bg-warning/10 text-warning",
                          r.severity === "info" && "border-primary/40 bg-primary/10 text-primary"
                        )}
                      >
                        {r.severity}
                      </span>
                    </td>
                    <td className="py-1.5 text-right font-mono">
                      {formatThreshold(r)}
                    </td>
                    <td className="py-1.5 text-right font-mono text-muted-foreground">{r.window_seconds}s</td>
                    <td className="py-1.5 text-right">
                      <button
                        onClick={() => {
                          if (confirm(`Delete rule "${r.name}"?`)) {
                            deleteMutation.mutate(r.id)
                          }
                        }}
                        className="p-1 rounded hover:bg-destructive/10 hover:text-destructive transition-colors"
                        title="Delete rule"
                      >
                        <Trash2 className="h-3 w-3" />
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function emptyRule(): Partial<AlertRule> {
  return {
    name: "",
    description: "",
    rule_type: "",
    enabled: true,
    threshold_bps: 0,
    threshold_pps: 0,
    threshold_count: 0,
    window_seconds: 60,
    cooldown_seconds: 300,
    severity: "warning",
    action: "notify",
  }
}

function formatThreshold(r: AlertRule): React.ReactNode {
  // A rule may legitimately set multiple thresholds (e.g. amplification with
  // both a unique-source count AND a sustained-bps floor). Render each
  // populated value on its own line so they never blur together visually.
  const parts: string[] = []
  if (r.threshold_count) parts.push(formatCount(r.threshold_count, r.rule_type))
  if (r.threshold_bps) parts.push(formatBpsThreshold(r.threshold_bps))
  if (r.threshold_pps) parts.push(`${r.threshold_pps.toLocaleString()} pps`)
  if (parts.length === 0) return <span className="text-muted-foreground">—</span>
  return (
    <div className="flex flex-col items-end">
      {parts.map((p, i) => (
        <span key={i} className="leading-tight">{p}</span>
      ))}
    </div>
  )
}

function formatCount(n: number, ruleType: string): string {
  const num = n.toLocaleString()
  // Suffix the unit so the user knows what the count refers to.
  switch (ruleType) {
    case "amplification":    return `${num} srcs`
    case "port_scan":        return `${num} ports`
    case "connection_flood": return `${num} flows`
    default:                 return num
  }
}

function formatBpsThreshold(bps: number): string {
  const units = ["bps", "Kbps", "Mbps", "Gbps", "Tbps"]
  let v = bps
  let i = 0
  while (v >= 1000 && i < units.length - 1) {
    v /= 1000
    i++
  }
  return `${v.toFixed(0)} ${units[i]}`
}

// =============================================================================
// Webhooks tab
// =============================================================================

function WebhooksTab() {
  const queryClient = useQueryClient()
  const [showForm, setShowForm] = useState(false)
  const [newWebhook, setNewWebhook] = useState<Partial<WebhookConfig>>({
    name: "",
    webhook_type: "slack",
    url: "",
    enabled: true,
    min_severity: "warning",
  })

  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ["webhooks"],
    queryFn: () => api.listWebhooks(),
  })

  const createMutation = useMutation({
    mutationFn: (w: Partial<WebhookConfig>) => api.createWebhook(w),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["webhooks"] })
      setShowForm(false)
      setNewWebhook({ name: "", webhook_type: "slack", url: "", enabled: true, min_severity: "warning" })
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.deleteWebhook(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["webhooks"] }),
  })

  if (error) return <ErrorDisplay error={error as Error} onRetry={() => refetch()} />

  const webhooks: WebhookConfig[] = data?.data || []

  return (
    <div className="space-y-4">
      <Card>
        <CardHeader className="pb-2">
          <div className="flex items-baseline justify-between">
            <CardTitle>Notification webhooks ({webhooks.length})</CardTitle>
            <button
              onClick={() => setShowForm(!showForm)}
              className="inline-flex items-center gap-1 px-2 py-0.5 text-[11px] font-medium rounded border border-input bg-muted/50 hover:bg-accent transition-colors"
            >
              <Plus className="h-3 w-3" />
              {showForm ? "Cancel" : "Add"}
            </button>
          </div>
        </CardHeader>
        <CardContent>
          {showForm && (
            <form
              onSubmit={(e) => {
                e.preventDefault()
                createMutation.mutate(newWebhook)
              }}
              className="space-y-2 mb-4 p-3 border border-border rounded bg-muted/20"
            >
              <div className="grid gap-2 sm:grid-cols-2">
                <Field label="Name">
                  <input
                    type="text"
                    required
                    value={newWebhook.name}
                    onChange={(e) => setNewWebhook((w) => ({ ...w, name: e.target.value }))}
                    placeholder="On-call Slack"
                    className="flex-1 h-7 px-2 rounded border border-input bg-background text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring"
                  />
                </Field>
                <Field label="Type">
                  <select
                    value={newWebhook.webhook_type}
                    onChange={(e) => setNewWebhook((w) => ({ ...w, webhook_type: e.target.value as WebhookConfig["webhook_type"] }))}
                    className="flex-1 h-7 px-2 rounded border border-input bg-background text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring"
                  >
                    <option value="slack">Slack</option>
                    <option value="teams">Microsoft Teams</option>
                    <option value="discord">Discord</option>
                    <option value="generic">Generic JSON</option>
                  </select>
                </Field>
              </div>
              <Field label="URL">
                <input
                  type="url"
                  required
                  value={newWebhook.url}
                  onChange={(e) => setNewWebhook((w) => ({ ...w, url: e.target.value }))}
                  placeholder="https://hooks.slack.com/services/..."
                  className="flex-1 h-7 px-2 rounded border border-input bg-background text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring font-mono"
                />
              </Field>
              <Field label="Min severity">
                <select
                  value={newWebhook.min_severity}
                  onChange={(e) => setNewWebhook((w) => ({ ...w, min_severity: e.target.value as WebhookConfig["min_severity"] }))}
                  className="h-7 px-2 rounded border border-input bg-background text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring"
                >
                  <option value="info">Info+</option>
                  <option value="warning">Warning+</option>
                  <option value="critical">Critical only</option>
                </select>
              </Field>
              <button
                type="submit"
                disabled={createMutation.isPending}
                className="px-3 py-1 text-xs font-medium rounded bg-primary text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
              >
                Create
              </button>
            </form>
          )}

          {isLoading ? (
            <TableSkeleton rows={3} cols={4} />
          ) : webhooks.length === 0 ? (
            <EmptyState message="No webhooks configured" />
          ) : (
            <table className="w-full text-xs">
              <thead>
                <tr className="border-b border-border">
                  <th className="pb-1.5 text-left font-medium text-muted-foreground">Name</th>
                  <th className="pb-1.5 text-left font-medium text-muted-foreground">Type</th>
                  <th className="pb-1.5 text-left font-medium text-muted-foreground">URL</th>
                  <th className="pb-1.5 text-left font-medium text-muted-foreground">Min severity</th>
                  <th className="pb-1.5 text-right font-medium text-muted-foreground">Actions</th>
                </tr>
              </thead>
              <tbody>
                {webhooks.map((w) => (
                  <tr key={w.id} className="border-b border-border/40 last:border-0 hover:bg-accent/50">
                    <td className="py-1.5 font-medium">{w.name}</td>
                    <td className="py-1.5 font-mono text-[10px] text-muted-foreground">{w.webhook_type}</td>
                    <td className="py-1.5 font-mono text-[10px] text-muted-foreground truncate max-w-xs">
                      {/* Mask all but the host part */}
                      {maskUrl(w.url)}
                    </td>
                    <td className="py-1.5 text-muted-foreground">{w.min_severity}</td>
                    <td className="py-1.5 text-right">
                      <button
                        onClick={() => {
                          if (confirm(`Delete webhook "${w.name}"?`)) {
                            deleteMutation.mutate(w.id)
                          }
                        }}
                        className="p-1 rounded hover:bg-destructive/10 hover:text-destructive transition-colors"
                      >
                        <Trash2 className="h-3 w-3" />
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

function maskUrl(url: string): string {
  try {
    const u = new URL(url)
    return `${u.protocol}//${u.host}/...`
  } catch {
    return url.length > 40 ? url.slice(0, 40) + "..." : url
  }
}

// =============================================================================
// Audit log tab
// =============================================================================

function AuditTab() {
  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ["audit-log"],
    queryFn: () => api.auditLog({ limit: 200 }),
  })

  if (error) return <ErrorDisplay error={error as Error} onRetry={() => refetch()} />

  const entries: AuditLogEntry[] = data?.data || []

  return (
    <Card>
      <CardHeader className="pb-2">
        <div className="flex items-baseline justify-between">
          <CardTitle className="flex items-center gap-2">
            <FileText className="h-3.5 w-3.5" />
            Audit log (last 200 entries)
          </CardTitle>
          <span className="text-[10px] text-muted-foreground">365-day retention</span>
        </div>
      </CardHeader>
      <CardContent>
        {isLoading ? (
          <TableSkeleton rows={10} cols={6} />
        ) : entries.length === 0 ? (
          <EmptyState message="No audit entries yet" />
        ) : (
          <div className="overflow-x-auto -mx-4 px-4 sm:-mx-5 sm:px-5">
            <table className="w-full text-xs">
              <thead>
                <tr className="border-b border-border">
                  <th className="pb-1.5 text-left font-medium text-muted-foreground">Time</th>
                  <th className="pb-1.5 text-left font-medium text-muted-foreground">User</th>
                  <th className="pb-1.5 text-left font-medium text-muted-foreground">Action</th>
                  <th className="pb-1.5 text-left font-medium text-muted-foreground hidden md:table-cell">Resource</th>
                  <th className="pb-1.5 text-left font-medium text-muted-foreground hidden md:table-cell">Client IP</th>
                  <th className="pb-1.5 text-left font-medium text-muted-foreground">Result</th>
                </tr>
              </thead>
              <tbody>
                {entries.map((e, i) => (
                  <tr key={i} className="border-b border-border/40 last:border-0 hover:bg-accent/50">
                    <td className="py-1 text-[10px] font-mono text-muted-foreground whitespace-nowrap">
                      {new Date(e.ts).toLocaleString()}
                    </td>
                    <td className="py-1">{e.user_email || <span className="text-muted-foreground">anonymous</span>}</td>
                    <td className="py-1 font-mono text-[10px]">{e.action}</td>
                    <td className="py-1 font-mono text-[10px] text-muted-foreground hidden md:table-cell">{e.resource}</td>
                    <td className="py-1 font-mono text-[10px] text-muted-foreground hidden md:table-cell">{e.client_ip}</td>
                    <td className="py-1">
                      <span
                        className={cn(
                          "px-1.5 py-0.5 text-[9px] font-medium rounded uppercase",
                          e.result === "success" && "bg-success/20 text-success",
                          e.result === "denied" && "bg-warning/20 text-warning",
                          e.result === "error" && "bg-destructive/20 text-destructive"
                        )}
                      >
                        {e.result}
                      </span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

// =============================================================================
// Hostgroups tab
// =============================================================================

function HostgroupsTab() {
  const queryClient = useQueryClient()
  const [showForm, setShowForm] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [draft, setDraft] = useState({ name: "", description: "", cidrsText: "" })

  const resetForm = () => {
    setShowForm(false)
    setEditingId(null)
    setDraft({ name: "", description: "", cidrsText: "" })
  }

  const startEdit = (g: Hostgroup) => {
    setEditingId(g.id)
    setDraft({ name: g.name, description: g.description || "", cidrsText: g.cidrs.join("\n") })
    setShowForm(true)
  }

  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ["hostgroups"],
    queryFn: () => api.listHostgroups(),
  })

  const createMutation = useMutation({
    mutationFn: (hg: Partial<Hostgroup>) => api.createHostgroup(hg),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["hostgroups"] })
      resetForm()
    },
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, hg }: { id: string; hg: Partial<Hostgroup> }) => api.updateHostgroup(id, hg),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["hostgroups"] })
      resetForm()
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.deleteHostgroup(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["hostgroups"] }),
  })

  if (error) return <ErrorDisplay error={error as Error} onRetry={() => refetch()} />

  const groups: Hostgroup[] = data?.data || []
  const isEditing = editingId !== null
  const isSaving = createMutation.isPending || updateMutation.isPending

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    const cidrs = draft.cidrsText.split(/[\n,]+/).map((s) => s.trim()).filter(Boolean)
    const payload = { name: draft.name, description: draft.description, cidrs }
    if (isEditing) {
      updateMutation.mutate({ id: editingId, hg: payload })
    } else {
      createMutation.mutate(payload)
    }
  }

  return (
    <Card>
      <CardHeader className="pb-2">
        <div className="flex items-center justify-between">
          <CardTitle>Hostgroups ({groups.length})</CardTitle>
          <button
            onClick={() => showForm ? resetForm() : setShowForm(true)}
            className="inline-flex items-center gap-1 px-2 py-0.5 text-[11px] font-medium rounded border border-input bg-muted/50 hover:bg-accent transition-colors"
          >
            <Plus className="h-3 w-3" />
            {showForm ? "Cancel" : "Add"}
          </button>
        </div>
      </CardHeader>
      <CardContent>
        {showForm && (
          <form onSubmit={handleSubmit} className="space-y-2 mb-4 p-3 border border-border rounded bg-muted/20">
            <div className="grid gap-2 sm:grid-cols-2">
              <Field label="Name">
                <input type="text" required value={draft.name}
                  onChange={(e) => setDraft((d) => ({ ...d, name: e.target.value }))}
                  placeholder="CDN servers"
                  className="flex-1 h-7 px-2 rounded border border-input bg-background text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring" />
              </Field>
              <Field label="Description">
                <input type="text" value={draft.description}
                  onChange={(e) => setDraft((d) => ({ ...d, description: e.target.value }))}
                  placeholder="Optional"
                  className="flex-1 h-7 px-2 rounded border border-input bg-background text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring" />
              </Field>
            </div>
            <Field label="CIDRs">
              <textarea
                required
                value={draft.cidrsText}
                onChange={(e) => setDraft((d) => ({ ...d, cidrsText: e.target.value }))}
                placeholder={"10.0.1.0/24\n10.0.2.0/24"}
                rows={3}
                className="flex-1 px-2 py-1 rounded border border-input bg-background text-xs font-mono outline-none focus-visible:ring-1 focus-visible:ring-ring"
              />
            </Field>
            <button type="submit" disabled={isSaving}
              className="px-3 py-1 text-xs font-medium rounded bg-primary text-primary-foreground hover:bg-primary/90 disabled:opacity-50">
              {isEditing ? "Save" : "Create"}
            </button>
          </form>
        )}

        {isLoading ? (
          <TableSkeleton rows={3} cols={4} />
        ) : groups.length === 0 ? (
          <EmptyState message="No hostgroups — rules use global LOCAL_AS prefixes" />
        ) : (
          <table className="w-full text-xs">
            <thead>
              <tr className="border-b border-border">
                <th className="pb-1.5 text-left font-medium text-muted-foreground">Name</th>
                <th className="pb-1.5 text-left font-medium text-muted-foreground">CIDRs</th>
                <th className="pb-1.5 text-left font-medium text-muted-foreground hidden md:table-cell">Description</th>
                <th className="pb-1.5 text-right font-medium text-muted-foreground">Actions</th>
              </tr>
            </thead>
            <tbody>
              {groups.map((g) => (
                <tr key={g.id} className={cn("border-b border-border/40 last:border-0 hover:bg-accent/50", editingId === g.id && "bg-primary/5")}>
                  <td className="py-1.5 font-medium">{g.name}</td>
                  <td className="py-1.5 font-mono text-[10px] text-muted-foreground">{g.cidrs.join(", ")}</td>
                  <td className="py-1.5 text-muted-foreground hidden md:table-cell">{g.description || "—"}</td>
                  <td className="py-1.5 text-right">
                    <div className="flex justify-end gap-0.5">
                      <button
                        onClick={() => startEdit(g)}
                        className="p-1 rounded hover:bg-accent transition-colors"
                        title="Edit hostgroup"
                      >
                        <Pencil className="h-3 w-3" />
                      </button>
                      <button
                        onClick={() => { if (confirm(`Delete hostgroup "${g.name}"?`)) deleteMutation.mutate(g.id) }}
                        className="p-1 rounded hover:bg-destructive/10 hover:text-destructive transition-colors"
                        title="Delete hostgroup"
                      >
                        <Trash2 className="h-3 w-3" />
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </CardContent>
    </Card>
  )
}

// Hostgroup selector reused in the rule create form
function HostgroupSelect({ value, onChange }: { value?: string; onChange: (id: string) => void }) {
  const { data } = useQuery({ queryKey: ["hostgroups"], queryFn: () => api.listHostgroups() })
  const groups: Hostgroup[] = data?.data || []
  if (groups.length === 0) return null
  return (
    <Field label="Hostgroup">
      <select value={value || ""} onChange={(e) => onChange(e.target.value)}
        className="flex-1 h-7 px-2 rounded border border-input bg-background text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring">
        <option value="">(global — all LOCAL_AS prefixes)</option>
        {groups.map((g) => <option key={g.id} value={g.id}>{g.name} ({g.cidrs.length} CIDRs)</option>)}
      </select>
    </Field>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="flex items-center gap-2">
      <span className="text-[10px] text-muted-foreground w-20 shrink-0">{label}</span>
      {children}
    </label>
  )
}
