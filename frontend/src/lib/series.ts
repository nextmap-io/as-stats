import type { LinkTimeSeries, TrafficPoint } from "./types"

/** Address-family switch shared by every per-link breakdown (URL `?ipv=`). */
export type IPFamily = "both" | "4" | "6"

export const IP_FAMILY_OPTIONS: readonly { value: IPFamily; label: string }[] = [
  { value: "both", label: "Both" },
  { value: "4", label: "IPv4" },
  { value: "6", label: "IPv6" },
]

/** Reads `?ipv=` the way the dashboard does: anything but 4 or 6 is "both". */
export function parseIPFamily(v: string | null): IPFamily {
  return v === "4" || v === "6" ? v : "both"
}

/**
 * mergeLinkSeries adds per-link series of several address families into one
 * per-link series (bytes and packets summed per bucket), so IPv4 + IPv6 is one
 * chart on one scale instead of two charts side by side.
 */
export function mergeLinkSeries(...lists: (LinkTimeSeries[] | undefined | null)[]): LinkTimeSeries[] {
  const byTag = new Map<string, { description: string; points: Map<string, TrafficPoint> }>()
  for (const list of lists) {
    for (const s of list ?? []) {
      let link = byTag.get(s.link_tag)
      if (!link) {
        link = { description: s.description, points: new Map() }
        byTag.set(s.link_tag, link)
      }
      if (!link.description && s.description) link.description = s.description
      for (const p of s.points) {
        const acc = link.points.get(p.t)
        if (!acc) {
          link.points.set(p.t, { ...p })
          continue
        }
        acc.bytes_in += p.bytes_in
        acc.bytes_out += p.bytes_out
        if (p.packets_in !== undefined) acc.packets_in = (acc.packets_in ?? 0) + p.packets_in
        if (p.packets_out !== undefined) acc.packets_out = (acc.packets_out ?? 0) + p.packets_out
      }
    }
  }
  return Array.from(byTag, ([link_tag, { description, points }]) => ({
    link_tag,
    description,
    points: Array.from(points.values()).sort((a, b) => Date.parse(a.t) - Date.parse(b.t)),
  }))
}
