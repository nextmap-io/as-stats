import { Link } from "react-router-dom"
import { useActiveAlerts } from "@/hooks/useApi"
import { hostTarget } from "@/lib/home"
import { formatAlertMetric, formatClock } from "@/lib/format"
import { cn } from "@/lib/utils"

/**
 * HostAlerts keeps the incident in view when an operator drills from Alerts,
 * Live threats or the home strip into an IP: one line per active alert on this
 * host, with the same words and colours as the home "Right now" strip. Renders
 * nothing when the host is quiet (normal stays silent).
 */
export function HostAlerts({ ip, enabled }: { ip: string; enabled: boolean }) {
  const { data } = useActiveAlerts(enabled, 200)
  const mine = (data?.data ?? []).filter((a) => hostTarget(a) === ip)
  if (!mine.length) return null
  return (
    <ul className="space-y-1 border-t border-foreground pt-3 text-sm text-foreground-secondary" aria-label="Active alerts on this host">
      {mine.map((a) => {
        const incident = a.severity === "critical"
        return (
          <li key={a.id} className="flex flex-wrap items-baseline gap-x-2">
            <span
              className={cn(
                "rounded-sm px-1.5 text-xs font-semibold leading-5",
                incident
                  ? "bg-destructive text-destructive-foreground"
                  : a.severity === "warning"
                    ? "bg-warning-muted text-warning"
                    : "border border-border text-foreground-secondary",
              )}
            >
              {incident ? "Incident" : a.severity === "warning" ? "Watch" : "Info"}
            </span>
            <span>
              <span className="font-medium text-foreground">{a.rule_name}</span>:{" "}
              {formatAlertMetric(a.metric_value, a.metric_type)} against a threshold of{" "}
              {formatAlertMetric(a.threshold, a.metric_type)}, since {formatClock(Date.parse(a.triggered_at))}
              {a.status === "acknowledged" ? ", acknowledged" : ""}.{" "}
              <Link to="/alerts" className="underline underline-offset-2">
                Open Alerts
              </Link>
              {" · "}
              {/* Live threats explains the attack (sources, ports, usual level). */}
              <Link to="/live" className="underline underline-offset-2">
                Live threats
              </Link>
            </span>
          </li>
        )
      })}
    </ul>
  )
}
