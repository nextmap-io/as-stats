import { useSearchParams } from "react-router-dom"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { useEffect, useRef, useState } from "react"
import { api } from "@/lib/api"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { ErrorDisplay, EmptyState } from "@/components/ui/error"
import { TableSkeleton } from "@/components/ui/skeleton"
import { useFeatureFlags } from "@/hooks/useFeatures"
import {
  useStorageStatus,
  useSetRetention,
  useReportSchedules,
  useCreateReportSchedule,
  useUpdateReportSchedule,
  useDeleteReportSchedule,
  useTestReport,
  useAPITokens,
  useCreateAPIToken,
  useRevokeAPIToken,
} from "@/hooks/useApi"
import { Trash2, Plus, FileText, Pencil, HardDrive, Check, X, Mail, Send, KeyRound, Copy } from "lucide-react"
import { cn, formatBytes, formatNumber, formatPercent } from "@/lib/utils"
import { formatCapacity, formatDateTime } from "@/lib/format"
import { diskLevel, levelTextClass } from "@/lib/home"
import { StateBadge } from "@/components/ui/state-badge"
import type {
  AlertRule,
  Hostgroup,
  WebhookConfig,
  AuditLogEntry,
  TableStorageStats,
  DiskStats,
  ReportSchedule,
  ReportFrequency,
  ReportFormat,
  ReportSection,
  APIToken,
  APITokenCreated,
} from "@/lib/types"

type Tab = "links" | "storage" | "tokens" | "rules" | "hostgroups" | "webhooks" | "reports" | "audit"

