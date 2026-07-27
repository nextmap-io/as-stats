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

/** asLabel renders the identity of an AS: "AS1234", "1234" or the group name. */
export function asLabel(asn: number | string, prefix = true): string {
  if (isPrivateASGroup(asn)) return PRIVATE_AS_GROUP_NAME
  return prefix ? `AS${asn}` : String(asn)
}
