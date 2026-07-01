import { useMemo, useState } from "react"
import { Link } from "react-router-dom"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { useLinks, useLinksTraffic, useLinkColors } from "@/hooks/useApi"
import { useFilters } from "@/hooks/useFilters"
import { api } from "@/lib/api"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { QueryBoundary } from "@/components/QueryBoundary"
import { DataTable, type Column } from "@/components/DataTable"
import { ExportButton, type ExportColumn } from "@/components/ExportButton"
import { LinkTrafficChart } from "@/components/charts/LinkTrafficChart"
import { ExpandableChart } from "@/components/ExpandableChart"
import { useUnit } from "@/hooks/useUnit"
import { Plus, Trash2, BarChart3 } from "lucide-react"
import { EmptyState } from "@/components/ui/error"
import type { LinkTraffic } from "@/lib/types"

export function Links() {
  const { filters, filterSearch, periodSeconds, timeBounds } = useFilters()
  const linksQuery = useLinks(filters)
  const { formatTraffic } = useUnit()
  const { data: ipv4Traffic } = useLinksTraffic(4, filters)
  const { data: ipv6Traffic } = useLinksTraffic(6, filters)
  const linkColors = useLinkColors()

  const linkRows = linksQuery.data?.data ?? []

  const columns = useMemo<Column<LinkTraffic>[]>(() => [
    {
      key: "tag",
      header: "Link",
      sortable: true,
      render: (l) => (
        <Link to={`/link/${l.tag}${filterSearch}`} className="text-primary hover:underline font-medium">
          {l.tag}
        </Link>
      ),
    },
    {
      key: "description",
      header: "Description",
      sortable: true,
      className: "text-muted-foreground truncate max-w-48",
      render: (l) => l.description || "-",
    },
    {
      key: "capacity_mbps",
      header: "Capacity",
      align: "right",
      numeric: true,
      sortable: true,
      className: "text-muted-foreground",
      sortValue: (l) => l.capacity_mbps || 0,
      render: (l) => (l.capacity_mbps ? `${l.capacity_mbps} Mbps` : "-"),
    },
    {
      key: "bytes_in",
      header: "Inbound",
      align: "right",
      numeric: true,
      sortable: true,
      render: (l) => formatTraffic(l.bytes_in, periodSeconds),
    },
    {
      key: "bytes_out",
      header: "Outbound",
      align: "right",
      numeric: true,
      sortable: true,
      render: (l) => formatTraffic(l.bytes_out, periodSeconds),
    },
    {
      key: "total",
      header: "Total",
      align: "right",
      numeric: true,
      sortable: true,
      className: "font-medium",
      sortValue: (l) => l.bytes_in + l.bytes_out,
      render: (l) => formatTraffic(l.bytes_in + l.bytes_out, periodSeconds),
    },
  ], [filterSearch, formatTraffic, periodSeconds])

  const exportColumns: ExportColumn<LinkTraffic>[] = [
    { key: "tag", header: "Link", value: (l) => l.tag },
    { key: "description", header: "Description", value: (l) => l.description },
    { key: "capacity_mbps", header: "Capacity (Mbps)", value: (l) => l.capacity_mbps ?? "" },
    { key: "bytes_in", header: "Bytes In", value: (l) => l.bytes_in },
    { key: "bytes_out", header: "Bytes Out", value: (l) => l.bytes_out },
    { key: "total", header: "Total Bytes", value: (l) => l.bytes_in + l.bytes_out },
  ]

  return (
    <div className="space-y-6">
      <div className="flex items-baseline justify-between">
        <h1 className="text-lg font-semibold tracking-tight">Links</h1>
      </div>

      {/* Traffic charts */}
      <div className="grid gap-4 lg:grid-cols-2">
        <Card className="overflow-visible">
          <CardContent className="px-4 pt-5 pb-4">
            {ipv4Traffic?.data && ipv4Traffic.data.length > 0 ? (
              <ExpandableChart title="IPv4 Traffic by Link">
                <LinkTrafficChart series={ipv4Traffic.data} title="IPv4 Traffic by Link" linkColors={linkColors} timeBounds={timeBounds} />
              </ExpandableChart>
            ) : (
              <EmptyState message="No IPv4 link traffic" icon={<BarChart3 className="size-8" />} />
            )}
          </CardContent>
        </Card>
        <Card className="overflow-visible">
          <CardContent className="px-4 pt-5 pb-4">
            {ipv6Traffic?.data && ipv6Traffic.data.length > 0 ? (
              <ExpandableChart title="IPv6 Traffic by Link">
                <LinkTrafficChart series={ipv6Traffic.data} title="IPv6 Traffic by Link" linkColors={linkColors} timeBounds={timeBounds} />
              </ExpandableChart>
            ) : (
              <EmptyState message="No IPv6 link traffic" icon={<BarChart3 className="size-8" />} />
            )}
          </CardContent>
        </Card>
      </div>

      {/* Links table */}
      <Card>
        <CardHeader className="pb-3">
          <div className="flex items-center justify-between gap-2">
            <CardTitle className="text-base">Known links with traffic</CardTitle>
            <ExportButton rows={linkRows} columns={exportColumns} filename="links" />
          </div>
        </CardHeader>
        <CardContent>
          <QueryBoundary query={linksQuery} isEmpty={(d) => d.data.length === 0} loadingCols={6}>
            {(data) => (
              <DataTable
                rows={data.data}
                columns={columns}
                rowKey={(l) => l.tag}
                tableClassName="text-sm"
              />
            )}
          </QueryBoundary>
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
            <Plus className="size-3" /> Add link
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
                className="size-8 rounded border border-border cursor-pointer p-0"
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
                      className="size-5 rounded cursor-pointer border-0 p-0"
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
                      <Trash2 className="size-3.5" />
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
