import { useReverseDNS } from "@/hooks/useDns"

/**
 * IP display with PTR as hover tooltip + inline on desktop.
 *
 * Uses inline-flex so the component never exceeds its parent's width:
 * the IP itself is non-shrinkable, while the PTR text truncates with
 * ellipsis when space is tight. This prevents long IPv6 addresses
 * combined with long reverse DNS names from blowing out table layouts.
 */
export function IPWithPTR({ ip }: { ip: string }) {
  const ptr = useReverseDNS(ip)
  return (
    <span className="inline-flex items-baseline gap-1 max-w-full" title={ptr || ip}>
      <span className="shrink-0">{ip}</span>
      {ptr && (
        <span className="text-muted-foreground text-[9px] hidden lg:inline truncate min-w-0">
          ({ptr})
        </span>
      )}
    </span>
  )
}
