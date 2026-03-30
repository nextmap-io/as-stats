import { useReverseDNS } from "@/hooks/useDns"

/** IP display with PTR as hover tooltip + inline on desktop */
export function IPWithPTR({ ip }: { ip: string }) {
  const ptr = useReverseDNS(ip)
  return (
    <span title={ptr || undefined}>
      {ip}
      {ptr && <span className="text-muted-foreground text-[9px] ml-1 hidden lg:inline">({ptr})</span>}
    </span>
  )
}
