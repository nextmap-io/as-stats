import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { useState, type ReactNode } from "react"
import { Link } from "react-router-dom"
import { api } from "@/lib/api"
import { useCurrentUser } from "@/hooks/useAuth"
import { useFeatures } from "@/hooks/useFeatures"
import { ErrorDisplay } from "@/components/ui/error"
import { TableSkeleton } from "@/components/ui/skeleton"
import { IPWithPTR } from "@/components/PTR"
import { BlockDialog } from "@/components/BlockDialog"
import { blockErrorMessage, blockSource, blockTarget, relativeTime } from "@/lib/block"
import { cn } from "@/lib/utils"
import type { BGPBlock, BGPSessionStatus } from "@/lib/types"

const HISTORY_LIMIT = 200

/**
 * BGP blackholes, ordered by the operator's questions: is the blackhole path
 * working, what is blocked right now (and until when), then how to block,
 * which rules apply, and what happened before.
 */
export function BGP() {
  const queryClient = useQueryClient()
  const { data: featuresRes } = useFeatures()
  const authEnabled = featuresRes?.data?.auth
  const { data: userData } = useCurrentUser(authEnabled)
  const isAdmin = authEnabled === false || userData?.data?.role === "admin"

  const status = useQuery({ queryKey: ["bgp-status"], queryFn: () => api.bgpStatus(), refetchInterval: 15_000 })
  const blocks = useQuery({ queryKey: ["bgp-blocks"], queryFn: () => api.bgpBlocks(), refetchInterval: 15_000 })
  const history = useQuery({ queryKey: ["bgp-block-history"], queryFn: () => api.bgpBlockHistory(HISTORY_LIMIT) })

  const invalidate = () => {
    for (const key of ["bgp-blocks", "bgp-block-history", "bgp-status"]) {
      queryClient.invalidateQueries({ queryKey: [key] })
    }
  }

  const check = useMutation({
    mutationFn: () => api.bgpStatus(true),
    onSuccess: (res) => queryClient.setQueryData(["bgp-status"], res),
  })

  // Block: the form only collects the target; the dialog states the impact.
  const [formTarget, setFormTarget] = useState("")
  const [reviewing, setReviewing] = useState<string | null>(null)
  const block = useMutation({
    mutationFn: ({ ip, duration, reason }: { ip: string; duration: number; reason: string }) => api.bgpBlock(ip, duration, reason),
    onSuccess: () => {
      setReviewing(null)
      setFormTarget("")
      invalidate()
    },
  })

  const [withdrawing, setWithdrawing] = useState<string | null>(null)
  const [withdrawReason, setWithdrawReason] = useState("")
  const unblock = useMutation({
    mutationFn: ({ ip, prefixLen, reason }: { ip: string; prefixLen?: number; reason?: string }) => api.bgpUnblock(ip, reason, prefixLen),
    onSuccess: () => {
      setWithdrawing(null)
      setWithdrawReason("")
      invalidate()
    },
  })
  const st = status.data?.data
  const active: BGPBlock[] = blocks.data?.data ?? []
  const past: BGPBlock[] = history.data?.data ?? []
  const orphans = st?.rib.orphans ?? []

  return (
    <div className="space-y-8">
      <div>
        <h1 className="text-2xl font-semibold">BGP blocks</h1>
        <p className="mt-1 max-w-[70ch] text-[13px] text-muted-foreground">
          RFC 7999 blackholes: a host route announced to your upstream{st?.peer_address ? <> through <span className="font-ip">{st.peer_address}</span></> : null}{" "}
          with {st?.communities?.length ? st.communities.join(", ") : "the BLACKHOLE community"}. Your providers then drop every packet to that
          address before it reaches you. Every block expires on its own.
        </p>
      </div>

      {status.error ? (
        <ErrorDisplay error={status.error as Error} onRetry={() => status.refetch()} title="Failed to load BGP status" />
      ) : (
        <RightNow st={st} activeCount={active.length} onCheck={() => check.mutate()} checking={check.isPending} />
      )}

      {st?.dry_run && (
        <p className="border-l-2 border-warning-mark pl-3 text-sm">
          <span className="font-semibold text-warning">Dry run.</span> BGP_DRY_RUN is on: blocks are checked and recorded, but no route is announced.
          Blocks recorded in dry run are never announced later.
        </p>
      )}

      {unblock.error && (
        <p role="alert" className="border-l-2 border-destructive py-1 pl-3 text-sm">
          <span className="font-semibold">Not withdrawn.</span> {blockErrorMessage(unblock.error)}
        </p>
      )}

      {/* Active blocks */}
      <Section
        title={`Active blocks (${active.length})`}
        help="Each block is withdrawn automatically when it expires, even across restarts. Withdraw ends it now: traffic flows again within seconds."
      >
        {blocks.error ? (
          <ErrorDisplay error={blocks.error as Error} onRetry={() => blocks.refetch()} title="Failed to load active blocks" />
        ) : blocks.isLoading ? (
          <TableSkeleton rows={3} cols={6} />
        ) : active.length === 0 ? (
          <p className="text-sm text-foreground-secondary">Nothing is blackholed.</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border text-left text-[13px] text-muted-foreground">
                  <th scope="col" className="pb-1.5 font-normal">Target</th>
                  <th scope="col" className="pb-1.5 font-normal">Why</th>
                  <th scope="col" className="hidden pb-1.5 font-normal sm:table-cell">By</th>
                  <th scope="col" className="hidden pb-1.5 font-normal md:table-cell">Since</th>
                  <th scope="col" className="pb-1.5 font-normal">Expires</th>
                  {isAdmin && <th scope="col" className="pb-1.5 text-right font-normal"><span className="sr-only">Actions</span></th>}
                </tr>
              </thead>
              <tbody>
                {active.map((b) => {
                  const target = blockTarget(b)
                  return (
                    <tr key={b.id} className="border-b border-border-soft align-top last:border-0">
                      <td className="py-2 pr-3">
                        <Link to={`/ip/${b.ip}`} className="font-ip text-[13px] text-link hover:underline">
                          <IPWithPTR ip={target} />
                        </Link>
                        {b.dry_run && <span className="ml-2 rounded border border-border px-1 text-[12px] text-muted-foreground">dry run</span>}
                      </td>
                      <td className="py-2 pr-3">
                        <BlockWhy b={b} />
                      </td>
                      <td className="hidden py-2 pr-3 text-foreground-secondary sm:table-cell">{b.blocked_by}</td>
                      <td className="hidden py-2 pr-3 text-foreground-secondary md:table-cell" title={new Date(b.blocked_at).toLocaleString()}>
                        {relativeTime(b.blocked_at)}
                      </td>
                      <td className="py-2 pr-3" title={b.expires_at ? new Date(b.expires_at).toLocaleString() : undefined}>
                        {b.expires_at ? (
                          <>
                            {relativeTime(b.expires_at)}
                            <span className="block text-[12px] text-muted-foreground">{new Date(b.expires_at).toLocaleTimeString()}</span>
                          </>
                        ) : (
                          <span className="text-muted-foreground">no expiry</span>
                        )}
                      </td>
                      {isAdmin && (
                        <td className="py-2 text-right">
                          {withdrawing === b.id ? (
                            <WithdrawConfirm
                              target={target}
                              reason={withdrawReason}
                              onReason={setWithdrawReason}
                              pending={unblock.isPending}
                              onConfirm={() => unblock.mutate({ ip: b.ip, prefixLen: b.prefix_len, reason: withdrawReason || undefined })}
                              onCancel={() => {
                                setWithdrawing(null)
                                unblock.reset()
                              }}
                            />
                          ) : (
                            <button
                              type="button"
                              onClick={() => {
                                unblock.reset()
                                setWithdrawReason("")
                                setWithdrawing(b.id)
                              }}
                              className="h-7 rounded border border-input bg-background px-2 text-[13px] hover:bg-accent"
                            >
                              Withdraw
                            </button>
                          )}
                        </td>
                      )}
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </Section>

      {/* Routes in the daemon nobody asked for */}
      {orphans.length > 0 && (
        <Section
          title="Routes without a block"
          help="The daemon announces these but as-stats has no active block for them (added by hand, or left by a crash). They are never withdrawn automatically."
        >
          <ul className="divide-y divide-border-soft border-y border-border-soft">
            {orphans.map((p) => {
              const [ip, len] = p.split("/")
              const host = ip.includes(":") ? 128 : 32
              const key = `orphan:${p}`
              return (
                <li key={p} className="flex flex-wrap items-center justify-between gap-2 py-2">
                  <span className="font-ip text-[13px]">
                    <span className="mr-2 rounded bg-warning-muted px-1 text-[12px] text-warning">Watch</span>
                    {p}
                  </span>
                  {isAdmin &&
                    (withdrawing === key ? (
                      <WithdrawConfirm
                        target={p}
                        reason={withdrawReason}
                        onReason={setWithdrawReason}
                        pending={unblock.isPending}
                        onConfirm={() => unblock.mutate({ ip, prefixLen: Number(len) < host ? Number(len) : undefined, reason: withdrawReason || undefined })}
                        onCancel={() => setWithdrawing(null)}
                      />
                    ) : (
                      <button
                        type="button"
                        onClick={() => setWithdrawing(key)}
                        className="h-7 rounded border border-input bg-background px-2 text-[13px] hover:bg-accent"
                      >
                        Withdraw
                      </button>
                    ))}
                </li>
              )
            })}
          </ul>
        </Section>
      )}

      {/* Block an address (admins) */}
      {isAdmin && (
        <Section
          title="Block an address"
          help={
            st
              ? `Only addresses inside the allow-list below, as host routes${st.min_prefix_len_v4 < 32 ? ` or prefixes up to /${st.min_prefix_len_v4}` : ""}. You confirm the impact and duration on the next step.`
              : "You confirm the impact and duration on the next step."
          }
        >
          <form
            className="flex flex-wrap items-end gap-2"
            onSubmit={(e) => {
              e.preventDefault()
              const t = formTarget.trim()
              if (t) {
                block.reset()
                setReviewing(t)
              }
            }}
          >
            <label className="space-y-1">
              <span className="block text-[13px] text-muted-foreground">Address</span>
              <input
                type="text"
                required
                value={formTarget}
                onChange={(e) => setFormTarget(e.target.value)}
                placeholder="198.51.100.10 or 2001:db8::10"
                className="font-ip h-8 w-72 max-w-full rounded border border-input bg-background px-2 text-sm"
              />
            </label>
            <button type="submit" disabled={!st?.ready} className="h-8 rounded border border-input bg-background px-3 text-sm hover:bg-accent disabled:opacity-50">
              Review block…
            </button>
          </form>
        </Section>
      )}

      {st && <SafetyRules st={st} />}

      {/* History */}
      <Section title="History" help={`The last ${HISTORY_LIMIT} blocks, newest first. A failed block never reached the upstream.`}>
        {history.error ? (
          <ErrorDisplay error={history.error as Error} onRetry={() => history.refetch()} title="Failed to load block history" />
        ) : history.isLoading ? (
          <TableSkeleton rows={5} cols={5} />
        ) : past.length === 0 ? (
          <p className="text-sm text-foreground-secondary">No block yet.</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border text-left text-[13px] text-muted-foreground">
                  <th scope="col" className="pb-1.5 font-normal">Target</th>
                  <th scope="col" className="pb-1.5 font-normal">Outcome</th>
                  <th scope="col" className="hidden pb-1.5 font-normal md:table-cell">Why</th>
                  <th scope="col" className="hidden pb-1.5 font-normal sm:table-cell">Blocked</th>
                  <th scope="col" className="hidden pb-1.5 font-normal lg:table-cell">Ended</th>
                </tr>
              </thead>
              <tbody>
                {past.map((b) => (
                  <tr key={b.id} className="border-b border-border-soft align-top last:border-0">
                    <td className="py-2 pr-3 font-ip text-[13px]">
                      <Link to={`/ip/${b.ip}`} className="text-link hover:underline">{blockTarget(b)}</Link>
                      {b.dry_run && <span className="ml-2 font-sans text-[12px] text-muted-foreground">dry run</span>}
                    </td>
                    <td className="py-2 pr-3">
                      <Outcome b={b} />
                    </td>
                    <td className="hidden py-2 pr-3 md:table-cell">
                      <BlockWhy b={b} />
                    </td>
                    <td className="hidden py-2 pr-3 text-foreground-secondary sm:table-cell" title={new Date(b.blocked_at).toLocaleString()}>
                      {new Date(b.blocked_at).toLocaleString()}
                      <span className="block text-[12px] text-muted-foreground">by {b.blocked_by}</span>
                    </td>
                    <td className="hidden py-2 text-foreground-secondary lg:table-cell">
                      {b.unblocked_at ? (
                        <>
                          {new Date(b.unblocked_at).toLocaleString()}
                          <span className="block text-[12px] text-muted-foreground">
                            {b.unblocked_by === "system" ? "" : `by ${b.unblocked_by}`}
                            {b.unblock_reason ? `${b.unblocked_by === "system" ? "" : " — "}${b.unblock_reason}` : ""}
                          </span>
                        </>
                      ) : (
                        <span className="text-muted-foreground">—</span>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Section>

      <BlockDialog
        target={reviewing}
        defaultReason=""
        subject="This address"
        pending={block.isPending}
        error={block.error}
        onConfirm={(duration, reason) => reviewing && block.mutate({ ip: reviewing, duration, reason })}
        onClose={() => {
          setReviewing(null)
          block.reset()
        }}
      />
    </div>
  )
}

// ---------------------------------------------------------------------------

function Section({ title, help, children }: { title: string; help: ReactNode; children: ReactNode }) {
  return (
    <section className="grid gap-x-8 gap-y-3 border-t border-foreground pt-3 md:grid-cols-[220px_1fr]">
      <div>
        <h2 className="text-[15px] font-semibold">{title}</h2>
        <p className="mt-1 text-[13px] text-muted-foreground">{help}</p>
      </div>
      <div className="min-w-0">{children}</div>
    </section>
  )
}

function Fact({ label, value, context, state }: { label: string; value: ReactNode; context: ReactNode; state?: "watch" | "incident" }) {
  return (
    <div className="min-w-0 py-1 sm:px-4 sm:first:pl-0">
      <div className="text-[13px] text-muted-foreground">{label}</div>
      <div className={cn("mt-0.5 flex flex-wrap items-baseline gap-2 text-[22px] leading-tight", state === "incident" && "text-destructive", state === "watch" && "text-warning")}>
        {value}
        {state === "incident" && <span className="rounded bg-destructive px-1.5 text-[12px] font-semibold text-destructive-foreground">Incident</span>}
        {state === "watch" && <span className="rounded bg-warning-muted px-1.5 text-[12px] font-semibold text-warning">Watch</span>}
      </div>
      <div className="mt-0.5 text-[13px] text-foreground-secondary">{context}</div>
    </div>
  )
}

function formatUptime(seconds: number): string {
  if (seconds <= 0) return ""
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  if (d > 0) return `up ${d} d ${h} h`
  if (h > 0) return `up ${h} h ${m} min`
  return `up ${m} min`
}

function RightNow({ st, activeCount, onCheck, checking }: { st?: BGPSessionStatus; activeCount: number; onCheck: () => void; checking: boolean }) {
  if (!st) return <div className="h-20 rounded bg-muted" aria-hidden />

  let session: { value: ReactNode; context: ReactNode; state?: "watch" | "incident" }
  if (!st.ready) {
    session = { value: "Not ready", state: "incident", context: `Active blocks not loaded from ClickHouse: ${st.load_error || "retrying"}. Blocking is refused until then.` }
  } else if (st.dry_run) {
    session = { value: "Dry run", state: "watch", context: "No command is run for new blocks." }
  } else if (st.state === "established") {
    session = { value: "Established", context: [st.peer_as ? `AS${st.peer_as}` : "", formatUptime(st.uptime)].filter(Boolean).join(" · ") || "Session up" }
  } else if (st.state === "unknown") {
    session = { value: "Unknown", context: "No status command (BGP_STATUS_CMD): the session is not checked." }
  } else {
    session = {
      value: st.state === "error" ? "Status failed" : `Session ${st.state}`,
      state: "incident",
      context: st.state_error || "Blackholes are not reaching your upstream.",
    }
  }

  const full = st.active_blocks >= st.max_active_blocks
  const autoFull = st.auto_blocks_per_hour > 0 && st.auto_blocks_last_hour >= st.auto_blocks_per_hour

  let rib: { value: ReactNode; context: ReactNode; state?: "watch" | "incident" }
  if (!st.rib.supported) {
    rib = { value: "Not checked", context: "No routes command (BGP_ROUTES_CMD)." }
  } else if (st.rib.error) {
    rib = { value: "Check failed", state: "incident", context: st.rib.error }
  } else if ((st.rib.missing?.length ?? 0) > 0) {
    rib = { value: `${st.rib.missing!.length} missing`, state: "incident", context: `Active but not announced: ${st.rib.missing!.join(", ")}` }
  } else if ((st.rib.orphans?.length ?? 0) > 0) {
    rib = { value: `${st.rib.orphans!.length} unknown`, state: "watch", context: "Routes in the daemon without a block (listed below)." }
  } else {
    rib = { value: "In sync", context: st.rib.checked_at ? `Checked ${relativeTime(st.rib.checked_at)}` : "Every active block is announced." }
  }

  return (
    <section aria-label="Right now" className="border-t-2 border-foreground pt-3">
      <div className="grid gap-y-3 sm:grid-cols-2 sm:divide-x sm:divide-border lg:grid-cols-4">
        <Fact label="Session" {...session} />
        <Fact
          label="Active blocks"
          value={<span>{activeCount}<span className="text-[15px] text-muted-foreground"> / {st.max_active_blocks}</span></span>}
          state={full ? "watch" : undefined}
          context={full ? "Limit reached: withdraw one to block another." : `${st.routes_announced} announced`}
        />
        <Fact
          label="Automatic blocks"
          value={<span>{st.auto_blocks_last_hour}<span className="text-[15px] text-muted-foreground"> / {st.auto_blocks_per_hour} per hour</span></span>}
          state={autoFull ? "watch" : undefined}
          context={st.auto_blocks_per_hour === 0 ? "Disabled (BGP_AUTO_BLOCK_MAX_PER_HOUR=0)." : autoFull ? "Further auto-blocks are refused this hour." : "From alert rules with action auto_block."}
        />
        <Fact label="Daemon routes" {...rib} />
      </div>
      <div className="mt-2">
        <button type="button" onClick={onCheck} disabled={checking} className="h-7 rounded border border-input bg-background px-2 text-[13px] hover:bg-accent disabled:opacity-50">
          {checking ? "Checking…" : "Check the daemon now"}
        </button>
      </div>
    </section>
  )
}

function BlockWhy({ b }: { b: BGPBlock }) {
  return (
    <div className="max-w-[40ch]">
      {b.reason === "manual" ? (
        <span>{blockSource(b)}</span>
      ) : (
        <Link to="/alerts" className="text-link hover:underline" title={b.alert_id ? `Alert ${b.alert_id}` : undefined}>
          {blockSource(b)}
        </Link>
      )}
      {b.description && <span className="block truncate text-[13px] text-muted-foreground" title={b.description}>{b.description}</span>}
    </div>
  )
}

function Outcome({ b }: { b: BGPBlock }) {
  if (b.status === "failed") return <span className="text-destructive">Failed</span>
  if (b.status === "active") return <span>Active</span>
  if (b.unblocked_by === "system") return <span className="text-foreground-secondary">Expired</span>
  return <span className="text-foreground-secondary">Withdrawn</span>
}

function WithdrawConfirm({
  target,
  reason,
  onReason,
  pending,
  onConfirm,
  onCancel,
}: {
  target: string
  reason: string
  onReason: (v: string) => void
  pending: boolean
  onConfirm: () => void
  onCancel: () => void
}) {
  return (
    <div className="inline-flex flex-col items-end gap-1.5 text-left" role="group" aria-label={`Withdraw ${target}`}>
      <span className="text-[13px]">Traffic to <span className="font-ip">{target}</span> flows again.</span>
      <div className="flex flex-wrap justify-end gap-1.5">
        <input
          type="text"
          value={reason}
          onChange={(e) => onReason(e.target.value)}
          placeholder="Reason (optional)"
          aria-label="Reason"
          className="h-7 w-40 rounded border border-input bg-background px-2 text-[13px]"
        />
        <button type="button" onClick={onConfirm} disabled={pending} className="h-7 rounded bg-primary px-2 text-[13px] font-semibold text-primary-foreground disabled:opacity-50">
          {pending ? "Withdrawing…" : "Withdraw"}
        </button>
        <button type="button" onClick={onCancel} className="h-7 rounded border border-input bg-background px-2 text-[13px] hover:bg-accent">
          Cancel
        </button>
      </div>
    </div>
  )
}

function SafetyRules({ st }: { st: BGPSessionStatus }) {
  const rows: [string, ReactNode][] = [
    [
      "Allowed space",
      st.allowed_prefix_count === 0 ? (
        <span className="text-destructive">None — every block is refused. Set BGP_ALLOWED_PREFIXES or LOCAL_AS.</span>
      ) : (
        <>
          <span className="font-ip">{(st.allowed_prefixes ?? []).join(", ")}</span>
          {st.allowed_prefix_count > (st.allowed_prefixes?.length ?? 0) && ` and ${st.allowed_prefix_count - (st.allowed_prefixes?.length ?? 0)} more`}
          <span className="block text-[13px] text-muted-foreground">from {st.allowed_source}</span>
        </>
      ),
    ],
    ["Never blocked", <span className="font-ip">{(st.protected_prefixes ?? []).join(", ") || "—"}</span>],
    ["Prefix length", `IPv4 /${st.min_prefix_len_v4}${st.min_prefix_len_v4 < 32 ? "–/32" : " only"}, IPv6 /${st.min_prefix_len_v6}${st.min_prefix_len_v6 < 128 ? "–/128" : " only"}${st.ipv6_enabled ? "" : " (IPv6 off: no BGP_NEXT_HOP_V6)"}`],
    ["Limits", `${st.max_active_blocks} active at most, ${st.auto_blocks_per_hour} automatic per hour`],
    ["Duration", `${st.default_duration_minutes} min by default, ${st.max_duration_minutes / 60} h at most`],
    ["Announced as", <span className="font-ip">{[st.communities.join(" "), st.next_hop_v4 && `next hop ${st.next_hop_v4}`, st.next_hop_v6 && st.next_hop_v6].filter(Boolean).join(", ")}</span>],
  ]
  return (
    <Section title="Safety rules" help="Checked on every block — manual, from an alert or automatic. Set by BGP_* variables on the API.">
      <dl className="grid gap-x-6 gap-y-2 text-sm sm:grid-cols-[160px_1fr]">
        {rows.map(([k, v]) => (
          <div key={k} className="contents">
            <dt className="text-muted-foreground">{k}</dt>
            <dd className="min-w-0 break-words">{v}</dd>
          </div>
        ))}
      </dl>
    </Section>
  )
}
