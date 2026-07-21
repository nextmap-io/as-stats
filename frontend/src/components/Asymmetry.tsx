import { cn, formatBytes } from "@/lib/utils"
import type { AsymmetryClass } from "@/lib/types"

/**
 * In/out asymmetry primitives (F2).
 *
 * `RatioBar` shows the split between inbound (received) and outbound (sent)
 * bytes as a two-segment bar; `AsymmetryBadge` labels the coarse traffic class
 * (eyeball / content / balanced). Colors reuse the shared traffic-in /
 * traffic-out tokens so the meaning is consistent with the charts.
 */

const CLASS_STYLES: Record<AsymmetryClass, string> = {
  eyeball: "border-traffic-in/40 text-traffic-in bg-traffic-in/10",
  content: "border-traffic-out/40 text-traffic-out bg-traffic-out/10",
  balanced: "border-border text-muted-foreground bg-muted/40",
}

const CLASS_TITLES: Record<AsymmetryClass, string> = {
  eyeball: "Eyeball — mostly inbound (access / broadband)",
  content: "Content — mostly outbound (hosting / CDN)",
  balanced: "Balanced — roughly symmetric in/out",
}

export function AsymmetryBadge({ cls }: { cls: AsymmetryClass | undefined }) {
  if (!cls) return <span className="text-muted-foreground">-</span>
  return (
    <span
      title={CLASS_TITLES[cls]}
      className={cn(
        "inline-block rounded border px-1.5 py-0.5 text-[10px] font-medium capitalize leading-none",
        CLASS_STYLES[cls],
      )}
    >
      {cls}
    </span>
  )
}

/**
 * Two-segment bar showing inbound vs outbound byte share, with a numeric
 * summary on hover. Falls back to a dash when neither side has traffic.
 */
export function RatioBar({ bytesIn, bytesOut }: { bytesIn: number; bytesOut: number }) {
  const total = bytesIn + bytesOut
  if (total <= 0) return <span className="text-muted-foreground">-</span>
  const inPct = (bytesIn / total) * 100
  const outPct = 100 - inPct
  return (
    <div
      className="flex items-center gap-2"
      title={`In ${formatBytes(bytesIn)} / Out ${formatBytes(bytesOut)}`}
    >
      <div className="flex h-1.5 w-20 overflow-hidden rounded-full bg-muted">
        <div className="h-1.5 bg-traffic-in" style={{ width: `${inPct}%` }} />
        <div className="h-1.5 bg-traffic-out" style={{ width: `${outPct}%` }} />
      </div>
      <span className="tabular-nums text-[10px] text-muted-foreground">
        <span className="text-traffic-in">{Math.round(inPct)}</span>
        <span className="mx-0.5">/</span>
        <span className="text-traffic-out">{Math.round(outPct)}</span>
      </span>
    </div>
  )
}
