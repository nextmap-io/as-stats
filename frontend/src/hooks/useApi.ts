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

export function useLinksGrouped(filters: QueryFilters) {
  return useQuery({
    queryKey: ["links-grouped", filters],
    queryFn: () => api.linksGrouped(filters),
  })
}

export function useLinksTimeSeries(filters: QueryFilters) {
  return useQuery({
    queryKey: ["links-timeseries", filters],
    queryFn: () => api.linksTimeSeries(filters),
  })
}

export function useLinkDetail(tag: string, filters: QueryFilters) {
  return useQuery({
    queryKey: ["link-detail", tag, filters],
    queryFn: () => api.linkDetail(tag, filters),
    enabled: !!tag,
  })
}

export function useSearch(query: string) {
  return useQuery({
    queryKey: ["search", query],
    queryFn: () => api.search(query),
    enabled: query.length >= 2,
  })
}
