import { useMemo, useState } from "react"
import { Link, useSearchParams } from "react-router-dom"
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
import { stepSecondsOf } from "@/lib/rate"
import { seriesColor, useChartColors } from "@/hooks/useChartColors"
import { Plus, Trash2 } from "lucide-react"
import { NoDataInWindow } from "@/components/ui/error"
import { Segmented } from "@/components/ui/segmented"
import { IP_FAMILY_OPTIONS, mergeLinkSeries, parseIPFamily, type IPFamily } from "@/lib/series"
import type { LinkTraffic } from "@/lib/types"
import { formatCapacity } from "@/lib/format"

export function Links() {
  const { filters, filterSearch, periodSeconds, timeBounds } = useFilters()
  const linksQuery = useLinks(filters)
  const { formatTraffic } = useUnit()
  const { data: ipv4Traffic } = useLinksTraffic(4, filters)
  const { data: ipv6Traffic } = useLinksTraffic(6, filters)
  const linkColors = useLinkColors()
  const step = stepSecondsOf(ipv4Traffic?.meta ?? ipv6Traffic?.meta, periodSeconds)
  const [searchParams, setSearchParams] = useSearchParams()
  const family = parseIPFamily(searchParams.get("ipv"))
  const setFamily = (v: IPFamily) =>
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        if (v === "both") next.delete("ipv")
        else next.set("ipv", v)
        return next
      },
      { replace: true },
    )
  const familySeries = useMemo(
    () =>
      family === "4"
        ? (ipv4Traffic?.data ?? [])
        : family === "6"
          ? (ipv6Traffic?.data ?? [])
          : mergeLinkSeries(ipv4Traffic?.data, ipv6Traffic?.data),
    [family, ipv4Traffic, ipv6Traffic],
  )
  const familyLabel = family === "both" ? "" : `, IPv${family}`

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
      render: (l) => (l.capacity_mbps ? formatCapacity(l.capacity_mbps * 1e6) : "-"),
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
      <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-2">
        <h1 className="text-2xl font-semibold">Links</h1>
      </div>

      {/* One stacked chart; the address family is a switch, not a second chart
          on its own scale (Ledger). */}
      <Card className="overflow-visible">
        <CardHeader>
          <div className="flex flex-wrap items-center justify-between gap-2">
            <CardTitle>Traffic by link</CardTitle>
            <Segmented label="IP version" options={IP_FAMILY_OPTIONS} value={family} onChange={setFamily} />
          </div>
        </CardHeader>
        <CardContent>
          {familySeries.length > 0 ? (
            <ExpandableChart title={`Traffic by link${familyLabel}`}>
              <LinkTrafficChart series={familySeries} linkColors={linkColors} timeBounds={timeBounds} stepSeconds={step} />
            </ExpandableChart>
          ) : (
            <NoDataInWindow hint={`No ${familyLabel ? `${familyLabel.slice(2)} ` : ""}traffic was credited to a configured link in this window.`} />
          )}
        </CardContent>
      </Card>

      {/* Links table */}
      <Card>
        <CardHeader>
          <div className="flex flex-wrap items-center justify-between gap-2">
            <CardTitle>Known links with traffic</CardTitle>
            <ExportButton rows={linkRows} columns={exportColumns} filename="links" />
          </div>
        </CardHeader>
        <CardContent>
          <QueryBoundary query={linksQuery} isEmpty={(d) => !d.data?.length} loadingCols={6}>
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

// A new link starts on the first series colour (cobalt); operators can pick any.
const DEFAULT_NEW_LINK_COLOR = "#2f55d4"

function LinkManager() {
  const queryClient = useQueryClient()
  const chartColors = useChartColors()
  const { data } = useQuery({
    queryKey: ["admin-links"],
    queryFn: () => api.adminLinks(),
  })

  const [showForm, setShowForm] = useState(false)
  const [form, setForm] = useState({ tag: "", router_ip: "", snmp_index: "", description: "", capacity_mbps: "", color: DEFAULT_NEW_LINK_COLOR })

  const createMutation = useMutation({
    mutationFn: (link: Record<string, unknown>) => api.createLink(link),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["admin-links"] })
      queryClient.invalidateQueries({ queryKey: ["links"] })
      setForm({ tag: "", router_ip: "", snmp_index: "", description: "", capacity_mbps: "", color: DEFAULT_NEW_LINK_COLOR })
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
      <CardHeader>
        <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
          <CardTitle>Link configuration</CardTitle>
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
          <div className="overflow-x-auto"><table className="w-full min-w-[560px] text-sm">
            <thead>
              <tr className="border-b border-foreground">
                <th className="pb-2 w-6"></th>
                <th className="pb-2 text-left text-[13px] font-normal text-muted-foreground">Tag</th>
                <th className="pb-2 text-left text-[13px] font-normal text-muted-foreground">Router IP</th>
                <th className="pb-2 text-right text-[13px] font-normal text-muted-foreground">SNMP index</th>
                <th className="pb-2 pl-6 text-left text-[13px] font-normal text-muted-foreground">Description</th>
                <th className="pb-2 text-right text-[13px] font-normal text-muted-foreground">Capacity</th>
                <th className="pb-2 text-right text-[13px] font-normal text-muted-foreground"></th>
              </tr>
            </thead>
            <tbody>
              {data.data.map((l, i) => (
                <tr key={l.tag} className="border-b border-border-soft last:border-0 hover:bg-accent/60">
                  <td className="py-1.5 pr-2">
                    <input
                      type="color"
                      defaultValue={l.color || seriesColor(chartColors, i)}
                      onBlur={e => {
                        if (e.target.value !== (l.color || seriesColor(chartColors, i))) {
                          updateColorMutation.mutate({
                            tag: l.tag, router_ip: l.router_ip,
                            snmp_index: l.snmp_index, description: l.description,
                            capacity_mbps: l.capacity_mbps, color: e.target.value,
                          })
                        }
                      }}
                      className="size-6 rounded cursor-pointer border-0 p-0"
                      title={l.color ? "Link colour" : "Automatic colour from the theme. Pick one to fix it."}
                      aria-label={`Colour for ${l.tag}`}
                    />
                  </td>
                  <td className="py-1.5 font-medium">{l.tag}</td>
                  <td className="py-1.5 font-ip text-muted-foreground">{l.router_ip}</td>
                  <td className="py-1.5 text-right">{l.snmp_index}</td>
                  <td className="py-1.5 pl-6 text-muted-foreground truncate max-w-48">{l.description || "—"}</td>
                  <td className="py-1.5 text-right">{l.capacity_mbps ? formatCapacity(l.capacity_mbps * 1e6) : "—"}</td>
                  <td className="py-1.5 text-right">
                    <button
                      onClick={() => {
                        if (confirm(`Delete link "${l.tag}"?`)) {
                          deleteMutation.mutate(l.tag)
                        }
                      }}
                      className="inline-flex size-7 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-accent hover:text-destructive"
                      title="Delete link"
                      aria-label={`Delete link ${l.tag}`}
                    >
                      <Trash2 className="size-3.5" aria-hidden />
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table></div>
        ) : (
          <p className="text-xs text-muted-foreground">No links configured. Add a link to map router interfaces to named links.</p>
        )}
      </CardContent>
    </Card>
  )
}
