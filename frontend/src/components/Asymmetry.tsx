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

// The class is computed from the AS's side of the exchange (see ourView in
// lib/asn.ts): an eyeball network receives far more than it sends, so from
// your side it is mostly OUTBOUND, and a content network mostly INBOUND.
const CLASS_STYLES: Record<AsymmetryClass, string> = {
  eyeball: "border-traffic-out/50 text-traffic-out",
  content: "border-traffic-in/50 text-traffic-in",
  balanced: "border-border text-muted-foreground",
}

const CLASS_TITLES: Record<AsymmetryClass, string> = {
  eyeball: "Eyeball network (access, broadband): it receives far more from you than it sends",
  content: "Content network (hosting, CDN): it sends you far more than it receives",
  balanced: "Balanced: roughly as much in as out",
}

export function AsymmetryBadge({ cls }: { cls: AsymmetryClass | undefined }) {
  if (!cls) return <span className="text-muted-foreground">-</span>
  return (
    <span
      title={CLASS_TITLES[cls]}
      className={cn(
        "inline-block rounded border px-1.5 py-0.5 text-xs font-medium capitalize leading-none",
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
 * `bytesIn` / `bytesOut` are from YOUR network's side: pass AS totals through
 * ourView() first.
 */
export function RatioBar({ bytesIn, bytesOut }: { bytesIn: number; bytesOut: number }) {
  const total = bytesIn + bytesOut
  if (total <= 0) return <span className="text-muted-foreground">-</span>
  const inPct = (bytesIn / total) * 100
  const outPct = 100 - inPct
  return (
    <div
      className="inline-flex items-center gap-2 pl-3"
      title={`In ${formatBytes(bytesIn)} / Out ${formatBytes(bytesOut)}`}
    >
      <div className="flex h-1.5 w-20 overflow-hidden rounded-[1px] bg-muted">
        <div className="h-1.5 bg-traffic-in" style={{ width: `${inPct}%` }} />
        <div className="h-1.5 bg-traffic-out" style={{ width: `${outPct}%` }} />
      </div>
      <span className="tabular-nums text-xs text-muted-foreground">
        <span className="text-traffic-in">{Math.round(inPct)}</span>
        <span className="mx-0.5">/</span>
        <span className="text-traffic-out">{Math.round(outPct)}</span>
      </span>
    </div>
  )
}
