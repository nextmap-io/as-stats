import { Link } from "react-router-dom"
import { asLabel, isPrivateASGroup, PRIVATE_AS_GROUP_NAME } from "@/lib/asn"

/**
 * ASRef renders an AS identity: a link to its detail page, or — for the
 * collapsed private-use group — a plain label, since there is nothing to drill
 * into and "AS4294967295" would read as a real ASN.
 */
export function ASRef({
  asn,
  search = "",
  prefix = true,
  className,
  groupClassName,
}: {
  asn: number | string
  /** Serialized filters to carry into the detail page (`filterSearch`). */
  search?: string
  /** Render the "AS" prefix before the number. */
  prefix?: boolean
  className?: string
  /** Replaces `className` on the non-clickable grouped row. */
  groupClassName?: string
}) {
  if (isPrivateASGroup(asn)) {
    return (
      <span
        className={groupClassName ?? "font-mono text-muted-foreground"}
        title="Private-use ASNs (64512-65534, 4200000000-4294967294) collapsed into one row"
      >
        {PRIVATE_AS_GROUP_NAME}
      </span>
    )
  }
  return (
    <Link to={`/as/${asn}${search}`} className={className}>
      {asLabel(asn, prefix)}
    </Link>
  )
}
