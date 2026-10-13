/**
 * Sentinel ASN the backend emits for the collapsed private-use group. Mirrors
 * `model.PrivateASGroup`. 4294967295 is reserved by IANA so it can never
 * collide with a routed ASN, and unlike 0 it does not already mean "unknown".
 */
export const PRIVATE_AS_GROUP = 4294967295

/** Display name the backend sets on the grouped row. */
export const PRIVATE_AS_GROUP_NAME = "Private / Internal"

/**
 * isPrivateASGroup reports whether an AS identity is the collapsed private-use
 * group. Accepts strings too: movers / talkers / conversations carry the ASN as
 * a text key.
 */
export function isPrivateASGroup(asn: number | string | null | undefined): boolean {
  if (asn === null || asn === undefined || asn === "") return false
  return Number(asn) === PRIVATE_AS_GROUP
}

/**
 * asPath returns the detail route for an ASN, or `undefined` for the grouped
 * row — it aggregates hundreds of downstream ASNs and has no detail page.
 */
export function asPath(asn: number | string, search = ""): string | undefined {
  return isPrivateASGroup(asn) ? undefined : `/as/${asn}${search}`
}

/**
 * ourView turns the per-AS byte totals of /top/as and the AS-detail asymmetry
 * fields into the direction convention used everywhere else in the UI: OUR
 * network's point of view, "in" = received by us from that AS, "out" = sent by
 * us to it. That is the convention of the charts (inbound above the axis), the
 * link totals and the AS-detail IPv4/IPv6 totals.
 *
 * /top/as and ASAsymmetry count from the AS's side instead: their `bytes_in`
 * is traffic_by_as direction 'in', i.e. flows whose DESTINATION is the AS —
 * what we sent to it. Rendering those fields as-is made one AS page say "in"
 * for two opposite things. Flip them here, in one place.
 */
export function ourView(as: { bytes_in?: number; bytes_out?: number }): { inBytes: number; outBytes: number } {
  return { inBytes: as.bytes_out ?? 0, outBytes: as.bytes_in ?? 0 }
}

/** asLabel renders the identity of an AS: "AS1234", "1234" or the group name. */
export function asLabel(asn: number | string, prefix = true): string {
  if (isPrivateASGroup(asn)) return PRIVATE_AS_GROUP_NAME
  return prefix ? `AS${asn}` : String(asn)
}
