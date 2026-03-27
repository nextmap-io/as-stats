import { useQuery } from "@tanstack/react-query"
import { api } from "@/lib/api"
import type { QueryFilters } from "@/lib/types"

export function useOverview(filters: QueryFilters) {
  return useQuery({
    queryKey: ["overview", filters],
    queryFn: () => api.overview(filters),
    refetchInterval: 60_000,
  })
}

export function useTopAS(filters: QueryFilters) {
  return useQuery({
    queryKey: ["top-as", filters],
    queryFn: () => api.topAS(filters),
  })
}

export function useTopASTraffic(ipVersion: number, filters: QueryFilters) {
  const f = { ...filters, ip_version: ipVersion, limit: 50 }
  return useQuery({
    queryKey: ["top-as-traffic", ipVersion, filters],
    queryFn: () => api.topASTraffic(f),
  })
}

export function useTopIP(filters: QueryFilters) {
  return useQuery({
    queryKey: ["top-ip", filters],
    queryFn: () => api.topIP(filters),
  })
}

export function useTopPrefix(filters: QueryFilters) {
  return useQuery({
    queryKey: ["top-prefix", filters],
    queryFn: () => api.topPrefix(filters),
  })
}

export function useASDetail(asn: number, filters: QueryFilters) {
  return useQuery({
    queryKey: ["as-detail", asn, filters],
    queryFn: () => api.asDetail(asn, filters),
    enabled: asn > 0,
  })
}

export function useASPeers(asn: number, filters: QueryFilters) {
  return useQuery({
    queryKey: ["as-peers", asn, filters],
    queryFn: () => api.asPeers(asn, filters),
    enabled: asn > 0,
  })
}

export function useASTopIPs(asn: number, filters: QueryFilters) {
  return useQuery({
    queryKey: ["as-top-ips", asn, filters],
    queryFn: () => api.asTopIPs(asn, filters),
    enabled: asn > 0,
  })
}

export function useIPDetail(ip: string, filters: QueryFilters) {
  return useQuery({
    queryKey: ["ip-detail", ip, filters],
    queryFn: () => api.ipDetail(ip, filters),
    enabled: !!ip,
  })
}

export function useLinks(filters: QueryFilters) {
  return useQuery({
    queryKey: ["links", filters],
    queryFn: () => api.links(filters),
  })
}

export function useLinksTraffic(ipVersion: number, filters: QueryFilters) {
  const f = { ...filters, ip_version: ipVersion }
  return useQuery({
    queryKey: ["links-traffic", ipVersion, filters],
    queryFn: () => api.linksTraffic(f),
  })
}

export function useLinkDetail(tag: string, filters: QueryFilters) {
  return useQuery({
    queryKey: ["link-detail", tag, filters],
    queryFn: () => api.linkDetail(tag, filters),
    enabled: !!tag,
  })
}

export function useLinkColors() {
  const { data } = useQuery({
    queryKey: ["admin-links"],
    queryFn: () => api.adminLinks(),
    staleTime: 300_000,
  })
  const colors: Record<string, string> = {}
  if (data?.data) {
    for (const l of data.data) {
      if (l.color) colors[l.tag] = l.color
    }
  }
  return colors
}

export function useSearch(query: string) {
  return useQuery({
    queryKey: ["search", query],
    queryFn: () => api.search(query),
    enabled: query.length >= 2,
  })
}
