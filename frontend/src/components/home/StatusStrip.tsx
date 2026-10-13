import type { ReactNode } from "react"
import { Link } from "react-router-dom"
import { useActiveAlerts, useAlertsSummary, useLiveStatus } from "@/hooks/useApi"
import { useFeatureFlags } from "@/hooks/useFeatures"
import {
  alertTargetLabel,
  alertsLevel,
  diskLevel,
  hostTarget,
  ingestionLevel,
  topAlert,
  utilLevel,
  worst,
  type Level,
  type LinkRowData,
} from "@/lib/home"
import { flowsPerSecond, ageSeconds, formatAge } from "@/lib/status"
import { formatBitRate } from "@/lib/rate"
import { Skeleton } from "@/components/ui/skeleton"
import { cn } from "@/lib/utils"
import { formatAlertMetric, formatCapacity, formatClock, formatCount, formatPct } from "@/lib/format"

/**
 * StatusStrip answers "is anything wrong?". It follows the dark-cockpit rule:
 * a normal fact stays unlit (plain text, no colour); only an abnormal one gets
 * colour AND a word ("Watch", "Incident"), so colour is never the only signal.
 */
export function StatusStrip({ links, linksLoading }: { links: LinkRowData[]; linksLoading: boolean }) {
  const features = useFeatureFlags()

  return (
    <div
      className={cn(
        "grid grid-cols-1 gap-y-5 md:gap-y-0 md:divide-x md:divide-border",
        features.alerts ? "md:grid-cols-[1.35fr_1fr_1fr]" : "md:grid-cols-2",
      )}
    >
      {features.alerts && <AlertsFact />}
      <LinkLoadFact links={links} loading={linksLoading} />
      <IngestionFact />
    </div>
  )
}

function Fact({
  label,
  level,
  value,
  children,
}: {
  label: string
  level: Level
  value: ReactNode
  children?: ReactNode
}) {
  return (
    <div className="min-w-0 border-t border-border pt-4 first:border-t-0 first:pt-0 md:border-t-0 md:px-6 md:pt-0 md:first:pl-0">
      <div className="flex items-center gap-2 text-[13px] text-muted-foreground">
        {label}
        {level !== "normal" && (
          <span
            className={cn(
              "rounded-sm px-1.5 text-xs font-semibold leading-5",
              level === "watch" ? "bg-warning-muted text-warning" : "bg-destructive text-destructive-foreground",
            )}
          >
            {level === "watch" ? "Watch" : "Incident"}
          </span>
        )}
      </div>
      <div
        className={cn(
          "mt-1 text-[22px] font-semibold leading-tight",
          level === "watch" && "text-warning",
          level === "critical" && "text-destructive",
        )}
        style={{ fontStretch: "108%" }}
      >
        {value}
      </div>
      {children && <div className="mt-1.5 text-sm leading-relaxed text-foreground-secondary">{children}</div>}
    </div>
  )
}

function FactSkeleton({ label }: { label: string }) {
  return (
    <div className="min-w-0 border-t border-border pt-4 first:border-t-0 first:pt-0 md:border-t-0 md:px-6 md:pt-0 md:first:pl-0">
      <div className="text-[13px] text-muted-foreground">{label}</div>
      <Skeleton className="mt-2 h-6 w-40" />
      <Skeleton className="mt-2 h-4 w-56" />
    </div>
  )
}

function AlertsFact() {
  const summary = useAlertsSummary(true)
  const active = useActiveAlerts(true)
  if (summary.isLoading) return <FactSkeleton label="Alerts" />
  if (summary.isError || !summary.data) {
    return (
      <Fact label="Alerts" level="watch" value="Unknown">
        The alert summary could not be loaded. <Link to="/alerts" className="underline underline-offset-2">Open Alerts</Link>
      </Fact>
    )
  }
  const by = summary.data.data.by_severity ?? {}
  const total = summary.data.data.total
  const critical = by.critical ?? 0
  const level = alertsLevel(by)
  const top = topAlert(active.data?.data ?? [])
  const value =
    total === 0 ? "No active alerts" : `${formatCount(total)} active, ${critical > 0 ? `${critical} critical` : "none critical"}`

  return (
    <Fact label="Alerts" level={level} value={value}>
      {top ? <AlertLine alert={top} /> : total === 0 ? "Nothing needs a person right now." : null}
    </Fact>
  )
}

