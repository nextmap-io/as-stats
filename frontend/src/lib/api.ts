import type {
  ApiResponse,
  ASDetailData,
  ASInfo,
  ASTraffic,
  IPDetailData,
  IPTraffic,
  LinkDetailData,
  LinkTraffic,
  Overview,
  PrefixTraffic,
  QueryFilters,
  UserInfo,
} from "./types"

const BASE = "/api/v1"

async function fetchAPI<T>(path: string, params?: QueryFilters): Promise<ApiResponse<T>> {
  const url = new URL(path, window.location.origin)
  url.pathname = `${BASE}${path}`

  if (params) {
    Object.entries(params).forEach(([key, value]) => {
      if (value !== undefined && value !== null && value !== "") {
        url.searchParams.set(key, String(value))
      }
    })
  }

  const res = await fetch(url.toString(), {
    headers: { "Content-Type": "application/json" },
    credentials: "include",
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

  ipDetail: (ip: string, filters?: QueryFilters) => fetchAPI<IPDetailData>(`/ip/${ip}`, filters),

  links: (filters?: QueryFilters) => fetchAPI<LinkTraffic[]>("/links", filters),
  linkDetail: (tag: string, filters?: QueryFilters) => fetchAPI<LinkDetailData>(`/link/${tag}`, filters),

  search: (q: string) => fetchAPI<ASInfo[]>("/search", { q }),

  me: () => fetchAPI<UserInfo>("/auth/me"),
}
