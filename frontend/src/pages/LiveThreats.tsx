import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Link, useSearchParams } from "react-router-dom"
import { api } from "@/lib/api"
import { blockErrorMessage } from "@/lib/block"
import { useFeatureFlags, useFeatures } from "@/hooks/useFeatures"
import { useCurrentUser } from "@/hooks/useAuth"
import { ErrorDisplay } from "@/components/ui/error"
import { Skeleton } from "@/components/ui/skeleton"
import { IPWithPTR } from "@/components/PTR"
import { ThreatSpark } from "@/components/threats/ThreatSpark"
import { formatClock } from "@/lib/format"
import { cn } from "@/lib/utils"
import {
  attentionSentence,
  deviatingAxis,
  evidence,
  explainThreat,
  formatBps,
  formatPps,
  normalizeThreats,
  quietSentence,
  ratioLabel,
  signatureLabel,
  usualLabel,
  windowPhrase,
} from "@/lib/threats"
import type { ThreatDirection, ThreatLevel, ThreatRow } from "@/lib/types"

const WINDOWS: { value: number; label: string }[] = [
  { value: 60, label: "1 min" },
  { value: 300, label: "5 min" },
  { value: 900, label: "15 min" },
  { value: 3600, label: "1 h" },
]
const DEFAULT_WINDOW = 300
const REFRESH_MS = 30_000
const BLOCK_MINUTES = 60

/**
 * Live Threats ranks hosts by how far they sit from their own usual level, not
 * by raw volume: a busy host at its usual level stays quiet. Inbound looks at
 * local destinations (attacks), outbound at local sources (compromised hosts).
 */
export function LiveThreats() {
  const [params, setParams] = useSearchParams()
  const dir: ThreatDirection = params.get("dir") === "out" ? "out" : "in"
  const windowParam = Number(params.get("window"))
  const windowSec = WINDOWS.some((w) => w.value === windowParam) ? windowParam : DEFAULT_WINDOW
  const showAll = params.get("all") === "1"

  const setParam = (key: string, value: string | null) => {
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        if (value === null) next.delete(key)
        else next.set(key, value)
        return next
      },
      { replace: true },
    )
  }

  const { data, isLoading, error, refetch, dataUpdatedAt } = useQuery({
    queryKey: ["live-threats", dir, windowSec, showAll],
    queryFn: () => api.liveThreats({ direction: dir, window: windowSec, limit: showAll ? 50 : 25, all: showAll }),
    refetchInterval: REFRESH_MS,
  })

  const features = useFeatureFlags()
  const { data: featuresRes } = useFeatures()
  const authEnabled = featuresRes?.data?.auth
  const { data: userData } = useCurrentUser(authEnabled)
  const isAdmin = authEnabled === false || userData?.data?.role === "admin"
  // Blackholing only makes sense for an attacked local destination.
  const canBlock = features.bgp && dir === "in" && isAdmin

  const queryClient = useQueryClient()
  const blockMutation = useMutation({
    mutationFn: ({ ip, reason }: { ip: string; reason: string }) => api.bgpBlock(ip, BLOCK_MINUTES, reason),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["bgp-blocks"] })
    },
  })

  const view = data ? normalizeThreats(data.data, { direction: dir, windowSeconds: windowSec, all: showAll }) : null
  const rows = view?.data.rows ?? []
  const flagged = rows.filter((r) => r.level !== "normal")
  const legacy = view?.legacy ?? false

  return (
    <div>
      <div className="flex flex-wrap items-baseline justify-between gap-x-6 gap-y-1">
        <h1 className="text-2xl font-semibold">Live threats</h1>
        <Link to="/alerts" className="text-[13px] text-foreground-secondary underline underline-offset-2 hover:text-foreground">
          Triggered alerts
        </Link>
      </div>
      <p className="mt-1 max-w-[62ch] text-[13px] text-muted-foreground">
        Each host is compared with its own usual level at this hour over the last 7 days. The dashed line on each chart
        is that usual peak (p95); a host that is busy but at its usual level stays quiet.
      </p>

      <div className="mt-5 flex flex-wrap items-center gap-x-5 gap-y-3 text-[13px]">
        <Segmented
          label="Direction"
          value={dir}
          options={[
            { value: "in", label: "Inbound" },
            { value: "out", label: "Outbound" },
          ]}
          onChange={(v) => setParam("dir", v === "in" ? null : v)}
        />
        <Segmented
          label="Window"
          value={String(windowSec)}
          options={WINDOWS.map((w) => ({ value: String(w.value), label: w.label }))}
          onChange={(v) => setParam("window", Number(v) === DEFAULT_WINDOW ? null : v)}
        />
        <label className="inline-flex cursor-pointer items-center gap-2 text-foreground-secondary">
          <input
            type="checkbox"
            checked={showAll}
            onChange={(e) => setParam("all", e.target.checked ? "1" : null)}
            className="size-4 accent-[var(--color-ring)]"
          />
          Show all busy hosts
        </label>
        <span className="text-muted-foreground sm:ml-auto">
          {dataUpdatedAt ? `Updated ${formatClock(dataUpdatedAt)}, ` : ""}refreshed every 30{" "}s
        </span>
      </div>

      <section aria-labelledby="threats-list" className="mt-4 border-t-2 border-foreground pt-4">
        <h2 id="threats-list" className="sr-only">
          {dir === "in" ? "Inbound" : "Outbound"} hosts
        </h2>

        {error ? (
          <ErrorDisplay error={error as Error} onRetry={() => refetch()} />
        ) : isLoading || !view ? (
          <div className="space-y-3" aria-label="Loading">
            <Skeleton className="h-5 w-2/3" />
            <Skeleton className="h-16 w-full" />
            <Skeleton className="h-16 w-full" />
          </div>
        ) : (
          <>
            {flagged.length === 0 ? (
              <p className="text-[15px]">{quietSentence(view.data.checked, windowSec)}</p>
            ) : (
              <p className="text-[15px]">{attentionSentence(flagged.length, view.data.checked, view.data.normal)}</p>
            )}

            {legacy && (
              <p className="mt-1 text-[13px] text-muted-foreground">
                This server does not report usual levels yet: hosts are flagged by how close they are to an alert rule
                {dir === "out" ? ", and only inbound traffic is shown" : ""}.
              </p>
            )}

            {blockMutation.error && (
              <div role="alert" className="mt-3 border-l-2 border-destructive py-1 pl-3 text-sm">
                <span className="font-semibold">Not blocked.</span> {blockErrorMessage(blockMutation.error, "This host")}
              </div>
            )}
            {blockMutation.isSuccess && (
              <p role="status" className="mt-3 text-sm text-foreground-secondary">
                Blackhole announced for {blockMutation.variables?.ip} for {BLOCK_MINUTES} minutes. See{" "}
                <Link to="/bgp" className="underline underline-offset-2">
                  BGP
                </Link>
                .
              </p>
            )}

            {rows.length > 0 && (
              <ul className="mt-4 border-t border-border">
                {rows.map((r) => (
                  <ThreatItem
                    key={r.ip}
                    row={r}
                    dir={legacy ? "in" : dir}
                    legacy={legacy}
                    windowSec={windowSec}
                    flowSearch={features.flow_search}
                    canBlock={canBlock && r.level !== "normal"}
                    blockPending={blockMutation.isPending}
                    onBlock={(reason) => {
                      if (confirm(`Blackhole ${r.ip} through BGP for ${BLOCK_MINUTES} minutes?`)) {
                        blockMutation.mutate({ ip: r.ip, reason })
                      }
                    }}
                  />
                ))}
              </ul>
            )}
          </>
        )}
      </section>
    </div>
  )
}

