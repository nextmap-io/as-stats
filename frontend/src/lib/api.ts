import type {
  ApiResponse,
  ASDetailData,
  ASInfo,
  ASTraffic,
  IPDetailData,
  IPTraffic,
  LinkConfig,
  LinkDetailData,
  LinkTimeSeries,
  LinkTraffic,
  Overview,
  PrefixTraffic,
  QueryFilters,
  UserInfo,
} from "./types"

const BASE = "/api/v1"

function getCSRFToken(): string {
  const match = document.cookie.match(/(?:^|;\s*)as_stats_csrf=([^;]+)/)
  return match ? match[1] : ""
}

interface FetchOptions {
  method?: string
  body?: unknown
}

async function fetchAPI<T>(
  path: string,
  params?: QueryFilters,
  opts?: FetchOptions,
): Promise<ApiResponse<T>> {
  const url = new URL(path, window.location.origin)
  url.pathname = `${BASE}${path}`

  if (params) {
    Object.entries(params).forEach(([key, value]) => {
      if (value !== undefined && value !== null && value !== "") {
        url.searchParams.set(key, String(value))
      }
    })
  }

  const headers: Record<string, string> = {
    "Content-Type": "application/json",
  }

  // Include CSRF token for state-changing requests
  const method = opts?.method || "GET"
  if (method !== "GET" && method !== "HEAD") {
    const csrf = getCSRFToken()
    if (csrf) {
      headers["X-CSRF-Token"] = csrf
    }
  }

  const res = await fetch(url.toString(), {
    method,
    headers,
    credentials: "include",
    body: opts?.body ? JSON.stringify(opts.body) : undefined,
  })

  if (res.status === 401) {
    window.location.href = "/auth/login"
    throw new Error("Authentication required")
  }

  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }))
    throw new Error(body.error || `API error: ${res.status}`)
  }

  return res.json()
}

export const api = {
  overview: (filters?: QueryFilters) => fetchAPI<Overview>("/overview", filters),

  topAS: (filters?: QueryFilters) => fetchAPI<ASTraffic[]>("/top/as", filters),
  topIP: (filters?: QueryFilters) => fetchAPI<IPTraffic[]>("/top/ip", filters),
  topPrefix: (filters?: QueryFilters) => fetchAPI<PrefixTraffic[]>("/top/prefix", filters),

  asDetail: (asn: number, filters?: QueryFilters) => fetchAPI<ASDetailData>(`/as/${asn}`, filters),
  asPeers: (asn: number, filters?: QueryFilters) => fetchAPI<ASTraffic[]>(`/as/${asn}/peers`, filters),
  asTopIPs: (asn: number, filters?: QueryFilters) => fetchAPI<IPTraffic[]>(`/as/${asn}/ips`, filters),

  ipDetail: (ip: string, filters?: QueryFilters) => fetchAPI<IPDetailData>(`/ip/${ip}`, filters),

  links: (filters?: QueryFilters) => fetchAPI<LinkTraffic[]>("/links", filters),
  linksTraffic: (filters?: QueryFilters) => fetchAPI<LinkTimeSeries[]>("/links/traffic", filters),
  linkDetail: (tag: string, filters?: QueryFilters) => fetchAPI<LinkDetailData>(`/link/${tag}`, filters),

  search: (q: string) => fetchAPI<ASInfo[]>("/search", { q }),

  me: () => fetchAPI<UserInfo>("/auth/me"),

  // Admin endpoints (POST/DELETE — include CSRF token)
  adminLinks: () => fetchAPI<LinkConfig[]>("/admin/links"),
  createLink: (link: Record<string, unknown>) =>
    fetchAPI<unknown>("/admin/links", undefined, { method: "POST", body: link }),
  deleteLink: (tag: string) =>
    fetchAPI<unknown>(`/admin/links/${tag}`, undefined, { method: "DELETE" }),
}
