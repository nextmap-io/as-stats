import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { api } from "@/lib/api"
import type { QueryFilters, ReportSchedule } from "@/lib/types"

const REFETCH = 300_000 // 5 minutes

export function useOverview(filters: QueryFilters) {
  return useQuery({
    queryKey: ["overview", filters],
    queryFn: () => api.overview(filters),
    refetchInterval: REFETCH,
  })
}

export function useTopAS(filters: QueryFilters) {
  return useQuery({
    queryKey: ["top-as", filters],
    queryFn: () => api.topAS(filters),
    refetchInterval: REFETCH,
  })
}

export function useTopASTraffic(ipVersion: number, filters: QueryFilters) {
  const f = { ...filters, ip_version: ipVersion, limit: 50 }
  return useQuery({
    queryKey: ["top-as-traffic", ipVersion, filters],
    queryFn: () => api.topASTraffic(f),
    refetchInterval: REFETCH,
  })
}

export function useTopIP(filters: QueryFilters) {
  return useQuery({
    queryKey: ["top-ip", filters],
    queryFn: () => api.topIP(filters),
    refetchInterval: REFETCH,
  })
}

export function useTopPrefix(filters: QueryFilters) {
  return useQuery({
    queryKey: ["top-prefix", filters],
    queryFn: () => api.topPrefix(filters),
    refetchInterval: REFETCH,
  })
}

export function useTopCountry(filters: QueryFilters) {
  return useQuery({
    queryKey: ["top-country", filters],
    queryFn: () => api.topCountry(filters),
    refetchInterval: REFETCH,
  })
}

export function useConversations(filters: QueryFilters) {
  return useQuery({
    queryKey: ["conversations", filters],
    queryFn: () => api.conversations(filters),
    refetchInterval: REFETCH,
  })
}

export function useMovers(dim: string, filters: QueryFilters) {
  return useQuery({
    queryKey: ["movers", dim, filters],
    queryFn: () => api.movers(dim, filters),
    refetchInterval: REFETCH,
  })
}

export function useTalkers(dim: string, filters: QueryFilters) {
  return useQuery({
    queryKey: ["talkers", dim, filters],
    queryFn: () => api.talkers(dim, filters),
    refetchInterval: REFETCH,
  })
}

export function useASDetail(asn: number, filters: QueryFilters) {
  return useQuery({
    queryKey: ["as-detail", asn, filters],
    queryFn: () => api.asDetail(asn, filters),
    enabled: asn > 0,
    refetchInterval: REFETCH,
  })
}

export function useASRemoteIPs(asn: number, filters: QueryFilters) {
  return useQuery({
    queryKey: ["as-remote-ips", asn, filters],
    queryFn: () => api.asRemoteIPs(asn, filters),
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
    refetchInterval: REFETCH,
  })
}

export function useLinks(filters: QueryFilters) {
  return useQuery({
    queryKey: ["links", filters],
    queryFn: () => api.links(filters),
    refetchInterval: REFETCH,
  })
}

export function useLinksTraffic(ipVersion: number, filters: QueryFilters) {
  const f = { ...filters, ip_version: ipVersion }
  return useQuery({
    queryKey: ["links-traffic", ipVersion, filters],
    queryFn: () => api.linksTraffic(f),
    refetchInterval: REFETCH,
  })
}

export function useLinkDetail(tag: string, filters: QueryFilters) {
  return useQuery({
    queryKey: ["link-detail", tag, filters],
    queryFn: () => api.linkDetail(tag, filters),
    enabled: !!tag,
    refetchInterval: REFETCH,
  })
}

export function useLinksCapacity(filters: QueryFilters) {
  return useQuery({
    queryKey: ["links-capacity", filters],
    queryFn: () => api.linksCapacity(filters),
    refetchInterval: REFETCH,
  })
}

export function useLinkLoadCurve(tag: string, filters: QueryFilters) {
  return useQuery({
    queryKey: ["link-load-curve", tag, filters],
    queryFn: () => api.linkLoadCurve(tag, filters),
    enabled: !!tag,
    refetchInterval: REFETCH,
  })
}

export function useLinkColors() {
  const { data } = useQuery({
    queryKey: ["admin-links"],
    queryFn: () => api.adminLinks(),
    staleTime: 30_000,
  })
  const colors: Record<string, string> = {}
  if (data?.data) {
    for (const l of data.data) {
      if (l.color) colors[l.tag] = l.color
    }
  }
  return colors
}

export function useStatus() {
  return useQuery({
    queryKey: ["status"],
    queryFn: () => api.status(),
    refetchInterval: 30_000,
  })
}

export function useSearch(query: string) {
  return useQuery({
    queryKey: ["search", query],
    queryFn: () => api.search(query),
    enabled: query.length >= 2,
  })
}

export function useStorageStatus() {
  return useQuery({
    queryKey: ["storage"],
    queryFn: () => api.storageStatus(),
    refetchInterval: 30_000,
  })
}

export function useSetRetention() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ table, ttl_days, enabled }: { table: string; ttl_days: number; enabled: boolean }) =>
      api.setRetention(table, { ttl_days, enabled }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["storage"] }),
  })
}

// ─── Scheduled reports (admin, FEATURE_REPORTS) ────────────

const REPORTS_KEY = ["report-schedules"]

export function useReportSchedules() {
  return useQuery({
    queryKey: REPORTS_KEY,
    queryFn: () => api.listReportSchedules(),
  })
}

export function useCreateReportSchedule() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (schedule: Partial<ReportSchedule>) => api.createReportSchedule(schedule),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: REPORTS_KEY }),
  })
}

export function useUpdateReportSchedule() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, schedule }: { id: string; schedule: Partial<ReportSchedule> }) =>
      api.updateReportSchedule(id, schedule),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: REPORTS_KEY }),
  })
}

export function useDeleteReportSchedule() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.deleteReportSchedule(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: REPORTS_KEY }),
  })
}

export function useTestReport() {
  return useMutation({
    mutationFn: (id: string) => api.testReport(id),
  })
}
