import type {
  Alert,
  AlertRule,
  AlertsSummary,
  ApiResponse,
  ASDetailData,
  ASInfo,
  ASTraffic,
  ASTrafficDetail,
  AuditLogEntry,
  Features,
  FlowLogEntry,
  FlowSearchFilters,
  IPDetailData,
  IPTraffic,
  LinkConfig,
  LinkDetailData,
  LinkTimeSeries,
  LinkTraffic,
  Overview,
  PortTraffic,
  PrefixTraffic,
  ProtocolTraffic,
  QueryFilters,
  TrafficPoint,
  UserInfo,
  WebhookConfig,
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
  topASTraffic: (filters?: QueryFilters) => fetchAPI<ASTrafficDetail[]>("/top/as/traffic", filters),
  topIP: (filters?: QueryFilters) => fetchAPI<IPTraffic[]>("/top/ip", filters),
  topPrefix: (filters?: QueryFilters) => fetchAPI<PrefixTraffic[]>("/top/prefix", filters),

  asDetail: (asn: number, filters?: QueryFilters) => fetchAPI<ASDetailData>(`/as/${asn}`, filters),
  asPeers: (asn: number, filters?: QueryFilters) => fetchAPI<ASTraffic[]>(`/as/${asn}/peers`, filters),
  asTopIPs: (asn: number, filters?: QueryFilters) => fetchAPI<IPTraffic[]>(`/as/${asn}/ips`, filters),
  asRemoteIPs: (asn: number, filters?: QueryFilters) => fetchAPI<IPTraffic[]>(`/as/${asn}/ips`, { ...filters, scope: "external" }),

  ipDetail: (ip: string, filters?: QueryFilters) => fetchAPI<IPDetailData>(`/ip/${ip}`, filters),

  links: (filters?: QueryFilters) => fetchAPI<LinkTraffic[]>("/links", filters),
  linksTraffic: (filters?: QueryFilters) => fetchAPI<LinkTimeSeries[]>("/links/traffic", filters),
  linkDetail: (tag: string, filters?: QueryFilters) => fetchAPI<LinkDetailData>(`/link/${tag}`, filters),

  status: () => fetchAPI<{ routers: { router_ip: string; last_seen: string; flow_count: number }[]; total_rows: number; db_size: number }>("/status"),
  dnsPtr: (ip: string) => fetchAPI<{ ip: string; ptr: string }>("/dns/ptr", { ip } as QueryFilters),
  search: (q: string) => fetchAPI<ASInfo[]>("/search", { q }),

  me: () => fetchAPI<UserInfo>("/auth/me"),
  logout: () => fetchAPI<unknown>("/auth/logout", undefined, { method: "POST" }),

  // Admin endpoints (POST/DELETE — include CSRF token)
  adminLinks: () => fetchAPI<LinkConfig[]>("/admin/links"),
  createLink: (link: Record<string, unknown>) =>
    fetchAPI<unknown>("/admin/links", undefined, { method: "POST", body: link }),
  deleteLink: (tag: string) =>
    fetchAPI<unknown>(`/admin/links/${tag}`, undefined, { method: "DELETE" }),

  // ─── Features ────────────────────────────────────────────
  features: () => fetchAPI<Features>("/features"),

  // ─── Flow search ─────────────────────────────────────────
  flowSearch: (filters: FlowSearchFilters) =>
    fetchAPI<FlowLogEntry[]>("/flows/search", filters as QueryFilters),
  flowTimeSeries: (filters: FlowSearchFilters) =>
    fetchAPI<TrafficPoint[]>("/flows/timeseries", filters as QueryFilters),
  flowExportCSV: (filters: FlowSearchFilters) => {
    const url = new URL("/api/v1/flows/search", window.location.origin)
    Object.entries({ ...filters, format: "csv" }).forEach(([k, v]) => {
      if (v !== undefined && v !== null && v !== "") url.searchParams.set(k, String(v))
    })
    window.location.href = url.toString()
  },

  // ─── Port stats ──────────────────────────────────────────
  topProtocol: (filters?: QueryFilters) =>
    fetchAPI<ProtocolTraffic[]>("/top/protocol", filters),
  topPort: (filters?: QueryFilters & { protocol?: number }) =>
    fetchAPI<PortTraffic[]>("/top/port", filters as QueryFilters),

  // ─── Alerts ──────────────────────────────────────────────
  alerts: (status?: string, limit?: number) =>
    fetchAPI<Alert[]>("/alerts", { ...(status && { status }), ...(limit && { limit }) } as QueryFilters),
  alertsSummary: () => fetchAPI<AlertsSummary>("/alerts/summary"),
  ackAlert: (id: string) =>
    fetchAPI<unknown>(`/alerts/${id}/ack`, undefined, { method: "POST" }),
  resolveAlert: (id: string) =>
    fetchAPI<unknown>(`/alerts/${id}/resolve`, undefined, { method: "POST" }),
  blockAlert: (id: string, durationMinutes: number, reason: string) =>
    fetchAPI<unknown>(`/alerts/${id}/block`, undefined, {
      method: "POST",
      body: { duration_minutes: durationMinutes, reason },
    }),

  // ─── Alert rules (admin) ─────────────────────────────────
  listRules: () => fetchAPI<AlertRule[]>("/admin/rules"),
  createRule: (rule: Partial<AlertRule>) =>
    fetchAPI<AlertRule>("/admin/rules", undefined, { method: "POST", body: rule }),
  updateRule: (id: string, rule: Partial<AlertRule>) =>
    fetchAPI<AlertRule>(`/admin/rules/${id}`, undefined, { method: "PUT", body: rule }),
  deleteRule: (id: string) =>
    fetchAPI<unknown>(`/admin/rules/${id}`, undefined, { method: "DELETE" }),

  // ─── Webhooks (admin) ────────────────────────────────────
  listWebhooks: () => fetchAPI<WebhookConfig[]>("/admin/webhooks"),
  createWebhook: (webhook: Partial<WebhookConfig>) =>
    fetchAPI<WebhookConfig>("/admin/webhooks", undefined, { method: "POST", body: webhook }),
  updateWebhook: (id: string, webhook: Partial<WebhookConfig>) =>
    fetchAPI<WebhookConfig>(`/admin/webhooks/${id}`, undefined, { method: "PUT", body: webhook }),
  deleteWebhook: (id: string) =>
    fetchAPI<unknown>(`/admin/webhooks/${id}`, undefined, { method: "DELETE" }),

  // ─── Audit log (admin) ───────────────────────────────────
  auditLog: (filters?: { from?: string; to?: string; user?: string; action?: string; limit?: number }) =>
    fetchAPI<AuditLogEntry[]>("/admin/audit", filters as QueryFilters),
}
