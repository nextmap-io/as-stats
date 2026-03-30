import { useQuery } from "@tanstack/react-query"
import { api } from "@/lib/api"

/** Hook for getting reverse DNS PTR record */
export function useReverseDNS(ip: string) {
  const { data } = useQuery({
    queryKey: ["dns-ptr", ip],
    queryFn: () => api.dnsPtr(ip),
    staleTime: 3600_000,
    enabled: !!ip,
  })
  return data?.data?.ptr || ""
}