function Segmented({
  label,
  value,
  options,
  onChange,
}: {
  label: string
  value: string
  options: { value: string; label: string }[]
  onChange: (v: string) => void
}) {
  return (
    <div className="inline-flex rounded bg-muted p-0.5" role="group" aria-label={label}>
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          aria-pressed={value === o.value}
          onClick={() => onChange(o.value)}
          className={cn(
            "rounded-[3px] px-2.5 py-1 transition-colors duration-100",
            value === o.value ? "bg-card font-semibold text-foreground" : "text-foreground-secondary hover:text-foreground",
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  )
}

function LevelBadge({ level }: { level: ThreatLevel }) {
  if (level === "normal") return null
  return (
    <span
      className={cn(
        "rounded-sm px-1.5 text-xs font-semibold leading-5",
        level === "watch" ? "bg-warning-muted text-warning" : "bg-destructive text-destructive-foreground",
      )}
    >
      {level === "watch" ? "Watch" : "Incident"}
    </span>
  )
}

function ThreatItem({
  row: r,
  dir,
  legacy,
  windowSec,
  flowSearch,
  canBlock,
  blockPending,
  onBlock,
}: {
  row: ThreatRow
  dir: ThreatDirection
  legacy: boolean
  windowSec: number
  flowSearch: boolean
  canBlock: boolean
  blockPending: boolean
  onBlock: (reason: string) => void
}) {
  const quiet = r.level === "normal"
  const axis = deviatingAxis(r)
  const hasHistory = r.baseline.samples > 0
  const sentence = explainThreat(r, dir, legacy)
  const facts = evidence(r, dir)
  const since = r.since ? Date.parse(r.since) : NaN
  const ratio = hasHistory ? ratioLabel(r.ratio) : ""

  const flowHref = (() => {
    const to = new Date()
    const from = new Date(to.getTime() - windowSec * 1000)
    const q = new URLSearchParams({ [dir === "in" ? "dst_ip" : "src_ip"]: r.ip, from: from.toISOString(), to: to.toISOString() })
    return `/flows?${q.toString()}`
  })()

  const figure = (kind: "bps" | "pps") => {
    const fmt = kind === "bps" ? formatBps : formatPps
    const now = kind === "bps" ? r.now.bps : r.now.pps
    const p50 = kind === "bps" ? r.baseline.bps_p50 : r.baseline.pps_p50
    const p95 = kind === "bps" ? r.baseline.bps_p95 : r.baseline.pps_p95
    return (
      <div className="leading-snug">
        <span className={cn("font-semibold", quiet ? "text-foreground-secondary" : "text-foreground")}>{fmt(now)}</span>
        {kind === axis && ratio && (
          <span
            className={cn(
              "ml-2 font-semibold",
              r.level === "incident" && "text-destructive",
              r.level === "watch" && "text-warning",
            )}
          >
            {ratio}
          </span>
        )}
        {!legacy && (
          <span className="block text-[13px] text-muted-foreground">
            {hasHistory ? usualLabel(p50, p95, fmt) : "no history"}
          </span>
        )}
      </div>
    )
  }

  return (
    <li className="grid grid-cols-2 gap-x-6 gap-y-2.5 border-b border-border py-4 lg:grid-cols-[minmax(0,1fr)_190px_220px] lg:gap-x-8">
      {/* Who and what. */}
      <div className="col-span-2 flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1.5">
        <LevelBadge level={r.level} />
        <Link
          to={`/ip/${encodeURIComponent(r.ip)}`}
          className={cn("min-w-0 max-w-full font-ip text-[15px] hover:underline", quiet ? "font-medium" : "font-semibold")}
        >
          <IPWithPTR ip={r.ip} />
        </Link>
        {r.signatures.map((s) => (
          <span key={s.kind} className="rounded-sm border border-border px-1.5 text-xs leading-5 text-foreground">
            {signatureLabel(s)}
          </span>
        ))}
      </div>

      {/* Actions: last on phones, top right on wide screens. */}
      <div className="order-last col-span-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-[13px] lg:order-none lg:col-span-1 lg:justify-end">
        <Link to={`/ip/${encodeURIComponent(r.ip)}`} className="text-foreground-secondary underline underline-offset-2 hover:text-foreground">
          IP page
        </Link>
        {flowSearch && (
          <Link to={flowHref} className="text-foreground-secondary underline underline-offset-2 hover:text-foreground">
            Flow search
          </Link>
        )}
        {canBlock && (
          <button
            type="button"
            onClick={() => onBlock(`Live threats: ${sentence}`)}
            disabled={blockPending}
            className="rounded border border-destructive px-1.5 py-0.5 text-xs text-destructive hover:bg-destructive hover:text-destructive-foreground disabled:opacity-50"
            title="Blackhole this address through BGP (admins only)"
          >
            Block
          </button>
        )}
      </div>

      {/* Why it is flagged. */}
      <div className="col-span-2 min-w-0 lg:col-span-1">
        <p className={cn(quiet ? "text-foreground-secondary" : "text-foreground")}>{sentence}</p>
        {facts.length > 0 && <p className="mt-0.5 text-[13px] text-muted-foreground">{facts.join(" · ")}</p>}
        {r.closest_rule && r.rule_pct > 0 && (
          <p className="mt-0.5 text-[13px] text-muted-foreground">
            Closest rule: {r.closest_rule}, at{" "}
            <span
              className={cn(
                !quiet && r.rule_pct >= 100 && "font-semibold text-destructive",
                !quiet && r.rule_pct >= 50 && r.rule_pct < 100 && "font-semibold text-warning",
              )}
            >
              {Math.round(r.rule_pct)}%
            </span>{" "}
            of its threshold
          </p>
        )}
      </div>

      {/* Now against usual. */}
      <div className="min-w-0 space-y-1.5">
        {figure("bps")}
        {((axis === "pps" && !quiet) || legacy) && figure("pps")}
      </div>

      {/* Last hour. */}
      <div className="min-w-0">
        {r.series.length > 1 ? (
          <>
            <ThreatSpark
              series={r.series}
              axis={axis}
              p95={axis === "pps" ? r.baseline.pps_p95 : r.baseline.bps_p95}
              since={r.since}
              level={r.level}
              dir={dir}
              label={`Last 60 minutes of ${axis === "pps" ? "packets" : "traffic"}${hasHistory ? ", dashed line at its usual peak" : ""}`}
            />
            <p className="mt-1 flex justify-between text-xs text-muted-foreground">
              <span>last 60 min</span>
              {!Number.isNaN(since) && <span className={cn(!quiet && "text-foreground-secondary")}>since {formatClock(since)}</span>}
            </p>
          </>
        ) : (
          <p className="text-[13px] text-muted-foreground">
            {legacy ? `Snapshot over the last ${windowPhrase(windowSec)}` : "No minute series"}
          </p>
        )}
      </div>
    </li>
  )
}
