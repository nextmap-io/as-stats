import { useQuery } from "@tanstack/react-query"
import { api } from "@/lib/api"

/**
 * Fetch the current user only after feature discovery has explicitly confirmed
 * that authentication is enabled. In unauthenticated deployments /auth/me is
 * intentionally unavailable, so probing it would create a false 401 redirect.
 */
export function useCurrentUser(authEnabled: boolean | undefined) {
  return useQuery({
    queryKey: ["auth-me"],
    queryFn: () => api.me(),
    staleTime: 300_000,
    retry: false,
    enabled: authEnabled === true,
  })
}
