import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { useState } from "react"
import { Link } from "react-router-dom"
import { api } from "@/lib/api"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { ErrorDisplay, EmptyState } from "@/components/ui/error"
import { TableSkeleton } from "@/components/ui/skeleton"
import { IPWithPTR } from "@/components/PTR"
import { Shield, Plus, X } from "lucide-react"
import { cn } from "@/lib/utils"
import type { BGPBlock } from "@/lib/types"

const DURATIONS: { value: number; label: string }[] = [
  { value: 15, label: "15 min" },
  { value: 30, label: "30 min" },
  { value: 60, label: "1 hour" },
  { value: 240, label: "4 hours" },
  { value: 1440, label: "24 hours" },
]

export function BGP() {
  const queryClient = useQueryClient()

  // --- Auth: check if current user is admin ---
  const { data: userData } = useQuery({
    queryKey: ["auth-me"],
    queryFn: () => api.me(),
    staleTime: 300_000,
    retry: false,
  })
  const user = userData?.data
  const isAdmin = user?.role === "admin"

  // --- BGP session status ---
  const {
    data: statusData,
    isLoading: statusLoading,
    error: statusError,
    refetch: refetchStatus,
  } = useQuery({
    queryKey: ["bgp-status"],
    queryFn: () => api.bgpStatus(),
    refetchInterval: 10_000,
  })

  // --- Active blocks ---
  const {
    data: blocksData,
    isLoading: blocksLoading,
    error: blocksError,
    refetch: refetchBlocks,
  } = useQuery({
    queryKey: ["bgp-blocks"],
    queryFn: () => api.bgpBlocks(),
    refetchInterval: 15_000,
  })

  // --- Block history ---
  const {
    data: historyData,
    isLoading: historyLoading,
    error: historyError,
    refetch: refetchHistory,
  } = useQuery({
    queryKey: ["bgp-block-history"],
    queryFn: () => api.bgpBlockHistory(200),
  })

  // --- Manual block mutation ---
  const blockMutation = useMutation({
    mutationFn: ({ ip, duration, description }: { ip: string; duration: number; description: string }) =>
      api.bgpBlock(ip, duration, description),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["bgp-blocks"] })
      queryClient.invalidateQueries({ queryKey: ["bgp-block-history"] })
      queryClient.invalidateQueries({ queryKey: ["bgp-status"] })
      setShowForm(false)
      setFormIP("")
      setFormDuration(60)
      setFormDescription("")
    },
  })

  // --- Unblock mutation ---
  const unblockMutation = useMutation({
    mutationFn: ({ ip, description }: { ip: string; description?: string }) =>
      api.bgpUnblock(ip, description),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["bgp-blocks"] })
      queryClient.invalidateQueries({ queryKey: ["bgp-block-history"] })
      queryClient.invalidateQueries({ queryKey: ["bgp-status"] })
      setUnblockTarget(null)
      setUnblockReason("")
    },
  })

  // --- Form state ---
  const [showForm, setShowForm] = useState(false)
  const [formIP, setFormIP] = useState("")
  const [formDuration, setFormDuration] = useState(60)
  const [formDescription, setFormDescription] = useState("")

  // --- Unblock confirm state ---
  const [unblockTarget, setUnblockTarget] = useState<string | null>(null)
  const [unblockReason, setUnblockReason] = useState("")

  const session = statusData?.data
  const blocks: BGPBlock[] = blocksData?.data || []
  const history: BGPBlock[] = historyData?.data || []

  return (
    <div className="space-y-4 animate-fade-in">
      <div className="flex items-baseline justify-between">
        <h1 className="text-lg font-semibold tracking-tight flex items-center gap-2">
          <Shield className="h-4 w-4" />
          BGP Blocks
        </h1>
      </div>

      {/* Section 1: Session Status */}
      {statusError ? (
        <ErrorDisplay error={statusError as Error} onRetry={() => refetchStatus()} title="Failed to load BGP status" />
      ) : (
        <Card>
          <CardHeader className="pb-1">
            <div className="flex items-center justify-between">
              <CardTitle>BGP Session</CardTitle>
              {session && (
                <span
                  className={cn(
                    "px-1.5 py-0.5 text-[9px] font-medium rounded border uppercase tracking-wide",
                    session.state === "established"
                      ? "bg-success/20 text-success border-success/40"
                      : "bg-destructive/20 text-destructive border-destructive/40"
                  )}
                >
                  {session.state}
                </span>
              )}
            </div>
          </CardHeader>
          <CardContent>
            {statusLoading ? (
              <div className="flex gap-6">
                <div className="h-4 w-24 rounded bg-muted animate-shimmer" />
                <div className="h-4 w-24 rounded bg-muted animate-shimmer" />
                <div className="h-4 w-24 rounded bg-muted animate-shimmer" />
              </div>
            ) : session ? (
              <div className="flex flex-wrap gap-x-6 gap-y-1 text-xs">
                {session.peer_address && (
                  <div>
                    <span className="text-muted-foreground">Peer: </span>
                    <span className="font-mono text-[11px]">{session.peer_address}</span>
                  </div>
                )}
                {session.local_as != null && session.peer_as != null && (
                  <div>
                    <span className="text-muted-foreground">AS: </span>
                    <span className="font-mono text-[11px]">
                      {session.local_as} / {session.peer_as}
                    </span>
                  </div>
                )}
                <div>
                  <span className="text-muted-foreground">Uptime: </span>
                  <span className="font-mono text-[11px] tabular-nums">{formatUptime(session.uptime)}</span>
                </div>
                <div>
                  <span className="text-muted-foreground">Routes announced: </span>
                  <span className="font-mono text-[11px] tabular-nums">{session.routes_announced}</span>
                </div>
              </div>
            ) : (
              <p className="text-xs text-muted-foreground">No session data</p>
            )}
          </CardContent>
        </Card>
      )}

      {/* Section 2: Manual Block Form (admin only) */}
      {isAdmin && (
        <Card>
          <CardHeader className="pb-2">
            <div className="flex items-center justify-between">
              <CardTitle>Manual block</CardTitle>
              <button
                onClick={() => {
                  setShowForm((s) => !s)
                  if (!showForm) {
                    setFormIP("")
                    setFormDuration(60)
                    setFormDescription("")
                  }
                }}
                className="inline-flex items-center gap-1 px-2 py-0.5 text-[11px] font-medium rounded border border-input bg-muted/50 hover:bg-accent transition-colors"
              >
                <Plus className="h-3 w-3" />
                {showForm ? "Cancel" : "Block IP"}
              </button>
            </div>
          </CardHeader>
          <CardContent>
            {showForm && (
              <form
                onSubmit={(e) => {
                  e.preventDefault()
                  blockMutation.mutate({ ip: formIP.trim(), duration: formDuration, description: formDescription.trim() })
                }}
                className="space-y-2 p-3 border border-border rounded bg-muted/20"
              >
                <div className="grid gap-2 sm:grid-cols-2">
                  <div className="space-y-1">
                    <label className="text-[10px] font-medium text-muted-foreground uppercase tracking-wider">
                      IP address
                    </label>
                    <input
                      type="text"
                      required
                      value={formIP}
                      onChange={(e) => setFormIP(e.target.value)}
                      placeholder="192.0.2.1 or 2001:db8::1"
                      className="w-full h-7 px-2 rounded border border-input bg-background text-xs font-mono outline-none focus-visible:ring-1 focus-visible:ring-ring"
                    />
                  </div>
                  <div className="space-y-1">
                    <label className="text-[10px] font-medium text-muted-foreground uppercase tracking-wider">
                      Duration
                    </label>
                    <select
                      value={formDuration}
                      onChange={(e) => setFormDuration(Number(e.target.value))}
                      className="w-full h-7 px-2 rounded border border-input bg-background text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring"
                    >
                      {DURATIONS.map((d) => (
                        <option key={d.value} value={d.value}>
                          {d.label}
                        </option>
                      ))}
                    </select>
                  </div>
                </div>
                <div className="space-y-1">
                  <label className="text-[10px] font-medium text-muted-foreground uppercase tracking-wider">
                    Description
                  </label>
                  <textarea
                    rows={2}
                    value={formDescription}
                    onChange={(e) => setFormDescription(e.target.value)}
                    placeholder="Optional reason for the block"
                    className="w-full px-2 py-1.5 rounded border border-input bg-background text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring resize-none"
                  />
                </div>
                <div className="flex items-center gap-2">
                  <button
                    type="submit"
                    disabled={blockMutation.isPending}
                    className="px-3 py-1 text-xs font-medium rounded bg-primary text-primary-foreground hover:bg-primary/90 transition-colors disabled:opacity-50"
                  >
                    {blockMutation.isPending ? "Blocking..." : "Block IP"}
                  </button>
                  {blockMutation.isError && (
                    <span className="text-[10px] text-destructive">
                      {(blockMutation.error as Error).message}
                    </span>
                  )}
                </div>
              </form>
            )}
          </CardContent>
        </Card>
      )}

      {/* Section 3: Active Blocks Table */}
      {blocksError ? (
        <ErrorDisplay error={blocksError as Error} onRetry={() => refetchBlocks()} title="Failed to load active blocks" />
      ) : (
        <Card>
          <CardHeader className="pb-2">
            <CardTitle>Active blocks ({blocks.length})</CardTitle>
          </CardHeader>
          <CardContent>
            {blocksLoading ? (
              <TableSkeleton rows={5} cols={7} />
            ) : blocks.length === 0 ? (
              <EmptyState message="No active blocks" />
            ) : (
              <div className="overflow-x-auto -mx-4 px-4 sm:-mx-5 sm:px-5">
                <table className="w-full text-xs" role="table">
                  <thead>
                    <tr className="border-b border-border">
                      <th scope="col" className="pb-1.5 text-left font-medium text-muted-foreground">IP</th>
                      <th scope="col" className="pb-1.5 text-left font-medium text-muted-foreground">Reason</th>
                      <th scope="col" className="pb-1.5 text-left font-medium text-muted-foreground hidden sm:table-cell">Blocked By</th>
                      <th scope="col" className="pb-1.5 text-left font-medium text-muted-foreground hidden md:table-cell">Blocked At</th>
                      <th scope="col" className="pb-1.5 text-left font-medium text-muted-foreground hidden md:table-cell">Expires At</th>
                      <th scope="col" className="pb-1.5 text-left font-medium text-muted-foreground hidden lg:table-cell">Description</th>
                      <th scope="col" className="pb-1.5 text-right font-medium text-muted-foreground">Actions</th>
                    </tr>
                  </thead>
                  <tbody>
                    {blocks.map((b) => (
                      <tr key={b.id} className="border-b border-border/40 last:border-0 hover:bg-accent/50 transition-colors">
                        <td className="py-1.5">
                          <Link to={`/ip/${b.ip}`} className="text-primary hover:underline font-mono text-[11px]">
                            <IPWithPTR ip={b.ip} />
                          </Link>
                        </td>
                        <td className="py-1.5">
                          <ReasonBadge reason={b.reason} />
                        </td>
                        <td className="py-1.5 text-muted-foreground hidden sm:table-cell">{b.blocked_by}</td>
                        <td className="py-1.5 text-muted-foreground text-[10px] hidden md:table-cell">
                          {relativeTime(b.blocked_at)}
                        </td>
                        <td className="py-1.5 text-muted-foreground text-[10px] hidden md:table-cell">
                          {b.expires_at ? relativeTime(b.expires_at) : <span className="opacity-50">indefinite</span>}
                        </td>
                        <td className="py-1.5 text-muted-foreground text-[10px] max-w-[200px] truncate hidden lg:table-cell" title={b.description}>
                          {b.description || <span className="opacity-50">--</span>}
                        </td>
                        <td className="py-1.5 text-right">
                          {unblockTarget === b.ip ? (
                            <div className="inline-flex items-center gap-1.5">
                              <input
                                type="text"
                                value={unblockReason}
                                onChange={(e) => setUnblockReason(e.target.value)}
                                placeholder="Reason"
                                className="h-6 w-28 px-1.5 rounded border border-input bg-background text-[10px] outline-none focus-visible:ring-1 focus-visible:ring-ring"
                              />
                              <button
                                onClick={() => unblockMutation.mutate({ ip: b.ip, description: unblockReason || undefined })}
                                disabled={unblockMutation.isPending}
                                className="px-1.5 py-0.5 text-[10px] rounded border border-destructive/40 text-destructive bg-destructive/10 hover:bg-destructive/20 transition-colors"
                              >
                                {unblockMutation.isPending ? "..." : "Confirm"}
                              </button>
                              <button
                                onClick={() => { setUnblockTarget(null); setUnblockReason("") }}
                                className="px-1 py-0.5 text-[10px] rounded border border-input bg-muted/50 hover:bg-accent transition-colors"
                              >
                                <X className="h-2.5 w-2.5" />
                              </button>
                            </div>
                          ) : (
                            <button
                              onClick={() => setUnblockTarget(b.ip)}
                              className="px-1.5 py-0.5 text-[10px] rounded border border-destructive/40 text-destructive bg-destructive/10 hover:bg-destructive/20 transition-colors"
                            >
                              Unblock
                            </button>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </CardContent>
        </Card>
      )}

      {/* Section 4: Block History */}
      {historyError ? (
        <ErrorDisplay error={historyError as Error} onRetry={() => refetchHistory()} title="Failed to load block history" />
      ) : (
        <Card>
          <CardHeader className="pb-2">
            <CardTitle>History (last 200)</CardTitle>
          </CardHeader>
          <CardContent>
            {historyLoading ? (
              <TableSkeleton rows={8} cols={8} />
            ) : history.length === 0 ? (
              <EmptyState message="No block history" />
            ) : (
              <div className="overflow-x-auto -mx-4 px-4 sm:-mx-5 sm:px-5">
                <table className="w-full text-xs" role="table">
                  <thead>
                    <tr className="border-b border-border">
                      <th scope="col" className="pb-1.5 text-left font-medium text-muted-foreground">IP</th>
                      <th scope="col" className="pb-1.5 text-left font-medium text-muted-foreground">Reason</th>
                      <th scope="col" className="pb-1.5 text-left font-medium text-muted-foreground">Status</th>
                      <th scope="col" className="pb-1.5 text-left font-medium text-muted-foreground hidden sm:table-cell">Blocked By</th>
                      <th scope="col" className="pb-1.5 text-left font-medium text-muted-foreground hidden md:table-cell">Blocked At</th>
                      <th scope="col" className="pb-1.5 text-left font-medium text-muted-foreground hidden md:table-cell">Description</th>
                      <th scope="col" className="pb-1.5 text-left font-medium text-muted-foreground hidden lg:table-cell">Unblocked By</th>
                      <th scope="col" className="pb-1.5 text-left font-medium text-muted-foreground hidden lg:table-cell">Unblocked At</th>
                      <th scope="col" className="pb-1.5 text-left font-medium text-muted-foreground hidden lg:table-cell">Unblock Reason</th>
                    </tr>
                  </thead>
                  <tbody>
                    {history.map((b) => (
                      <tr
                        key={b.id}
                        className={cn(
                          "border-b border-border/40 last:border-0 transition-colors",
                          b.status === "withdrawn"
                            ? "text-muted-foreground/60 hover:bg-accent/30"
                            : "hover:bg-accent/50"
                        )}
                      >
                        <td className="py-1.5">
                          <Link to={`/ip/${b.ip}`} className="text-primary hover:underline font-mono text-[11px]">
                            <IPWithPTR ip={b.ip} />
                          </Link>
                        </td>
                        <td className="py-1.5">
                          <ReasonBadge reason={b.reason} />
                        </td>
                        <td className="py-1.5">
                          <StatusBadge status={b.status} />
                        </td>
                        <td className="py-1.5 hidden sm:table-cell">{b.blocked_by}</td>
                        <td className="py-1.5 text-[10px] hidden md:table-cell">
                          {relativeTime(b.blocked_at)}
                        </td>
                        <td className="py-1.5 text-[10px] max-w-[180px] truncate hidden md:table-cell" title={b.description}>
                          {b.description || <span className="opacity-50">--</span>}
                        </td>
                        <td className="py-1.5 hidden lg:table-cell">{b.unblocked_by || <span className="opacity-50">--</span>}</td>
                        <td className="py-1.5 text-[10px] hidden lg:table-cell">
                          {b.unblocked_at ? relativeTime(b.unblocked_at) : <span className="opacity-50">--</span>}
                        </td>
                        <td className="py-1.5 text-[10px] max-w-[160px] truncate hidden lg:table-cell" title={b.unblock_reason || undefined}>
                          {b.unblock_reason || <span className="opacity-50">--</span>}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </CardContent>
        </Card>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function ReasonBadge({ reason }: { reason: BGPBlock["reason"] }) {
  const styles =
    reason === "auto_block"
      ? "bg-success/20 text-success border-success/40"
      : "bg-primary/20 text-primary border-primary/40"
  const label = reason === "auto_block" ? "auto" : "manual"
  return (
    <span className={cn("px-1.5 py-0.5 text-[9px] font-medium rounded border uppercase tracking-wide", styles)}>
      {label}
    </span>
  )
}

function StatusBadge({ status }: { status: BGPBlock["status"] }) {
  const styles =
    status === "active"
      ? "bg-destructive/20 text-destructive border-destructive/40"
      : "bg-muted text-muted-foreground border-border"
  return (
    <span className={cn("px-1.5 py-0.5 text-[9px] font-medium rounded border uppercase tracking-wide", styles)}>
      {status}
    </span>
  )
}

function formatUptime(seconds: number): string {
  if (seconds <= 0) return "0m"
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  if (h > 0) return `${h}h ${m}m`
  return `${m}m`
}

function relativeTime(iso: string): string {
  const now = Date.now()
  const then = new Date(iso).getTime()
  const diff = now - then

  // Future timestamps
  if (diff < 0) {
    const absDiff = -diff
    if (absDiff < 60_000) return "in <1m"
    if (absDiff < 3600_000) return `in ${Math.round(absDiff / 60_000)}m`
    if (absDiff < 86400_000) return `in ${Math.round(absDiff / 3600_000)}h`
    return `in ${Math.round(absDiff / 86400_000)}d`
  }

  if (diff < 60_000) return "<1m ago"
  if (diff < 3600_000) return `${Math.round(diff / 60_000)}m ago`
  if (diff < 86400_000) return `${Math.round(diff / 3600_000)}h ago`
  return `${Math.round(diff / 86400_000)}d ago`
}