function AlertLine({ alert }: { alert: NonNullable<ReturnType<typeof topAlert>> }) {
  const host = hostTarget(alert)
  const target = alertTargetLabel(alert)
  return (
    <>
      <span className="mr-1.5 inline-block rounded-sm border border-border px-1.5 text-xs capitalize leading-5 text-foreground-secondary">
        {alert.severity}
      </span>
      {alert.rule_name} on{" "}
      {host ? (
        <Link to={`/ip/${encodeURIComponent(host)}`} className="font-ip font-medium text-foreground underline underline-offset-2">
          {host}
        </Link>
      ) : (
        <span className="font-medium text-foreground">{target}</span>
      )}
      : {formatAlertMetric(alert.metric_value, alert.metric_type)} against a threshold of{" "}
      {formatAlertMetric(alert.threshold, alert.metric_type)}, since {formatClock(Date.parse(alert.triggered_at))}.
    </>
  )
}

function LinkLoadFact({ links, loading }: { links: LinkRowData[]; loading: boolean }) {
  if (loading) return <FactSkeleton label="Link load" />
  const withCap = links.filter((l) => l.peakPct !== null)
  if (!withCap.length) {
    return (
      <Fact label="Link load" level="normal" value="No capacities set">
        Set link capacities in <Link to="/links" className="underline underline-offset-2">Links</Link> to see utilisation.
      </Fact>
    )
  }
  const peak = withCap.reduce((a, b) => ((b.peakPct ?? 0) > (a.peakPct ?? 0) ? b : a))
  const busiest = withCap.reduce((a, b) => ((b.nowPct ?? 0) > (a.nowPct ?? 0) ? b : a))
  const level = worst(utilLevel(peak.peakPct), utilLevel(busiest.nowPct))
  return (
    <Fact label="Link load" level={level} value={`${formatPct(peak.peakPct)} peak`}>
      <Link to={`/link/${encodeURIComponent(peak.tag)}`} className="font-medium text-foreground hover:underline">
        {peak.name}
      </Link>{" "}
      peaked at {formatBitRate(Math.max(peak.peakIn, peak.peakOut))} of {formatCapacity(peak.capacityBps)}. Busiest link
      now: {busiest.name}, {formatPct(busiest.nowPct)}.
    </Fact>
  )
}

function IngestionFact() {
  const { data, isLoading, isError, dataUpdatedAt } = useLiveStatus()
  if (isLoading) return <FactSkeleton label="Ingestion" />
  if (isError || !data) {
    return (
      <Fact label="Ingestion" level="critical" value="Status unavailable">
        The collector status could not be loaded. <Link to="/status" className="underline underline-offset-2">Open Status</Link>
      </Fact>
    )
  }
  const ok = data.routers.filter((r) => r.state === "ok").length
  const lastSeen = data.routers.map((r) => Date.parse(r.last_seen)).filter((t) => !Number.isNaN(t))
  const newest = lastSeen.length ? new Date(Math.max(...lastSeen)).toISOString() : ""
  const level = worst(ingestionLevel(data), diskLevel(data.disk_used_pct))
  const silent = data.routers.filter((r) => r.state !== "ok")
  return (
    <Fact label="Ingestion" level={level} value={`${formatCount(flowsPerSecond(data))} flows/s`}>
      {ok} of {data.routers.length} router{data.routers.length === 1 ? "" : "s"} reporting, last flow{" "}
      {formatAge(ageSeconds(newest, dataUpdatedAt))}.
      {silent.length > 0 && (
        <>
          {" "}
          Not reporting: <span className="font-ip text-foreground">{silent.map((r) => r.router_ip).join(", ")}</span>.
        </>
      )}
      {data.disk_used_pct !== null && data.disk_used_pct !== undefined && (
        <> ClickHouse disk at {formatPct(data.disk_used_pct)}.</>
      )}
    </Fact>
  )
}
