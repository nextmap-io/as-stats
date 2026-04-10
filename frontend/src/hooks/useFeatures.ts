import { useQuery } from "@tanstack/react-query"
import { api } from "@/lib/api"
import type { Features } from "@/lib/types"

/**
 * useFeatures returns the currently enabled server features.
 *
 * Cached forever (staleTime: Infinity) because feature flags only change
 * on server restart. Frontend refreshes on page reload.
 */
export function useFeatures() {
  return useQuery({
    queryKey: ["features"],
    queryFn: () => api.features(),
    staleTime: Infinity,
    gcTime: Infinity,
    retry: 1,
  })
}

/**
 * Convenience hook: returns a boolean-keyed object, with safe defaults
 * (all disabled) while loading.
 */
export function useFeatureFlags(): Features {
  const { data } = useFeatures()
  return data?.data ?? {
    flow_search: false,
    port_stats: false,
    alerts: false,
    bgp: false,
    auth: false,
  }
}
