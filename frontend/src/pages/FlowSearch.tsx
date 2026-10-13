import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link, useSearchParams } from "react-router-dom"
import { api } from "@/lib/api"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { formatBytes, formatNumber } from "@/lib/utils"
import { Search, Download, X, AlertCircle } from "lucide-react"
import type { FlowSearchFilters, FlowLogEntry } from "@/lib/types"
import { IPWithPTR } from "@/components/PTR"

const PROTOCOLS = [
  { value: 0, label: "All" },
  { value: 6, label: "TCP" },
  { value: 17, label: "UDP" },
  { value: 1, label: "ICMP" },
  { value: 47, label: "GRE" },
  { value: 50, label: "ESP" },
  { value: 89, label: "OSPF" },
]

const PERIODS = [
  { value: "1h", label: "1h" },
  { value: "6h", label: "6h" },
  { value: "24h", label: "24h" },
  { value: "7d", label: "7d" },
  { value: "30d", label: "30d" },
]

/**
 * Filters passed in the URL by other pages (Live Threats links here with
 * ?dst_ip=…&from=…&to=…). When present, the search runs straight away.
 */
function filtersFromURL(params: URLSearchParams): FlowSearchFilters | null {
  const out: FlowSearchFilters = { limit: 100 }
  let any = false
  for (const key of ["src_ip", "dst_ip"] as const) {
    const v = params.get(key)?.trim()
    if (v) {
      out[key] = v
      any = true
    }
  }
  const from = params.get("from")
  const to = params.get("to")
  if (from && to && !Number.isNaN(Date.parse(from)) && !Number.isNaN(Date.parse(to))) {
    out.from = from
    out.to = to
  } else {
    out.period = "1h"
  }
  return any ? out : null
}

