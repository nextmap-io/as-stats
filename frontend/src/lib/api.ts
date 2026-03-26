import type { ApiResponse, QueryFilters } from "./types"

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
  overview: (filters?: QueryFilters) => fetchAPI<any>("/overview", filters),

  topAS: (filters?: QueryFilters) => fetchAPI<any>("/top/as", filters),
  topIP: (filters?: QueryFilters) => fetchAPI<any>("/top/ip", filters),
  topPrefix: (filters?: QueryFilters) => fetchAPI<any>("/top/prefix", filters),

  asDetail: (asn: number, filters?: QueryFilters) => fetchAPI<any>(`/as/${asn}`, filters),
  asPeers: (asn: number, filters?: QueryFilters) => fetchAPI<any>(`/as/${asn}/peers`, filters),

  ipDetail: (ip: string, filters?: QueryFilters) => fetchAPI<any>(`/ip/${ip}`, filters),

  links: (filters?: QueryFilters) => fetchAPI<any>("/links", filters),
  linkDetail: (tag: string, filters?: QueryFilters) => fetchAPI<any>(`/link/${tag}`, filters),

  search: (q: string) => fetchAPI<any>("/search", { q } as any),

  me: () => fetchAPI<any>("/auth/me"),
}
