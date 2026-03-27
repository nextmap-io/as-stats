import { useState } from "react"
import { Link } from "react-router-dom"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { useLinks, useLinksTraffic, useLinkColors } from "@/hooks/useApi"
import { useFilters } from "@/hooks/useFilters"
import { api } from "@/lib/api"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { LinkTrafficChart } from "@/components/charts/LinkTrafficChart"
import { ExpandableChart } from "@/components/ExpandableChart"
import { useUnit } from "@/hooks/useUnit"
import { Plus, Trash2, BarChart3 } from "lucide-react"
import { EmptyState } from "@/components/ui/error"

export function Links() {
  const { filters, filterSearch, periodSeconds, timeBounds } = useFilters()
  const { data, isLoading, error } = useLinks(filters)
  const { formatTraffic } = useUnit()
  const { data: ipv4Traffic } = useLinksTraffic(4, filters)
  const { data: ipv6Traffic } = useLinksTraffic(6, filters)
  const linkColors = useLinkColors()

  return (
    <div className="space-y-6">
      <div className="flex items-baseline justify-between">
        <h1 className="text-lg font-semibold tracking-tight">Links</h1>
      </div>

      {/* Traffic charts */}
      <div className="grid gap-4 lg:grid-cols-2">
        <Card className="overflow-visible">
          <CardContent className="pt-5 pb-8">
            {ipv4Traffic?.data && ipv4Traffic.data.length > 0 ? (
              <ExpandableChart title="IPv4 Traffic by Link">
                <LinkTrafficChart series={ipv4Traffic.data} title="IPv4 Traffic by Link" linkColors={linkColors} />
              </ExpandableChart>
            ) : (
              <EmptyState message="No IPv4 link traffic" icon={<BarChart3 className="h-8 w-8" />} />
            )}
          </CardContent>
        </Card>
        <Card className="overflow-visible">
          <CardContent className="pt-5 pb-8">
            {ipv6Traffic?.data && ipv6Traffic.data.length > 0 ? (
              <ExpandableChart title="IPv6 Traffic by Link">
                <LinkTrafficChart series={ipv6Traffic.data} title="IPv6 Traffic by Link" linkColors={linkColors} />
              </ExpandableChart>
            ) : (
              <EmptyState message="No IPv6 link traffic" icon={<BarChart3 className="h-8 w-8" />} />
            )}
          </CardContent>
        </Card>
      </div>

      {/* Links table */}
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base">Known links with traffic</CardTitle>
        </CardHeader>
        <CardContent>
          {isLoading && <p className="text-muted-foreground">Loading...</p>}
          {error && <p className="text-destructive">{error.message}</p>}
          {data?.data && (
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border">
                  <th className="pb-2 text-left font-medium text-muted-foreground">Link</th>
                  <th className="pb-2 text-left font-medium text-muted-foreground">Description</th>
                  <th className="pb-2 text-right font-medium text-muted-foreground">Capacity</th>
                  <th className="pb-2 text-right font-medium text-muted-foreground">Inbound</th>
                  <th className="pb-2 text-right font-medium text-muted-foreground">Outbound</th>
                  <th className="pb-2 text-right font-medium text-muted-foreground">Total</th>
                </tr>
              </thead>
              <tbody>
                {data.data.map(l => (
                  <tr key={l.tag} className="border-b border-border/50 last:border-0 hover:bg-muted/50">
                    <td className="py-2">
                      <Link to={`/link/${l.tag}${filterSearch}`} className="text-primary hover:underline font-medium">
                        {l.tag}
                      </Link>
                    </td>
                    <td className="py-2 text-muted-foreground truncate max-w-48">{l.description || "-"}</td>
                    <td className="py-2 text-right font-mono text-muted-foreground">
                      {l.capacity_mbps ? `${l.capacity_mbps} Mbps` : "-"}
                    </td>
                    <td className="py-2 text-right font-mono">{formatTraffic(l.bytes_in, periodSeconds)}</td>
                    <td className="py-2 text-right font-mono">{formatTraffic(l.bytes_out, periodSeconds)}</td>
                    <td className="py-2 text-right font-mono font-medium">
                      {formatTraffic(l.bytes_in + l.bytes_out, periodSeconds)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </CardContent>
      </Card>

      {/* Link management */}
      <LinkManager />
    </div>
  )
}

function LinkManager() {
  const queryClient = useQueryClient()
  const { data } = useQuery({
    queryKey: ["admin-links"],
    queryFn: () => api.adminLinks(),
  })

  const [showForm, setShowForm] = useState(false)
  const [form, setForm] = useState({ tag: "", router_ip: "", snmp_index: "", description: "", capacity_mbps: "", color: "#e74c3c" })

  const createMutation = useMutation({
    mutationFn: (link: Record<string, unknown>) => api.createLink(link),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["admin-links"] })
      queryClient.invalidateQueries({ queryKey: ["links"] })
      setForm({ tag: "", router_ip: "", snmp_index: "", description: "", capacity_mbps: "", color: "#e74c3c" })
      setShowForm(false)
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (tag: string) => api.deleteLink(tag),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["admin-links"] })
      queryClient.invalidateQueries({ queryKey: ["links"] })
    },
  })

  // Update a link (re-create with same data + new color)
  const updateColorMutation = useMutation({
    mutationFn: (link: Record<string, unknown>) => api.createLink(link),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["admin-links"] })
    },
  })

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    createMutation.mutate({
      tag: form.tag,
      router_ip: form.router_ip,
      snmp_index: Number(form.snmp_index) || 0,
      description: form.description,
      capacity_mbps: Number(form.capacity_mbps) || 0,
      color: form.color,
    })
  }

  return (
    <Card>
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          <CardTitle className="text-base">Link Configuration</CardTitle>
          <button
            onClick={() => setShowForm(!showForm)}
            className="text-xs text-primary hover:underline inline-flex items-center gap-1"
          >
            <Plus className="h-3 w-3" /> Add link
          </button>
        </div>
      </CardHeader>
      <CardContent>
        {showForm && (
          <form onSubmit={handleSubmit} className="grid grid-cols-2 md:grid-cols-5 gap-2 mb-4 p-3 bg-muted/50 rounded">
            <input
              placeholder="Tag (e.g. transit-cogent)"
              value={form.tag}
              onChange={e => setForm(f => ({ ...f, tag: e.target.value }))}
              required
              className="bg-background border border-border rounded px-2 py-1.5 text-xs"
            />
            <input
              placeholder="Router IP"
              value={form.router_ip}
              onChange={e => setForm(f => ({ ...f, router_ip: e.target.value }))}
              required
              className="bg-background border border-border rounded px-2 py-1.5 text-xs"
            />
            <input
              placeholder="SNMP index"
              type="number"
              value={form.snmp_index}
              onChange={e => setForm(f => ({ ...f, snmp_index: e.target.value }))}
              required
              className="bg-background border border-border rounded px-2 py-1.5 text-xs"
            />
            <input
              placeholder="Description"
              value={form.description}
              onChange={e => setForm(f => ({ ...f, description: e.target.value }))}
              className="bg-background border border-border rounded px-2 py-1.5 text-xs"
            />
            <div className="flex gap-2">
              <input
                placeholder="Mbps"
                type="number"
                value={form.capacity_mbps}
                onChange={e => setForm(f => ({ ...f, capacity_mbps: e.target.value }))}
                className="bg-background border border-border rounded px-2 py-1.5 text-xs flex-1"
              />
              <input
                type="color"
                value={form.color}
                onChange={e => setForm(f => ({ ...f, color: e.target.value }))}
                className="w-8 h-8 rounded border border-border cursor-pointer p-0"
                title="Link color"
              />
              <button
                type="submit"
                disabled={createMutation.isPending}
                className="bg-primary text-primary-foreground px-3 py-1.5 rounded text-xs font-medium hover:opacity-90 disabled:opacity-50"
              >
                {createMutation.isPending ? "..." : "Add"}
              </button>
            </div>
            {createMutation.isError && (
              <p className="col-span-full text-xs text-destructive">{(createMutation.error as Error).message}</p>
            )}
          </form>
        )}

        {data?.data && data.data.length > 0 ? (
          <table className="w-full text-xs">
            <thead>
              <tr className="border-b border-border">
                <th className="pb-2 w-6"></th>
                <th className="pb-2 text-left font-medium text-muted-foreground">Tag</th>
                <th className="pb-2 text-left font-medium text-muted-foreground">Router IP</th>
                <th className="pb-2 text-right font-medium text-muted-foreground">SNMP Index</th>
                <th className="pb-2 text-left font-medium text-muted-foreground">Description</th>
                <th className="pb-2 text-right font-medium text-muted-foreground">Capacity</th>
                <th className="pb-2 text-right font-medium text-muted-foreground"></th>
              </tr>
            </thead>
            <tbody>
              {data.data.map((l) => (
                <tr key={l.tag} className="border-b border-border/50 last:border-0 hover:bg-muted/50">
                  <td className="py-1.5">
                    <input
                      type="color"
                      defaultValue={l.color || "#e74c3c"}
                      onBlur={e => {
                        if (e.target.value !== (l.color || "#e74c3c")) {
                          updateColorMutation.mutate({
                            tag: l.tag, router_ip: l.router_ip,
                            snmp_index: l.snmp_index, description: l.description,
                            capacity_mbps: l.capacity_mbps, color: e.target.value,
                          })
                        }
                      }}
                      className="w-5 h-5 rounded cursor-pointer border-0 p-0"
                      title="Change link color"
                    />
                  </td>
                  <td className="py-1.5 font-medium">{l.tag}</td>
                  <td className="py-1.5 font-mono text-muted-foreground">{l.router_ip}</td>
                  <td className="py-1.5 text-right font-mono">{l.snmp_index}</td>
                  <td className="py-1.5 text-muted-foreground truncate max-w-48">{l.description || "-"}</td>
                  <td className="py-1.5 text-right font-mono">{l.capacity_mbps ? `${l.capacity_mbps} Mbps` : "-"}</td>
                  <td className="py-1.5 text-right">
                    <button
                      onClick={() => {
                        if (confirm(`Delete link "${l.tag}"?`)) {
                          deleteMutation.mutate(l.tag)
                        }
                      }}
                      className="text-muted-foreground hover:text-destructive transition-colors"
                      title="Delete link"
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : (
          <p className="text-xs text-muted-foreground">No links configured. Add a link to map router interfaces to named links.</p>
        )}
      </CardContent>
    </Card>
  )
}