const TABS: { value: Tab; label: string; requiresFeature?: keyof ReturnType<typeof useFeatureFlags> }[] = [
  { value: "links", label: "Links" },
  { value: "storage", label: "Storage" },
  { value: "tokens", label: "Tokens" },
  { value: "rules", label: "Alert rules", requiresFeature: "alerts" },
  { value: "hostgroups", label: "Hostgroups", requiresFeature: "alerts" },
  { value: "webhooks", label: "Webhooks", requiresFeature: "alerts" },
  { value: "reports", label: "Reports", requiresFeature: "reports" },
  { value: "audit", label: "Audit log", requiresFeature: "alerts" },
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
    <div className="space-y-4">
      <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-2">
        <h1 className="text-2xl font-semibold">Admin</h1>
      </div>

      {/* Tabs */}
      <div className="flex gap-1 border-b border-border overflow-x-auto">
        {visibleTabs.map((t) => (
          <button
            key={t.value}
            onClick={() => setTab(t.value)}
            aria-current={tab === t.value ? "page" : undefined}
            className={cn(
              "min-h-8 px-3 py-1.5 text-[13px] border-b-2 -mb-px transition-colors whitespace-nowrap",
              tab === t.value ? "border-foreground font-semibold text-foreground" : "border-transparent text-foreground-secondary hover:text-foreground"
            )}
          >
            {t.label}
          </button>
        ))}
      </div>

      {tab === "links" && <LinksTab />}
      {tab === "storage" && <StorageTab />}
      {tab === "tokens" && <TokensTab />}
      {tab === "rules" && features.alerts && <RulesTab />}
      {tab === "hostgroups" && features.alerts && <HostgroupsTab />}
      {tab === "webhooks" && features.alerts && <WebhooksTab />}
      {tab === "reports" && features.reports && <ReportsTab />}
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
      <CardHeader>
        <CardTitle>Configured links</CardTitle>
      </CardHeader>
      <CardContent>
        {isLoading ? (
          <TableSkeleton rows={4} cols={5} />
        ) : !data?.data || data.data.length === 0 ? (
          <EmptyState message="No links configured" />
        ) : (
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-foreground">
                <th className="pb-1.5 text-left text-[13px] font-normal text-muted-foreground">Tag</th>
                <th className="pb-1.5 text-left text-[13px] font-normal text-muted-foreground">Router IP</th>
                <th className="pb-1.5 text-right text-[13px] font-normal text-muted-foreground">SNMP index</th>
                <th className="pb-1.5 pl-6 text-left text-[13px] font-normal text-muted-foreground">Description</th>
                <th className="pb-1.5 text-right text-[13px] font-normal text-muted-foreground">Capacity</th>
              </tr>
            </thead>
            <tbody>
              {data.data.map((l) => (
                <tr key={l.tag} className="border-b border-border-soft last:border-0 hover:bg-accent/50">
                  <td className="py-1.5">{l.tag}</td>
                  <td className="py-1.5 font-ip text-[13px]">{l.router_ip}</td>
                  <td className="py-1.5 text-right">{l.snmp_index}</td>
                  <td className="py-1.5 pl-6 text-muted-foreground">{l.description || "-"}</td>
                  <td className="py-1.5 text-right text-muted-foreground">
                    {l.capacity_mbps ? formatCapacity(l.capacity_mbps * 1e6) : "-"}
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
// Storage tab — per-table size/rows/parts + editable retention + disk gauge
// =============================================================================

function StorageTab() {
  const { data, isLoading, error, refetch } = useStorageStatus()

  if (error) return <ErrorDisplay error={error as Error} onRetry={() => refetch()} />

  const tables: TableStorageStats[] = data?.data?.tables || []
  const disks: DiskStats[] = data?.data?.disks || []
  const pendingTotal = tables.reduce((acc, t) => acc + t.pending_mutations, 0)
  const anyCold = tables.some((t) => t.has_cold_volume)

  return (
    <div className="space-y-4">
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <HardDrive className="size-3.5" />
            Disk usage
          </CardTitle>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <TableSkeleton rows={2} cols={1} />
          ) : disks.length === 0 ? (
            <EmptyState message="No disk information available" />
          ) : (
            <div className="space-y-3">
              {disks.map((d) => (
                <DiskGauge key={d.name} disk={d} />
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-2">
            <CardTitle>Tables ({tables.length})</CardTitle>
            {pendingTotal > 0 && (
              <span
                className="px-1.5 py-0.5 text-xs font-medium rounded border border-warning/40 bg-warning/10 text-warning"
                title={`${formatNumber(pendingTotal)} pending mutation(s) across all tables — TTL changes are still materializing`}
              >
                TTL lag · {formatNumber(pendingTotal)}
              </span>
            )}
          </div>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <TableSkeleton rows={8} cols={9} />
          ) : tables.length === 0 ? (
            <EmptyState message="No tables found" />
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-foreground">
                    <th className="pb-1.5 text-left text-[13px] font-normal text-muted-foreground">Table</th>
                    <th className="pb-1.5 text-right text-[13px] font-normal text-muted-foreground">Compressed</th>
                    <th className="pb-1.5 text-right text-[13px] font-normal text-muted-foreground hidden lg:table-cell" title="Bytes on disk: hot (fast disk) / cold volume">
                      Hot / cold
                    </th>
                    <th className="pb-1.5 text-right text-[13px] font-normal text-muted-foreground">Rows</th>
                    <th className="pb-1.5 text-right text-[13px] font-normal text-muted-foreground">Parts</th>
                    <th className="pb-1.5 text-left pl-6 text-[13px] font-normal text-muted-foreground hidden md:table-cell">Oldest data</th>
                    <th className="pb-1.5 text-right text-[13px] font-normal text-muted-foreground">TTL (days)</th>
                    <th className="pb-1.5 text-right text-[13px] font-normal text-muted-foreground" title="Move data older than this many days to the cold volume (0 = off)">
                      Move to cold after (days)
                    </th>
                    <th className="pb-1.5 text-center text-[13px] font-normal text-muted-foreground">Managed</th>
                  </tr>
                </thead>
                <tbody>
                  {tables.map((t) => (
                    <StorageRow key={t.table} table={t} />
                  ))}
                </tbody>
              </table>
            </div>
          )}
          {!isLoading && tables.length > 0 && (
            <p className="mt-3 text-xs text-muted-foreground">
              {anyCold
                ? "Move to cold applies to tables whose storage policy has a volume named “cold”; older partitions are moved a few at a time by the reconciler. 0 keeps everything on the hot disk."
                : "No table uses a storage policy with a “cold” volume, so moving data to a second disk is unavailable. See “Cold storage on a second disk” in the README."}
            </p>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

function DiskGauge({ disk }: { disk: DiskStats }) {
  const pct = Math.min(100, Math.max(0, disk.used_percent))
  // Threshold colours mirror the disk_usage alert defaults (80% warn, 90% crit).
  // A normal disk stays unlit (no green); a full one gets the colour and the word.
  const level = diskLevel(pct)
  const barColor =
    level === "critical" ? "bg-destructive" : level === "watch" ? "bg-warning-mark" : "bg-foreground-secondary"

  return (
    <div>
      <div className="flex items-baseline justify-between gap-3 mb-1">
        <span className="min-w-0 truncate text-[13px] text-foreground">
          {disk.name}
          {disk.path && <span className="ml-2 font-mono text-xs text-muted-foreground">{disk.path}</span>}
        </span>
        <span className={cn("inline-flex shrink-0 items-center gap-1.5 tabular-nums text-[13px]", levelTextClass(level))}>
          <StateBadge level={level} />
          {formatPercent(pct)}
        </span>
      </div>
      <div className="h-2 w-full rounded-[1px] bg-muted overflow-hidden">
        <div
          className={cn("h-full rounded-[1px] transition-colors", barColor)}
          style={{ width: `${pct}%` }}
        />
      </div>
      <div className="mt-1 text-xs text-muted-foreground tabular-nums">
        {formatBytes(disk.used_bytes)} used · {formatBytes(disk.free_bytes)} free · {formatBytes(disk.total_bytes)} total
      </div>
    </div>
  )
}

// DaysField is an inline-editable day count with save / discard buttons.
//
// It adopts the server value whenever it actually changes (own save, 30s
// refetch, another admin's edit) unless the field is focused. Without this the
// draft would keep the value read at mount and could push a stale value back
// over someone else's change. A focused field is left alone so we never yank a
// half-typed number out from under the operator.
function DaysField({
  value,
  min,
  max,
  disabled,
  title,
  label,
  zeroLabel,
  saveTitle,
  onSave,
}: {
  value: number
  min: number
  max?: number
  disabled: boolean
  title?: string
  /** Accessible name of the input, e.g. "Retention of flows_raw, days". */
  label: string
  /** Shown (as a placeholder) instead of a bare "0": "not set", "off". */
  zeroLabel: string
  saveTitle: string
  onSave: (days: number) => void
}) {
  // 0 is "no policy" for a TTL and "off" for the cold tier: an input showing
  // "0" read as "keep 0 days", one Enter away from a destructive save.
  const serverValue = value === 0 ? "" : String(value)
  const [draft, setDraft] = useState<string>(serverValue)
  const editingRef = useRef(false)

  useEffect(() => {
    if (!editingRef.current) setDraft(serverValue)
  }, [serverValue])

  const dirty = draft !== serverValue

  const save = () => {
    const days = Number(draft)
    if (!Number.isInteger(days) || days < min || (max !== undefined && days > max)) {
      setDraft(serverValue)
      return
    }
    if (days === value) return
    onSave(days)
  }

  return (
    <div className="flex items-center justify-end gap-1" title={title}>
      <input
        type="number"
        min={min}
        max={max}
        value={draft}
        placeholder={zeroLabel}
        aria-label={label}
        disabled={disabled}
        onChange={(e) => setDraft(e.target.value)}
        onFocus={() => { editingRef.current = true }}
        onBlur={() => { editingRef.current = false }}
        onKeyDown={(e) => { if (e.key === "Enter") save() }}
        className="w-[4.5rem] h-7 px-1.5 rounded border border-input bg-background text-[13px] tabular-nums text-right outline-none placeholder:text-muted-foreground focus-visible:ring-1 focus-visible:ring-ring disabled:opacity-50"
      />
      {dirty && !disabled && (
        <div className="flex gap-0.5">
          <button
            onClick={save}
            className="inline-flex size-7 items-center justify-center rounded hover:bg-accent transition-colors"
            title={saveTitle}
            aria-label={saveTitle}
          >
            <Check className="size-3" />
          </button>
          <button
            onClick={() => setDraft(serverValue)}
            className="inline-flex size-7 items-center justify-center rounded hover:bg-accent transition-colors"
            title="Discard"
            aria-label="Discard"
          >
            <X className="size-3" />
          </button>
        </div>
      )}
    </div>
  )
}

/** What each pre-upgrade table kept by a migration is, for its Storage row. */
const LEGACY_TABLES: Record<string, { badge: string; note: string }> = {
  flows_log_legacy: {
    badge: "legacy (pre-3.4 layout), expires by itself",
    note: "Pre-3.4 layout, kept read-only and still searchable.",
  },
  traffic_by_port_legacy: {
    badge: "legacy (pre-3.5 port stats), expires by itself",
    note: "Pre-3.5 port statistics (every flow counted twice), kept until it expires and no longer shown anywhere.",
  },
}
const LEGACY_FALLBACK = { badge: "legacy, expires by itself", note: "Kept from an earlier version until it expires." }

function StorageRow({ table }: { table: TableStorageStats }) {
  const setRetention = useSetRetention()

  // cold_after_days is omitted on ttl / enabled changes: the server keeps the
  // table's current cold tier (and rejects a ttl that would no longer exceed it).
  const saveDays = (days: number) =>
    setRetention.mutate({ table: table.table, ttl_days: days, enabled: table.ttl_enabled })

  const saveCold = (days: number) =>
    setRetention.mutate({
      table: table.table,
      ttl_days: table.ttl_days,
      enabled: table.ttl_enabled,
      cold_after_days: days,
    })

  const toggleEnabled = () => {
    setRetention.mutate({
      table: table.table,
      ttl_days: table.ttl_days || 1,
      enabled: !table.ttl_enabled,
    })
  }

  const diskBreakdown = table.disks
    .map((d) => `${d.disk}${d.cold ? " (cold)" : ""}: ${formatBytes(d.bytes)} in ${formatNumber(d.parts)} part(s)`)
    .join("\n")

  const coldTitle = table.has_cold_volume
    ? `Partitions older than this move to the cold volume of storage policy “${table.storage_policy}” (0 = off; must be lower than the TTL)`
    : `Storage policy “${table.storage_policy || "default"}” has no volume named “cold” — switch the table to a hot/cold policy first (README › Cold storage on a second disk)`

  // A follower (flows_log_legacy, traffic_by_port_legacy) has no policy of
  // its own: its row shows the leader's values read-only.
  const follows = table.retention_follows
  const legacy = table.legacy ? (LEGACY_TABLES[table.table] ?? LEGACY_FALLBACK) : undefined
  const followsTitle = follows
    ? `${legacy ? `${legacy.note} ` : ""}Retention follows ${follows}: the table expires by itself under that policy — change it on the ${follows} row.`
    : undefined

  return (
    <tr className="border-b border-border-soft last:border-0 hover:bg-accent/50">
      <td className="py-1.5 text-[13px]">
        <div className="flex items-center gap-1.5">
          {table.table}
          {table.legacy && (
            <span
              className="px-1 py-0.5 text-xs rounded border border-input text-muted-foreground"
              title={followsTitle}
            >
              {legacy?.badge}
            </span>
          )}
          {table.pending_mutations > 0 && (
            <span
              className="px-1 py-0.5 text-xs font-medium rounded border border-warning/40 bg-warning/10 text-warning"
              title={`${formatNumber(table.pending_mutations)} pending mutation(s) — TTL change still materializing`}
            >
              TTL lag
            </span>
          )}
        </div>
        {setRetention.isError && (
          <div className="text-xs text-destructive">{(setRetention.error as Error).message}</div>
        )}
      </td>
      <td className="py-1.5 text-right tabular-nums">{formatBytes(table.compressed_bytes)}</td>
      <td className="py-1.5 text-right tabular-nums text-muted-foreground hidden lg:table-cell whitespace-nowrap" title={diskBreakdown}>
        {formatBytes(table.hot_bytes)}
        <span className="mx-1">/</span>
        {table.has_cold_volume || table.cold_bytes > 0 ? formatBytes(table.cold_bytes) : "—"}
      </td>
      <td className="py-1.5 text-right tabular-nums text-muted-foreground">{formatNumber(table.rows)}</td>
      <td className="py-1.5 text-right tabular-nums text-muted-foreground">{formatNumber(table.parts)}</td>
      <td className="py-1.5 pl-6 text-[13px] text-muted-foreground hidden md:table-cell whitespace-nowrap">
        {table.oldest_data ? formatDateTime(table.oldest_data) : "—"}
      </td>
      {follows ? (
        <>
          <td className="py-1.5 text-right tabular-nums text-xs text-muted-foreground" title={followsTitle}>
            {table.ttl_days}
          </td>
          <td className="py-1.5 text-right tabular-nums text-xs text-muted-foreground" title={followsTitle}>
            {table.cold_after_days}
          </td>
          <td className="py-1.5 text-center text-xs text-muted-foreground whitespace-nowrap" title={followsTitle}>
            as {follows}
          </td>
        </>
      ) : (
        <>
          <td className="py-1.5 text-right">
            <DaysField
              value={table.ttl_days}
              min={Math.max(1, table.cold_after_days + 1)}
              disabled={setRetention.isPending}
              label={`Retention of ${table.table}, days`}
              zeroLabel="not set"
              saveTitle="Save retention"
              onSave={saveDays}
            />
          </td>
          <td className="py-1.5 text-right">
            <DaysField
              value={table.cold_after_days}
              min={0}
              max={Math.max(0, table.ttl_days - 1)}
              disabled={setRetention.isPending || !table.has_cold_volume}
              title={coldTitle}
              label={`Move ${table.table} to cold after, days`}
              zeroLabel="off"
              saveTitle="Save move-to-cold age"
              onSave={saveCold}
            />
          </td>
          <td className="py-1.5 text-center">
            <button
              onClick={toggleEnabled}
              disabled={setRetention.isPending}
              aria-pressed={table.ttl_enabled}
              aria-label={`Reconciler manages ${table.table}`}
              // A setting, not a state to watch: neutral, never green (Ledger).
              className={cn(
                "min-h-7 min-w-10 px-2 text-[13px] rounded border disabled:opacity-50 transition-colors",
                table.ttl_enabled
                  ? "border-foreground text-foreground"
                  : "border-input bg-background text-muted-foreground hover:text-foreground"
              )}
              title={table.ttl_enabled ? "Reconciler manages this table — click to disable" : "Unmanaged — click to enable reconciler"}
            >
              {table.ttl_enabled ? "on" : "off"}
            </button>
          </td>
        </>
      )}
    </tr>
  )
}

// =============================================================================
// Tokens tab — read-only API tokens (Module G)
//
// Tokens grant viewer-role, GET/HEAD-only programmatic access via a Bearer
// header. The plaintext is returned exactly once on creation and never stored;
// list rows expose only the display prefix, never the hash or plaintext.
// =============================================================================

// The backend stores "never expires" / "never used" as the Unix epoch
// (1970-01-01T00:00:00Z), i.e. a non-positive timestamp. Treat those as unset.
function isUnsetTime(ts: string): boolean {
  const t = new Date(ts).getTime()
  return !Number.isFinite(t) || t <= 0
}

type TokenStatus = "active" | "revoked" | "expired"

function tokenStatus(t: APIToken): TokenStatus {
  if (t.revoked) return "revoked"
  if (!isUnsetTime(t.expires_at) && new Date(t.expires_at).getTime() < Date.now()) return "expired"
  return "active"
}

// minExpiryDate is tomorrow (UTC) as YYYY-MM-DD — the earliest selectable expiry
// for a new token. Kept in a helper so the impure Date.now stays out of render.
function minExpiryDate(): string {
  return new Date(Date.now() + 86_400_000).toISOString().slice(0, 10)
}

// daysUntil converts a YYYY-MM-DD date (interpreted as end-of-day UTC) into a
// whole number of days from now, for the backend's expires_in_days contract.
// Returns 0 (never) for an empty/past date.
function daysUntil(dateStr: string): number {
  if (!dateStr) return 0
  const target = new Date(`${dateStr}T23:59:59Z`).getTime()
  if (!Number.isFinite(target)) return 0
  const days = Math.ceil((target - Date.now()) / 86_400_000)
  return days > 0 ? days : 0
}

function TokensTab() {
  const [showForm, setShowForm] = useState(false)
  const [name, setName] = useState("")
  const [expiryDate, setExpiryDate] = useState("")
  // Plaintext of the just-minted token — shown once, then dismissed forever.
  const [minted, setMinted] = useState<APITokenCreated | null>(null)
  const [copied, setCopied] = useState(false)

  const { data, isLoading, error, refetch } = useAPITokens()
  const createMutation = useCreateAPIToken()
  const revokeMutation = useRevokeAPIToken()

  const resetForm = () => {
    setShowForm(false)
    setName("")
    setExpiryDate("")
  }

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    const trimmed = name.trim()
    if (!trimmed) return
    createMutation.mutate(
      { name: trimmed, expires_in_days: daysUntil(expiryDate) },
      {
        onSuccess: (res) => {
          setMinted(res.data)
          setCopied(false)
          resetForm()
        },
      },
    )
  }

  const copyToken = async () => {
    if (!minted) return
    try {
      await navigator.clipboard.writeText(minted.token)
      setCopied(true)
    } catch {
      setCopied(false)
    }
  }

  if (error) return <ErrorDisplay error={error as Error} onRetry={() => refetch()} />

  const tokens: APIToken[] = data?.data || []

  return (
    <Card>
      <CardHeader>
        <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
          <CardTitle className="flex items-center gap-2">
            <KeyRound className="size-3.5" />
            API tokens ({tokens.length})
          </CardTitle>
          <button
            onClick={() => (showForm ? resetForm() : setShowForm(true))}
            className="inline-flex items-center gap-1 px-2 py-0.5 text-[13px] font-medium rounded border border-input bg-background hover:bg-accent transition-colors"
          >
            <Plus className="size-3" />
            {showForm ? "Cancel" : "Mint token"}
          </button>
        </div>
      </CardHeader>
      <CardContent>
        <p className="text-xs text-muted-foreground mb-3">
          Read-only tokens grant <span className="font-mono">GET</span>-only programmatic access via an{" "}
          <span className="">Authorization: Bearer</span> header. They carry viewer permissions and
          bypass CSRF for token auth only.
        </p>

        {/* One-time plaintext reveal — shown once after minting, never again. */}
        {minted && (
          <div className="mb-4 p-3 rounded border border-warning/50 bg-warning/10 space-y-2">
            <div className="flex items-center gap-1.5 text-warning">
              <KeyRound className="size-3.5" />
              <span className="text-xs font-semibold">Copy your new token now</span>
            </div>
            <p className="text-xs text-muted-foreground">
              This is the only time <span className="font-medium">{minted.name}</span> will be shown. It cannot be
              retrieved again — store it somewhere safe.
            </p>
            <div className="flex flex-wrap items-center gap-2">
              <code className="flex-1 px-2 py-1.5 rounded border border-input bg-background font-mono text-[13px] break-all select-all">
                {minted.token}
              </code>
              <button
                onClick={copyToken}
                className={cn(
                  "inline-flex items-center gap-1 px-2 py-1.5 text-[13px] font-medium rounded border transition-colors shrink-0",
                  copied
                    ? "border-success/40 bg-success/10 text-success"
                    : "border-input bg-background hover:bg-accent"
                )}
                title="Copy token to clipboard"
              >
                {copied ? <Check className="size-3" /> : <Copy className="size-3" />}
                {copied ? "Copied" : "Copy"}
              </button>
            </div>
            <button
              onClick={() => setMinted(null)}
              className="text-xs text-muted-foreground hover:text-foreground underline underline-offset-2"
            >
              I&apos;ve saved it — dismiss
            </button>
          </div>
        )}

        {showForm && (
          <form onSubmit={handleSubmit} className="space-y-2 mb-4 p-3 border border-border rounded bg-muted/20">
            <div className="grid gap-2 sm:grid-cols-2">
              <Field label="Name">
                <input
                  type="text"
                  required
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="Grafana datasource"
                  className="flex-1 h-7 px-2 rounded border border-input bg-background text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring"
                />
              </Field>
              <Field label="Expires">
                <input
                  type="date"
                  value={expiryDate}
                  min={minExpiryDate()}
                  onChange={(e) => setExpiryDate(e.target.value)}
                  className="flex-1 h-7 px-2 rounded border border-input bg-background text-xs tabular-nums outline-none focus-visible:ring-1 focus-visible:ring-ring"
                />
              </Field>
            </div>
            <p className="text-xs text-muted-foreground italic px-1">
              Leave the expiry empty for a token that never expires.
            </p>
            <button
              type="submit"
              disabled={createMutation.isPending || !name.trim()}
              className="px-3 py-1 text-xs font-medium rounded bg-primary text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
            >
              Mint token
            </button>
          </form>
        )}

        {isLoading ? (
          <TableSkeleton rows={4} cols={6} />
        ) : tokens.length === 0 ? (
          <EmptyState message="No API tokens — mint one for read-only programmatic access" />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-foreground">
                  <th className="pb-1.5 text-left text-[13px] font-normal text-muted-foreground">Name</th>
                  <th className="pb-1.5 text-left text-[13px] font-normal text-muted-foreground">Prefix</th>
                  <th className="pb-1.5 text-left text-[13px] font-normal text-muted-foreground hidden md:table-cell">Owner</th>
                  <th className="pb-1.5 text-left text-[13px] font-normal text-muted-foreground hidden lg:table-cell">Created</th>
                  <th className="pb-1.5 text-left text-[13px] font-normal text-muted-foreground hidden lg:table-cell">Last used</th>
                  <th className="pb-1.5 text-left text-[13px] font-normal text-muted-foreground">Expires</th>
                  <th className="pb-1.5 text-left text-[13px] font-normal text-muted-foreground">Status</th>
                  <th className="pb-1.5 text-right text-[13px] font-normal text-muted-foreground">Actions</th>
                </tr>
              </thead>
              <tbody>
                {tokens.map((t) => (
                  <TokenRow key={t.id} token={t} onRevoke={(id) => revokeMutation.mutate(id)} revoking={revokeMutation.isPending} />
                ))}
              </tbody>
            </table>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function TokenRow({ token, onRevoke, revoking }: { token: APIToken; onRevoke: (id: string) => void; revoking: boolean }) {
  const status = tokenStatus(token)
  return (
    <tr className="border-b border-border-soft last:border-0 hover:bg-accent/50">
      <td className="py-1.5 font-medium">{token.name}</td>
      <td className="py-1.5 font-mono text-xs text-muted-foreground">{token.token_prefix}…</td>
      <td className="py-1.5 text-xs text-muted-foreground hidden md:table-cell truncate max-w-[14rem]">{token.owner || "—"}</td>
      <td className="py-1.5 text-xs text-muted-foreground hidden lg:table-cell whitespace-nowrap">
        {formatDateTime(token.created_at)}
      </td>
      <td className="py-1.5 text-xs text-muted-foreground hidden lg:table-cell whitespace-nowrap">
        {isUnsetTime(token.last_used_at) ? "never" : formatDateTime(token.last_used_at)}
      </td>
      <td className="py-1.5 text-xs text-muted-foreground whitespace-nowrap">
        {isUnsetTime(token.expires_at) ? "never" : formatDateTime(token.expires_at)}
      </td>
      <td className="py-1.5">
        <span
          className={cn(
            // A token's lifecycle is configuration, not an incident: neutral
            // chips, the word carries it (Ledger: normal stays silent).
            "px-1.5 py-0.5 text-xs rounded border",
            status === "active" ? "border-foreground text-foreground" : "border-border text-muted-foreground"
          )}
        >
          {status}
        </span>
      </td>
      <td className="py-1.5 text-right">
        {status === "active" ? (
          <button
            onClick={() => { if (confirm(`Revoke token "${token.name}"? This cannot be undone.`)) onRevoke(token.id) }}
            disabled={revoking}
            className="inline-flex size-7 items-center justify-center rounded hover:bg-destructive/10 hover:text-destructive transition-colors disabled:opacity-50"
            title="Revoke token"
          >
            <Trash2 className="size-3.5" aria-hidden />
          </button>
        ) : (
          <span className="text-muted-foreground">—</span>
        )}
      </td>
    </tr>
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
  anomaly:         { label: "Traffic anomaly (baseline)", description: "Statistical baseline on per-link hourly throughput: fires when the current hour exceeds median + k·MAD of the same hour-of-week over the last 8 weeks. No fixed bps/pps threshold — tune with the sensitivity (k).", fields: [] },
}

// Anomaly rules carry sensitivity k as threshold_count = round(k*10) so the
// feature needs no schema change (e.g. k=2.5 threshold_count=25). These two
// helpers convert between the two representations for the rule editor.
const ANOMALY_DEFAULT_K = 2.5

function kFromThresholdCount(count?: number): number {
  return count && count > 0 ? count / 10 : ANOMALY_DEFAULT_K
}

function thresholdCountFromK(k: number): number {
  return Math.round(k * 10)
}

function RulesTab() {
  const queryClient = useQueryClient()
  const [showForm, setShowForm] = useState(false)
  const [draft, setDraft] = useState<Partial<AlertRule>>(() => emptyRule())

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
      <CardHeader>
        <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
          <CardTitle>Alert rules ({rules.length})</CardTitle>
          <button
            onClick={() => { setShowForm((s) => !s); if (!showForm) setDraft(emptyRule()) }}
            className="inline-flex items-center gap-1 px-2 py-0.5 text-[13px] font-medium rounded border border-input bg-background hover:bg-accent transition-colors"
          >
            <Plus className="size-3" />
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
                  onChange={(e) => {
                    const rt = e.target.value as AlertRule["rule_type"]
                    setDraft((d) => ({
                      ...d,
                      rule_type: rt,
                      threshold_bps: 0,
                      threshold_pps: 0,
                      // Anomaly rules seed threshold_count with the default k.
                      threshold_count: rt === "anomaly" ? thresholdCountFromK(ANOMALY_DEFAULT_K) : 0,
                    }))
                  }}
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
              <p className="text-xs text-muted-foreground italic px-1">{meta.description}</p>
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
            {meta && meta.fields.length > 0 && (
              <div className="grid gap-2 sm:grid-cols-2">
                {meta.fields.includes("bps") && (
                  <Field label={meta.fieldLabels?.bps || "Threshold bps"}>
                    <input
                      type="number"
                      min={0}
                      value={draft.threshold_bps || ""}
                      onChange={(e) => setDraft((d) => ({ ...d, threshold_bps: Number(e.target.value) }))}
                      placeholder="500000000"
                      className="flex-1 h-7 px-2 rounded border border-input bg-background text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring"
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
                      className="flex-1 h-7 px-2 rounded border border-input bg-background text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring"
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
                      className="flex-1 h-7 px-2 rounded border border-input bg-background text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring"
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
                  className="w-20 h-7 px-2 rounded border border-input bg-background text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring"
                />
                <span className="text-xs text-muted-foreground ml-1">/{draft.subnet_prefix_len || 24} IPv4 aggregation</span>
              </Field>
            )}
            {draft.rule_type === "anomaly" && (
              <>
                <Field label="Link filter">
                  <input
                    type="text"
                    value={draft.target_filter || ""}
                    onChange={(e) => setDraft((d) => ({ ...d, target_filter: e.target.value }))}
                    placeholder="(all links)"
                    className="flex-1 h-7 px-2 rounded border border-input bg-background text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring"
                  />
                </Field>
                <p className="text-xs text-muted-foreground italic px-1">
                  Optional — restrict evaluation to a single link tag. Empty evaluates every link.
                </p>
                <Field label="Sensitivity (k)">
                  <input
                    type="number"
                    min={0.5}
                    step={0.1}
                    value={kFromThresholdCount(draft.threshold_count)}
                    onChange={(e) => setDraft((d) => ({ ...d, threshold_count: thresholdCountFromK(Number(e.target.value)) }))}
                    className="w-24 h-7 px-2 rounded border border-input bg-background text-xs tabular-nums outline-none focus-visible:ring-1 focus-visible:ring-ring"
                  />
                  <span className="text-xs text-muted-foreground ml-1">higher = less sensitive</span>
                </Field>
                <p className="text-xs text-muted-foreground italic px-1">
                  Fires when the current hour&apos;s throughput exceeds{" "}
                  <span className="">median + k·MAD</span> of the same hour-of-week over the
                  last 8 weeks (median absolute deviation — robust to outliers). k=2.5 flags a moderate
                  spike; raise it to suppress noise, lower it to catch smaller deviations.
                </p>
              </>
            )}
            <HostgroupSelect value={draft.hostgroup_id} onChange={(id) => setDraft((d) => ({ ...d, hostgroup_id: id }))} />
            <div className="grid gap-2 sm:grid-cols-3">
              <Field label="Window (s)">
                <input
                  type="number"
                  min={10}
                  value={draft.window_seconds || 60}
                  onChange={(e) => setDraft((d) => ({ ...d, window_seconds: Number(e.target.value) }))}
                  className="flex-1 h-7 px-2 rounded border border-input bg-background text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring"
                />
              </Field>
              <Field label="Cooldown (s)">
                <input
                  type="number"
                  min={0}
                  value={draft.cooldown_seconds || 300}
                  onChange={(e) => setDraft((d) => ({ ...d, cooldown_seconds: Number(e.target.value) }))}
                  className="flex-1 h-7 px-2 rounded border border-input bg-background text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring"
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
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
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
                  <tr key={r.id} className="border-b border-border-soft last:border-0 hover:bg-accent/50">
                    <td className="py-1.5">
                      <button
                        onClick={() => toggleMutation.mutate(r)}
                        className={cn(
                          "px-1.5 py-0.5 text-xs rounded border font-medium",
                          r.enabled ? "border-success/40 bg-success/10 text-success" : "border-input bg-background text-muted-foreground"
                        )}
                      >
                        {r.enabled ? "ON" : "OFF"}
                      </button>
                    </td>
                    <td className="py-1.5 font-medium">
                      {r.name}
                      {r.description && <div className="text-xs text-muted-foreground font-normal">{r.description}</div>}
                    </td>
                    <td className="py-1.5 text-xs text-muted-foreground">{r.rule_type}</td>
                    <td className="py-1.5">
                      <span
                        className={cn(
                          "px-1.5 py-0.5 text-xs font-medium rounded border ",
                          r.severity === "critical" && "border-destructive/40 bg-destructive/10 text-destructive",
                          r.severity === "warning" && "border-warning/40 bg-warning/10 text-warning",
                          r.severity === "info" && "border-primary/40 bg-primary/10 text-primary"
                        )}
                      >
                        {r.severity}
                      </span>
                    </td>
                    <td className="py-1.5 text-right">
                      {formatThreshold(r)}
                    </td>
                    <td className="py-1.5 text-right text-muted-foreground">{r.window_seconds}s</td>
                    <td className="py-1.5 text-right">
                      <button
                        onClick={() => {
                          if (confirm(`Delete rule "${r.name}"?`)) {
                            deleteMutation.mutate(r.id)
                          }
                        }}
                        className="inline-flex size-7 items-center justify-center rounded hover:bg-destructive/10 hover:text-destructive transition-colors"
                        title="Delete rule"
                      >
                        <Trash2 className="size-3.5" aria-hidden />
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
  // Anomaly rules have no fixed threshold — threshold_count carries the
  // sensitivity k as k*10. Surface it as "k=N.N" rather than a raw count.
  if (r.rule_type === "anomaly") {
    return <span className="tabular-nums">k={kFromThresholdCount(r.threshold_count).toFixed(1)}</span>
  }
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
      {parts.map((p) => (
        <span key={p} className="leading-tight">{p}</span>
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
        <CardHeader>
          <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-2">
            <CardTitle>Notification webhooks ({webhooks.length})</CardTitle>
            <button
              onClick={() => setShowForm(!showForm)}
              className="inline-flex items-center gap-1 px-2 py-0.5 text-[13px] font-medium rounded border border-input bg-background hover:bg-accent transition-colors"
            >
              <Plus className="size-3" />
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
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-foreground">
                  <th className="pb-1.5 text-left text-[13px] font-normal text-muted-foreground">Name</th>
                  <th className="pb-1.5 text-left text-[13px] font-normal text-muted-foreground">Type</th>
                  <th className="pb-1.5 text-left text-[13px] font-normal text-muted-foreground">URL</th>
                  <th className="pb-1.5 text-left text-[13px] font-normal text-muted-foreground">Min severity</th>
                  <th className="pb-1.5 text-right text-[13px] font-normal text-muted-foreground">Actions</th>
                </tr>
              </thead>
              <tbody>
                {webhooks.map((w) => (
                  <tr key={w.id} className="border-b border-border-soft last:border-0 hover:bg-accent/50">
                    <td className="py-1.5 font-medium">{w.name}</td>
                    <td className="py-1.5 text-xs text-muted-foreground">{w.webhook_type}</td>
                    <td className="py-1.5 font-ip text-xs text-muted-foreground truncate max-w-xs">
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
                        className="inline-flex size-7 items-center justify-center rounded hover:bg-destructive/10 hover:text-destructive transition-colors"
                      >
                        <Trash2 className="size-3.5" aria-hidden />
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
// Reports tab — scheduled HTML+CSV email reports (Module D, FEATURE_REPORTS)
// =============================================================================

const REPORT_SECTIONS: { value: ReportSection; label: string }[] = [
  { value: "overview", label: "Overview" },
  { value: "top_as", label: "Top AS" },
  { value: "top_country", label: "Top countries" },
  { value: "capacity", label: "Capacity" },
  { value: "alerts", label: "Alerts" },
]

const DOW_LABELS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"]

interface ReportDraft {
  name: string
  frequency: ReportFrequency
  hour: number
  day_of_week: number
  day_of_month: number
  recipients: string
  sections: string[]
  format: ReportFormat
  enabled: boolean
}

function emptyReportDraft(): ReportDraft {
  return {
    name: "",
    frequency: "daily",
    hour: 6,
    day_of_week: 1,
    day_of_month: 1,
    recipients: "",
    sections: ["overview", "top_as"],
    format: "both",
    enabled: true,
  }
}

function parseSections(s: string): string[] {
  return s.split(",").map((x) => x.trim()).filter(Boolean)
}

function pad2(n: number): string {
  return String(n).padStart(2, "0")
}

function formatSchedule(s: ReportSchedule): string {
  const at = `${pad2(s.hour)}:00 UTC`
  switch (s.frequency) {
    case "weekly":
      return `Weekly · ${DOW_LABELS[s.day_of_week] ?? "?"} ${at}`
    case "monthly":
      return `Monthly · day ${s.day_of_month} ${at}`
    default:
      return `Daily · ${at}`
  }
}

function ReportsTab() {
  const [showForm, setShowForm] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [draft, setDraft] = useState<ReportDraft>(() => emptyReportDraft())

  const { data, isLoading, error, refetch } = useReportSchedules()
  const createMutation = useCreateReportSchedule()
  const updateMutation = useUpdateReportSchedule()

  const resetForm = () => {
    setShowForm(false)
    setEditingId(null)
    setDraft(emptyReportDraft())
  }

  const startEdit = (s: ReportSchedule) => {
    setEditingId(s.id)
    setDraft({
      name: s.name,
      frequency: s.frequency,
      hour: s.hour,
      day_of_week: s.day_of_week,
      day_of_month: s.day_of_month || 1,
      recipients: s.recipients,
      sections: parseSections(s.sections),
      format: s.format,
      enabled: s.enabled,
    })
    setShowForm(true)
  }

  if (error) return <ErrorDisplay error={error as Error} onRetry={() => refetch()} />

  const schedules: ReportSchedule[] = data?.data || []
  const isEditing = editingId !== null
  const isSaving = createMutation.isPending || updateMutation.isPending

  const toggleSection = (key: ReportSection) => {
    setDraft((d) => ({
      ...d,
      sections: d.sections.includes(key)
        ? d.sections.filter((s) => s !== key)
        : [...d.sections, key],
    }))
  }

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    const payload: Partial<ReportSchedule> = {
      name: draft.name,
      frequency: draft.frequency,
      hour: draft.hour,
      day_of_week: draft.frequency === "weekly" ? draft.day_of_week : 0,
      day_of_month: draft.frequency === "monthly" ? draft.day_of_month : 0,
      recipients: draft.recipients,
      sections: draft.sections.join(","),
      format: draft.format,
      enabled: draft.enabled,
    }
    if (isEditing) {
      updateMutation.mutate({ id: editingId, schedule: payload }, { onSuccess: resetForm })
    } else {
      createMutation.mutate(payload, { onSuccess: resetForm })
    }
  }

  return (
    <Card>
      <CardHeader>
        <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
          <CardTitle className="flex items-center gap-2">
            <Mail className="size-3.5" />
            Scheduled reports ({schedules.length})
          </CardTitle>
          <button
            onClick={() => (showForm ? resetForm() : setShowForm(true))}
            className="inline-flex items-center gap-1 px-2 py-0.5 text-[13px] font-medium rounded border border-input bg-background hover:bg-accent transition-colors"
          >
            <Plus className="size-3" />
            {showForm ? "Cancel" : "Add"}
          </button>
        </div>
      </CardHeader>
      <CardContent>
        {showForm && (
          <form onSubmit={handleSubmit} className="space-y-2 mb-4 p-3 border border-border rounded bg-muted/20">
            <div className="grid gap-2 sm:grid-cols-2">
              <Field label="Name">
                <input
                  type="text"
                  required
                  value={draft.name}
                  onChange={(e) => setDraft((d) => ({ ...d, name: e.target.value }))}
                  placeholder="Weekly NOC summary"
                  className="flex-1 h-7 px-2 rounded border border-input bg-background text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring"
                />
              </Field>
              <Field label="Format">
                <select
                  value={draft.format}
                  onChange={(e) => setDraft((d) => ({ ...d, format: e.target.value as ReportFormat }))}
                  className="flex-1 h-7 px-2 rounded border border-input bg-background text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring"
                >
                  <option value="html">HTML only</option>
                  <option value="csv">CSV only</option>
                  <option value="both">HTML + CSV</option>
                </select>
              </Field>
            </div>

            <div className="grid gap-2 sm:grid-cols-3">
              <Field label="Frequency">
                <select
                  value={draft.frequency}
                  onChange={(e) => setDraft((d) => ({ ...d, frequency: e.target.value as ReportFrequency }))}
                  className="flex-1 h-7 px-2 rounded border border-input bg-background text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring"
                >
                  <option value="daily">Daily</option>
                  <option value="weekly">Weekly</option>
                  <option value="monthly">Monthly</option>
                </select>
              </Field>
              <Field label="Hour (UTC)">
                <input
                  type="number"
                  min={0}
                  max={23}
                  required
                  value={draft.hour}
                  onChange={(e) => setDraft((d) => ({ ...d, hour: Number(e.target.value) }))}
                  className="flex-1 h-7 px-2 rounded border border-input bg-background text-xs tabular-nums outline-none focus-visible:ring-1 focus-visible:ring-ring"
                />
              </Field>
              {draft.frequency === "weekly" && (
                <Field label="Day of week">
                  <select
                    value={draft.day_of_week}
                    onChange={(e) => setDraft((d) => ({ ...d, day_of_week: Number(e.target.value) }))}
                    className="flex-1 h-7 px-2 rounded border border-input bg-background text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring"
                  >
                    {DOW_LABELS.map((label, i) => (
                      <option key={label} value={i}>{label}</option>
                    ))}
                  </select>
                </Field>
              )}
              {draft.frequency === "monthly" && (
                <Field label="Day of month">
                  <input
                    type="number"
                    min={1}
                    max={28}
                    value={draft.day_of_month}
                    onChange={(e) => setDraft((d) => ({ ...d, day_of_month: Number(e.target.value) }))}
                    className="flex-1 h-7 px-2 rounded border border-input bg-background text-xs tabular-nums outline-none focus-visible:ring-1 focus-visible:ring-ring"
                  />
                </Field>
              )}
            </div>

            <Field label="Recipients">
              <input
                type="text"
                required
                value={draft.recipients}
                onChange={(e) => setDraft((d) => ({ ...d, recipients: e.target.value }))}
                placeholder="noc@example.net, ops@example.net"
                className="flex-1 h-7 px-2 rounded border border-input bg-background text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring"
              />
            </Field>

            <div className="flex items-start gap-2">
              <span className="text-xs text-muted-foreground w-20 shrink-0 pt-1">Sections</span>
              <div className="flex flex-wrap gap-x-3 gap-y-1">
                {REPORT_SECTIONS.map((s) => (
                  <label key={s.value} className="inline-flex items-center gap-1.5 text-xs cursor-pointer">
                    <input
                      type="checkbox"
                      checked={draft.sections.includes(s.value)}
                      onChange={() => toggleSection(s.value)}
                      className="size-4 rounded border-input accent-primary"
                    />
                    {s.label}
                  </label>
                ))}
              </div>
            </div>

            <button
              type="submit"
              disabled={isSaving || draft.sections.length === 0}
              className="px-3 py-1 text-xs font-medium rounded bg-primary text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
            >
              {isEditing ? "Save" : "Create"}
            </button>
          </form>
        )}

        {isLoading ? (
          <TableSkeleton rows={3} cols={6} />
        ) : schedules.length === 0 ? (
          <EmptyState message="No report schedules configured" />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-foreground">
                  <th className="pb-1.5 text-left text-[13px] font-normal text-muted-foreground">Enabled</th>
                  <th className="pb-1.5 text-left text-[13px] font-normal text-muted-foreground">Name</th>
                  <th className="pb-1.5 text-left text-[13px] font-normal text-muted-foreground">Schedule</th>
                  <th className="pb-1.5 text-left text-[13px] font-normal text-muted-foreground hidden md:table-cell">Recipients</th>
                  <th className="pb-1.5 text-left text-[13px] font-normal text-muted-foreground hidden lg:table-cell">Sections</th>
                  <th className="pb-1.5 text-left text-[13px] font-normal text-muted-foreground">Format</th>
                  <th className="pb-1.5 text-right text-[13px] font-normal text-muted-foreground">Actions</th>
                </tr>
              </thead>
              <tbody>
                {schedules.map((s) => (
                  <ReportRow key={s.id} schedule={s} editing={editingId === s.id} onEdit={() => startEdit(s)} />
                ))}
              </tbody>
            </table>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function ReportRow({ schedule, editing, onEdit }: { schedule: ReportSchedule; editing: boolean; onEdit: () => void }) {
  const updateMutation = useUpdateReportSchedule()
  const deleteMutation = useDeleteReportSchedule()
  const testMutation = useTestReport()

  const toggleEnabled = () => {
    updateMutation.mutate({
      id: schedule.id,
      schedule: { ...schedule, enabled: !schedule.enabled },
    })
  }

  const sendTest = () => {
    testMutation.mutate(schedule.id, {
      onError: (err) => alert(`Test failed: ${(err as Error).message}`),
    })
  }

  return (
    <tr className={cn("border-b border-border-soft last:border-0 hover:bg-accent/50", editing && "bg-primary/5")}>
      <td className="py-1.5">
        <button
          onClick={toggleEnabled}
          disabled={updateMutation.isPending}
          aria-pressed={schedule.enabled}
          className={cn(
            "min-h-7 min-w-10 px-2 text-[13px] rounded border disabled:opacity-50 transition-colors",
            schedule.enabled ? "border-foreground text-foreground" : "border-input bg-background text-muted-foreground hover:text-foreground"
          )}
        >
          {schedule.enabled ? "on" : "off"}
        </button>
      </td>
      <td className="py-1.5 font-medium">{schedule.name}</td>
      <td className="py-1.5 text-xs text-muted-foreground whitespace-nowrap">{formatSchedule(schedule)}</td>
      <td className="py-1.5 font-ip text-xs text-muted-foreground hidden md:table-cell truncate max-w-[16rem]">
        {schedule.recipients}
      </td>
      <td className="py-1.5 text-xs text-muted-foreground hidden lg:table-cell">{schedule.sections}</td>
      <td className="py-1.5 text-xs text-muted-foreground">{schedule.format}</td>
      <td className="py-1.5 text-right">
        <div className="flex justify-end gap-0.5">
          <button
            onClick={sendTest}
            disabled={testMutation.isPending}
            className={cn(
              "p-1 rounded transition-colors disabled:opacity-50",
              testMutation.isSuccess ? "text-success" : "hover:bg-accent"
            )}
            title="Send test report now"
          >
            {testMutation.isSuccess ? <Check className="size-3" /> : <Send className="size-3" />}
          </button>
          <button
            onClick={onEdit}
            className="inline-flex size-7 items-center justify-center rounded hover:bg-accent transition-colors"
            title="Edit schedule"
          >
            <Pencil className="size-3.5" aria-hidden />
          </button>
          <button
            onClick={() => { if (confirm(`Delete report "${schedule.name}"?`)) deleteMutation.mutate(schedule.id) }}
            className="inline-flex size-7 items-center justify-center rounded hover:bg-destructive/10 hover:text-destructive transition-colors"
            title="Delete schedule"
          >
            <Trash2 className="size-3.5" aria-hidden />
          </button>
        </div>
      </td>
    </tr>
  )
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
      <CardHeader>
        <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-2">
          <CardTitle className="flex items-center gap-2">
            <FileText className="size-3.5" />
            Audit log (last 200 entries)
          </CardTitle>
          <span className="text-xs text-muted-foreground">365-day retention</span>
        </div>
      </CardHeader>
      <CardContent>
        {isLoading ? (
          <TableSkeleton rows={10} cols={6} />
        ) : entries.length === 0 ? (
          <EmptyState message="No audit entries yet" />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-foreground">
                  <th className="pb-1.5 text-left text-[13px] font-normal text-muted-foreground">Time</th>
                  <th className="pb-1.5 text-left text-[13px] font-normal text-muted-foreground">User</th>
                  <th className="pb-1.5 text-left text-[13px] font-normal text-muted-foreground">Action</th>
                  <th className="pb-1.5 text-left text-[13px] font-normal text-muted-foreground hidden md:table-cell">Resource</th>
                  <th className="pb-1.5 text-left text-[13px] font-normal text-muted-foreground hidden md:table-cell">Client IP</th>
                  <th className="pb-1.5 text-left text-[13px] font-normal text-muted-foreground">Result</th>
                </tr>
              </thead>
              <tbody>
                {entries.map((e) => (
                  <tr key={`${e.ts}-${e.action}-${e.resource}-${e.client_ip}`} className="border-b border-border-soft last:border-0 hover:bg-accent/50">
                    <td className="py-1 text-xs text-muted-foreground whitespace-nowrap">
                      {formatDateTime(e.ts)}
                    </td>
                    <td className="py-1">{e.user_email || <span className="text-muted-foreground">anonymous</span>}</td>
                    <td className="py-1 text-xs">{e.action}</td>
                    <td className="py-1 text-xs text-muted-foreground hidden md:table-cell">{e.resource}</td>
                    <td className="py-1 font-ip text-xs text-muted-foreground hidden md:table-cell">{e.client_ip}</td>
                    <td className="py-1">
                      <span
                        className={cn(
                          // Success is the normal outcome and stays unlit; a denied
                          // or failed action is what an auditor looks for.
                          "text-[13px]",
                          e.result === "success" && "text-muted-foreground",
                          e.result === "denied" && "rounded-sm bg-warning-muted px-1.5 font-semibold text-warning",
                          e.result === "error" && "rounded-sm bg-destructive px-1.5 font-semibold text-destructive-foreground"
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
    const cidrs = draft.cidrsText.split(/[\n,]+/).flatMap((s) => {
      const t = s.trim()
      return t ? [t] : []
    })
    const payload = { name: draft.name, description: draft.description, cidrs }
    if (isEditing) {
      updateMutation.mutate({ id: editingId, hg: payload })
    } else {
      createMutation.mutate(payload)
    }
  }

  return (
    <Card>
      <CardHeader>
        <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
          <CardTitle>Hostgroups ({groups.length})</CardTitle>
          <button
            onClick={() => showForm ? resetForm() : setShowForm(true)}
            className="inline-flex items-center gap-1 px-2 py-0.5 text-[13px] font-medium rounded border border-input bg-background hover:bg-accent transition-colors"
          >
            <Plus className="size-3" />
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
                className="flex-1 px-2 py-1 rounded border border-input bg-background text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring"
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
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-foreground">
                <th className="pb-1.5 text-left text-[13px] font-normal text-muted-foreground">Name</th>
                <th className="pb-1.5 text-left text-[13px] font-normal text-muted-foreground">CIDRs</th>
                <th className="pb-1.5 text-left text-[13px] font-normal text-muted-foreground hidden md:table-cell">Description</th>
                <th className="pb-1.5 text-right text-[13px] font-normal text-muted-foreground">Actions</th>
              </tr>
            </thead>
            <tbody>
              {groups.map((g) => (
                <tr key={g.id} className={cn("border-b border-border-soft last:border-0 hover:bg-accent/50", editingId === g.id && "bg-primary/5")}>
                  <td className="py-1.5 font-medium">{g.name}</td>
                  <td className="py-1.5 font-ip text-xs text-muted-foreground">{g.cidrs.join(", ")}</td>
                  <td className="py-1.5 text-muted-foreground hidden md:table-cell">{g.description || "—"}</td>
                  <td className="py-1.5 text-right">
                    <div className="flex justify-end gap-0.5">
                      <button
                        onClick={() => startEdit(g)}
                        className="inline-flex size-7 items-center justify-center rounded hover:bg-accent transition-colors"
                        title="Edit hostgroup"
                      >
                        <Pencil className="size-3.5" aria-hidden />
                      </button>
                      <button
                        onClick={() => { if (confirm(`Delete hostgroup "${g.name}"?`)) deleteMutation.mutate(g.id) }}
                        className="inline-flex size-7 items-center justify-center rounded hover:bg-destructive/10 hover:text-destructive transition-colors"
                        title="Delete hostgroup"
                      >
                        <Trash2 className="size-3.5" aria-hidden />
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
      <span className="text-xs text-muted-foreground w-20 shrink-0">{label}</span>
      {children}
    </label>
  )
}