export function FlowSearch() {
  const [searchParams] = useSearchParams()
  const [initial] = useState(() => filtersFromURL(searchParams))
  const [filters, setFilters] = useState<FlowSearchFilters>(initial ?? {
    period: "1h",
    limit: 100,
  })
  const [submitted, setSubmitted] = useState<FlowSearchFilters | null>(initial)

  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ["flow-search", submitted],
    queryFn: () => api.flowSearch(submitted!),
    enabled: !!submitted,
  })

  const results: FlowLogEntry[] = data?.data || []

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    setSubmitted({ ...filters })
  }

  const handleReset = () => {
    setFilters({ period: "1h", limit: 100 })
    setSubmitted(null)
  }

  const handleExport = () => {
    if (submitted) api.flowExportCSV(submitted)
  }

  const update = <K extends keyof FlowSearchFilters>(key: K, value: FlowSearchFilters[K]) => {
    setFilters(f => ({ ...f, [key]: value }))
  }

  return (
    <div className="space-y-4">
      <div className="flex items-baseline justify-between">
        <h1 className="text-2xl font-semibold">Flow Search</h1>
        <span className="text-xs text-muted-foreground">
          Forensic search over the flow log (max 30 days)
        </span>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Filters</CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSubmit} className="space-y-3">
            {/* Period */}
            <div className="flex items-center gap-2" role="group" aria-label="Period">
              <span className="text-xs text-muted-foreground w-20 shrink-0">Period</span>
              <div className="flex gap-1">
                {PERIODS.map(p => (
                  <button
                    key={p.value}
                    type="button"
                    onClick={() => setFilters(f => ({ ...f, period: p.value, from: undefined, to: undefined }))}
                    className={`px-2.5 py-1 text-xs rounded transition-colors ${
                      filters.period === p.value
                        ? "bg-primary text-primary-foreground"
                        : "bg-muted/50 text-muted-foreground hover:bg-accent"
                    }`}
                  >
                    {p.label}
                  </button>
                ))}
              </div>
              {filters.from && filters.to && (
                <span className="text-[13px] text-foreground-secondary">
                  {new Date(filters.from).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}
                  {"–"}
                  {new Date(filters.to).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}
                </span>
              )}
            </div>

            {/* Source / Destination grids */}
            <div className="grid gap-3 lg:grid-cols-2">
              <div className="space-y-2">
                <div className="text-xs font-medium text-muted-foreground">Source</div>
                <Field label="IP / CIDR">
                  <input
                    type="text"
                    value={filters.src_ip || ""}
                    onChange={e => update("src_ip", e.target.value)}
                    placeholder="10.0.0.1 or 10.0.0.0/24"
                    className="flex-1 h-7 px-2 rounded border border-input bg-background text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring"
                  />
                </Field>
                <Field label="AS">
                  <input
                    type="number"
                    value={filters.src_as || ""}
                    onChange={e => update("src_as", e.target.value ? Number(e.target.value) : undefined)}
                    placeholder="13335"
                    className="w-28 h-7 px-2 rounded border border-input bg-background text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring"
                  />
                </Field>
                <Field label="Port">
                  <input
                    type="number"
                    value={filters.src_port || ""}
                    onChange={e => update("src_port", e.target.value ? Number(e.target.value) : undefined)}
                    placeholder="any"
                    className="w-24 h-7 px-2 rounded border border-input bg-background text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring"
                    min={1}
                    max={65535}
                  />
                </Field>
              </div>

              <div className="space-y-2">
                <div className="text-xs font-medium text-muted-foreground">Destination</div>
                <Field label="IP / CIDR">
                  <input
                    type="text"
                    value={filters.dst_ip || ""}
                    onChange={e => update("dst_ip", e.target.value)}
                    placeholder="1.1.1.1"
                    className="flex-1 h-7 px-2 rounded border border-input bg-background text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring"
                  />
                </Field>
                <Field label="AS">
                  <input
                    type="number"
                    value={filters.dst_as || ""}
                    onChange={e => update("dst_as", e.target.value ? Number(e.target.value) : undefined)}
                    placeholder="13335"
                    className="w-28 h-7 px-2 rounded border border-input bg-background text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring"
                  />
                </Field>
                <Field label="Port">
                  <input
                    type="number"
                    value={filters.dst_port || ""}
                    onChange={e => update("dst_port", e.target.value ? Number(e.target.value) : undefined)}
                    placeholder="e.g. 443"
                    className="w-24 h-7 px-2 rounded border border-input bg-background text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring"
                    min={1}
                    max={65535}
                  />
                </Field>
              </div>
            </div>

            {/* Protocol + IP version + min bytes */}
            <div className="flex flex-wrap items-center gap-3">
              <Field label="Protocol">
                <select
                  value={filters.protocol || 0}
                  onChange={e => update("protocol", Number(e.target.value) || undefined)}
                  className="h-7 px-2 rounded border border-input bg-background text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring"
                >
                  {PROTOCOLS.map(p => (
                    <option key={p.value} value={p.value}>{p.label}</option>
                  ))}
                </select>
              </Field>
              <Field label="IP version">
                <select
                  value={filters.ip_version || 0}
                  onChange={e => update("ip_version", e.target.value ? Number(e.target.value) as 4 | 6 : undefined)}
                  className="h-7 px-2 rounded border border-input bg-background text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring"
                >
                  <option value={0}>All</option>
                  <option value={4}>IPv4</option>
                  <option value={6}>IPv6</option>
                </select>
              </Field>
              <Field label="Min bytes">
                <input
                  type="number"
                  value={filters.min_bytes || ""}
                  onChange={e => update("min_bytes", e.target.value ? Number(e.target.value) : undefined)}
                  placeholder="0"
                  className="w-28 h-7 px-2 rounded border border-input bg-background text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring"
                  min={0}
                />
              </Field>
              <Field label="Limit">
                <input
                  type="number"
                  value={filters.limit || 100}
                  onChange={e => update("limit", Number(e.target.value))}
                  className="w-20 h-7 px-2 rounded border border-input bg-background text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring"
                  min={1}
                  max={1000}
                />
              </Field>
            </div>

            {/* Action buttons */}
            <div className="flex gap-2 pt-1">
              <button
                type="submit"
                disabled={isLoading}
                className="inline-flex items-center gap-1.5 px-3 py-1 text-xs font-medium rounded bg-primary text-primary-foreground hover:bg-primary/90 transition-colors disabled:opacity-50"
              >
                <Search className="size-3" />
                Search
              </button>
              <button
                type="button"
                onClick={handleReset}
                className="inline-flex items-center gap-1.5 px-3 py-1 text-xs font-medium rounded border border-input bg-background hover:bg-accent transition-colors"
              >
                <X className="size-3" />
                Reset
              </button>
              <button
                type="button"
                onClick={handleExport}
                disabled={!submitted || results.length === 0}
                className="ml-auto inline-flex items-center gap-1.5 px-3 py-1 text-xs font-medium rounded border border-input bg-background hover:bg-accent transition-colors disabled:opacity-50"
                title="Export current search as CSV (max 100k rows)"
              >
                <Download className="size-3" />
                Export CSV
              </button>
            </div>
          </form>
        </CardContent>
      </Card>

      {/* Results */}
      {error && (
        <Card className="border-destructive/30">
          <CardContent className="p-4 flex items-start gap-2 text-destructive">
            <AlertCircle className="size-4 shrink-0 mt-0.5" />
            <p className="text-xs">{(error as Error).message}</p>
          </CardContent>
        </Card>
      )}

      {submitted && !error && (
        <Card>
          <CardHeader>
            <div className="flex items-baseline justify-between">
              <CardTitle>
                Results {isLoading ? "(loading...)" : `(${formatNumber(results.length)})`}
              </CardTitle>
              {results.length >= (submitted.limit || 100) && (
                <span className="text-xs text-muted-foreground">
                  Limit reached — refine filters or increase limit
                </span>
              )}
            </div>
          </CardHeader>
          <CardContent>
            {isLoading ? (
              <p className="text-muted-foreground text-xs">Searching…</p>
            ) : results.length === 0 ? (
              <p className="text-muted-foreground text-xs">No flows match your filters</p>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-border">
                      <th className="pb-1.5 text-left font-medium text-muted-foreground">Source</th>
                      <th className="pb-1.5 text-left font-medium text-muted-foreground">→</th>
                      <th className="pb-1.5 text-left font-medium text-muted-foreground">Destination</th>
                      <th className="pb-1.5 text-left font-medium text-muted-foreground">Protocol</th>
                      <th className="pb-1.5 text-right font-medium text-muted-foreground">Bytes</th>
                      <th className="pb-1.5 text-right font-medium text-muted-foreground hidden sm:table-cell">Packets</th>
                      <th className="pb-1.5 text-right font-medium text-muted-foreground hidden sm:table-cell">Flows</th>
                    </tr>
                  </thead>
                  <tbody>
                    {results.map((e) => (
                      <tr key={`${e.src_ip}-${e.src_port}-${e.dst_ip}-${e.dst_port}-${e.protocol}`} className="border-b border-border-soft last:border-0 hover:bg-accent/60">
                        <td className="py-1 font-ip text-xs max-w-[260px]">
                          <Link to={`/ip/${e.src_ip}`} className="text-primary hover:underline">
                            <IPWithPTR ip={e.src_ip} />
                          </Link>
                          {e.src_port > 0 && <span className="text-muted-foreground">:{e.src_port}</span>}
                          {e.src_as > 0 && (
                            <span className="block text-xs text-muted-foreground">
                              <Link to={`/as/${e.src_as}`} className="hover:underline">AS{e.src_as}</Link>
                            </span>
                          )}
                        </td>
                        <td className="py-1 text-muted-foreground">→</td>
                        <td className="py-1 font-ip text-xs max-w-[260px]">
                          <Link to={`/ip/${e.dst_ip}`} className="text-primary hover:underline">
                            <IPWithPTR ip={e.dst_ip} />
                          </Link>
                          {e.dst_port > 0 && <span className="text-muted-foreground">:{e.dst_port}</span>}
                          {e.dst_as > 0 && (
                            <span className="block text-xs text-muted-foreground">
                              <Link to={`/as/${e.dst_as}`} className="hover:underline">AS{e.dst_as}</Link>
                            </span>
                          )}
                        </td>
                        <td className="py-1">
                          <span className="inline-flex items-center gap-1">
                            <ProtocolBadge protocol={e.protocol_name || String(e.protocol)} />
                            {e.service && (
                              <span className="text-xs text-muted-foreground">{e.service}</span>
                            )}
                          </span>
                        </td>
                        <td className="py-1 text-right">{formatBytes(e.bytes)}</td>
                        <td className="py-1 text-right text-muted-foreground hidden sm:table-cell">{formatNumber(e.packets)}</td>
                        <td className="py-1 text-right text-muted-foreground hidden sm:table-cell">{formatNumber(e.flow_count)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
            {isLoading && (
              <button
                type="button"
                onClick={() => refetch()}
                className="mt-2 text-xs text-muted-foreground hover:text-foreground"
              >
                Refresh
              </button>
            )}
          </CardContent>
        </Card>
      )}
    </div>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="flex items-center gap-2">
      <span className="text-xs text-muted-foreground w-16 shrink-0">{label}</span>
      {children}
    </label>
  )
}

// Protocol color coding — security-relevant highlights
function ProtocolBadge({ protocol }: { protocol: string }) {
  const risky = ["Telnet", "FTP", "HTTP"]
  const admin = ["SSH", "RDP", "VNC"]
  const encrypted = ["HTTPS", "SSH", "TLS", "DoT"]
  const common = ["TCP", "UDP", "ICMP", "DNS", "NTP"]

  let color = "bg-muted/50 text-muted-foreground"
  if (risky.some(r => protocol.includes(r))) color = "bg-destructive/20 text-destructive"
  else if (admin.some(r => protocol.includes(r))) color = "bg-blue-500/20 text-blue-500"
  else if (encrypted.some(r => protocol.includes(r))) color = "bg-success/20 text-success"
  else if (common.includes(protocol)) color = "bg-muted/50 text-foreground"

  return (
    <span className={`px-1.5 py-0.5 text-xs rounded ${color}`}>
      {protocol}
    </span>
  )
}
