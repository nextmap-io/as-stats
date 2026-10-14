import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { api, isApiError } from "@/lib/api"
import { liveStatusFromLegacy } from "@/lib/status"
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

export function useTrafficHeatmap(filters: QueryFilters) {
  return useQuery({
    queryKey: ["traffic-heatmap", filters],
    queryFn: () => api.trafficHeatmap(filters),
    refetchInterval: REFETCH,
  })
}

export function useStatus(filters?: QueryFilters) {
  return useQuery({
    queryKey: ["status", filters],
    queryFn: () => api.status(filters),
    refetchInterval: 30_000,
  })
}

// useLiveStatus backs the header health dot and the home status strip. It reads
// GET /status/live (3.1: last 10 minutes only, server-cached 30 s). On an older
// server that answers 404 it falls back to the windowed /status over the last
// hour and derives the same shape client-side (see liveStatusFromLegacy).
// One shared key, so the header and the home page share a single poll.
export function useLiveStatus() {
  return useQuery({
    queryKey: ["status", "live"],
    queryFn: async () => {
      try {
        return (await api.statusLive()).data
      } catch (err) {
        if (!isApiError(err, 404, 405)) throw err
        const legacy = await api.status({ period: "1h" })
        return liveStatusFromLegacy(legacy.data, Date.now())
      }
    },
    staleTime: 30_000,
    refetchInterval: 60_000,
    retry: false,
  })
}

// Shared by the header badge and the home status strip (one poll, one key).
export function useAlertsSummary(enabled: boolean) {
  return useQuery({
    queryKey: ["alerts-summary"],
    queryFn: () => api.alertsSummary(),
    refetchInterval: 30_000,
    enabled,
    retry: false,
  })
}

export function useActiveAlerts(enabled: boolean, limit = 50) {
  return useQuery({
    queryKey: ["alerts", "active", limit],
    queryFn: () => api.alerts("active", limit),
    refetchInterval: 30_000,
    enabled,
    retry: false,
  })
}

export function useSearch(query: string) {
  return useQuery({
    queryKey: ["search", query],
    queryFn: () => api.search(query),
    enabled: query.length >= 2,
  })
}

// ─── Anomaly explainability (Module E) ─────────────────────
// On-demand decomposition of a link's anomalous window. Disabled until a
// target link tag is provided (e.g. when the operator clicks "Explain").
export function useAnomalyExplain(target: string, filters: QueryFilters) {
  return useQuery({
    queryKey: ["anomaly-explain", target, filters],
    queryFn: () => api.anomalyExplain(target, filters),
    enabled: !!target,
  })
}

// /admin/storage is admin-only server-side; callers pass enabled=false for
// non-admins so viewers don't poll an endpoint that can only answer 403.
export function useStorageStatus(enabled = true) {
  return useQuery({
    queryKey: ["storage"],
    queryFn: () => api.storageStatus(),
    enabled,
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

// ─── Read-only API tokens (admin, Module G) ────────────────

const API_TOKENS_KEY = ["api-tokens"]

export function useAPITokens() {
  return useQuery({
    queryKey: API_TOKENS_KEY,
    queryFn: () => api.listAPITokens(),
  })
}

export function useCreateAPIToken() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (body: { name: string; expires_in_days?: number }) => api.createAPIToken(body),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: API_TOKENS_KEY }),
  })
}

export function useRevokeAPIToken() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.revokeAPIToken(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: API_TOKENS_KEY }),
  })
}
