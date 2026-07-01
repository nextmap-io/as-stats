import { useMemo } from "react"
import { Link } from "react-router-dom"
import { useConversations } from "@/hooks/useApi"
import { useFilters } from "@/hooks/useFilters"
import { useFeatureFlags } from "@/hooks/useFeatures"
import { useUnit } from "@/hooks/useUnit"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { QueryBoundary } from "@/components/QueryBoundary"
import { DataTable, PercentBar, type Column } from "@/components/DataTable"
import { ExportButton, type ExportColumn } from "@/components/ExportButton"
import { IPWithPTR } from "@/components/PTR"
import { formatNumber } from "@/lib/utils"
import { cn } from "@/lib/utils"
import { Search } from "lucide-react"
import type { Conversation } from "@/lib/types"

/** Conversation grouping dimensions (F3). Mirrors store.convDimensions. */
type Dim = "src_dst_ip" | "src_dst_as" | "dst_port_proto"

const DIMS: { value: Dim; label: string }[] = [
  { value: "src_dst_ip", label: "IP ↔ IP" },
  { value: "src_dst_as", label: "AS ↔ AS" },
  { value: "dst_port_proto", label: "Port + Proto" },
]

function asDim(v: string | undefined): Dim {
  return v === "src_dst_as" || v === "dst_port_proto" ? v : "src_dst_ip"
}

const PROTO_NAMES: Record<string, string> = {
  "1": "ICMP",
  "6": "TCP",
  "17": "UDP",
  "47": "GRE",
  "50": "ESP",
  "58": "ICMPv6",
  "89": "OSPF",
  "132": "SCTP",
}

function protoLabel(proto: string): string {
  return PROTO_NAMES[proto] ? `${PROTO_NAMES[proto]} (${proto})` : `Proto ${proto}`
}

export function Conversations() {
  const { filters, setFilter, periodSeconds, filterSearch } = useFilters()
  const { formatTraffic } = useUnit()
  const features = useFeatureFlags()

  const dim = asDim(filters.dim)
  const query = useConversations({ ...filters, dim, limit: 100 })
  const rows = query.data?.data ?? []
  const totalBytes = rows.reduce((s, r) => s + r.total_bytes, 0)

  // Build a Flow Search drill-down URL scoped to the conversation, when it maps
  // cleanly onto flow filters. Falls back to null for endpoints we can't map.
  const drillTo = useMemo(() => {
    const period = filters.period || "1h"
    return (c: Conversation): string | null => {
      const base = `/flows?period=${period}`
      if (dim === "src_dst_ip") {
        return `${base}&src_ip=${encodeURIComponent(c.endpoint_a)}&dst_ip=${encodeURIComponent(c.endpoint_b)}`
      }
      if (dim === "src_dst_as") {
        return `${base}&src_as=${encodeURIComponent(c.endpoint_a)}&dst_as=${encodeURIComponent(c.endpoint_b)}`
      }
      return `${base}&protocol=${encodeURIComponent(c.endpoint_a)}&dst_port=${encodeURIComponent(c.endpoint_b)}`
    }
  }, [dim, filters.period])

  const columns = useMemo<Column<Conversation>[]>(() => {
    const cols: Column<Conversation>[] = [
      {
        key: "rank",
        header: "#",
        className: "text-muted-foreground w-8",
        render: (_row, i) => i + 1,
      },
    ]

    if (dim === "src_dst_ip") {
      cols.push(
        {
          key: "endpoint_a",
          header: "Endpoint A",
          sortable: true,
          className: "max-w-[260px]",
          render: (c) => (
            <Link to={`/ip/${c.endpoint_a}${filterSearch}`} className="text-primary hover:underline font-mono text-[11px] block truncate">
              <IPWithPTR ip={c.endpoint_a} />
            </Link>
          ),
        },
        {
          key: "endpoint_b",
          header: "Endpoint B",
          sortable: true,
          className: "max-w-[260px]",
          render: (c) => (
            <Link to={`/ip/${c.endpoint_b}${filterSearch}`} className="text-primary hover:underline font-mono text-[11px] block truncate">
              <IPWithPTR ip={c.endpoint_b} />
            </Link>
          ),
        },
      )
    } else if (dim === "src_dst_as") {
      cols.push(
        {
          key: "endpoint_a",
          header: "AS A",
          sortable: true,
          render: (c) => (
            <Link to={`/as/${c.endpoint_a}${filterSearch}`} className="text-primary hover:underline font-mono">
              AS{c.endpoint_a}
            </Link>
          ),
        },
        {
          key: "endpoint_b",
          header: "AS B",
          sortable: true,
          render: (c) => (
            <Link to={`/as/${c.endpoint_b}${filterSearch}`} className="text-primary hover:underline font-mono">
              AS{c.endpoint_b}
            </Link>
          ),
        },
      )
    } else {
      cols.push(
        {
          key: "endpoint_a",
          header: "Protocol",
          sortable: true,
          className: "font-mono text-[11px]",
          render: (c) => protoLabel(c.endpoint_a),
        },
        {
          key: "endpoint_b",
          header: "Dst Port",
          sortable: true,
          numeric: true,
          render: (c) => c.endpoint_b,
        },
      )
    }

    cols.push({
      key: "total_bytes",
      header: "Total",
      align: "right",
      numeric: true,
      sortable: true,
      render: (c) => formatTraffic(c.total_bytes, periodSeconds),
    })

    if (dim === "dst_port_proto") {
      cols.push({
        key: "total_packets",
        header: "Packets",
        align: "right",
        numeric: true,
        sortable: true,
        hideOnMobile: true,
        className: "text-muted-foreground",
        render: (c) => formatNumber(c.total_packets),
      })
    } else {
      cols.push(
        {
          key: "forward_bytes",
          header: "A → B",
          align: "right",
          numeric: true,
          sortable: true,
          hideOnMobile: true,
          className: "text-traffic-out",
          render: (c) => formatTraffic(c.forward_bytes, periodSeconds),
        },
        {
          key: "reverse_bytes",
          header: "B → A",
          align: "right",
          numeric: true,
          sortable: true,
          hideOnMobile: true,
          className: "text-traffic-in",
          render: (c) => formatTraffic(c.reverse_bytes, periodSeconds),
        },
      )
    }

    cols.push(
      {
        key: "flows",
        header: "Flows",
        align: "right",
        numeric: true,
        sortable: true,
        hideOnMobile: true,
        className: "text-muted-foreground",
        render: (c) => formatNumber(c.flows),
      },
      {
        key: "pct",
        header: "%",
        align: "right",
        sortable: true,
        sortValue: (c) => c.total_bytes,
        render: (c) => <PercentBar pct={totalBytes > 0 ? (c.total_bytes / totalBytes) * 100 : 0} />,
      },
      {
        key: "drill",
        header: "",
        align: "center",
        render: (c) => {
          const to = drillTo(c)
          return to ? (
            <Link
              to={to}
              className="inline-flex items-center text-muted-foreground hover:text-primary transition-colors"
              title="Open in Flow Search"
              aria-label="Open in Flow Search"
            >
              <Search className="size-3.5" />
            </Link>
          ) : null
        },
      },
    )

    return cols
  }, [dim, filterSearch, formatTraffic, periodSeconds, totalBytes, drillTo])

  const exportColumns: ExportColumn<Conversation>[] = [
    { key: "endpoint_a", header: "Endpoint A", value: (r) => r.endpoint_a },
    { key: "endpoint_b", header: "Endpoint B", value: (r) => r.endpoint_b },
    { key: "total_bytes", header: "Total Bytes", value: (r) => r.total_bytes },
    { key: "forward_bytes", header: "Forward Bytes", value: (r) => r.forward_bytes },
    { key: "reverse_bytes", header: "Reverse Bytes", value: (r) => r.reverse_bytes },
    { key: "total_packets", header: "Total Packets", value: (r) => r.total_packets },
    { key: "flows", header: "Flows", value: (r) => r.flows },
  ]

  if (!features.flow_search) {
    return (
      <div className="space-y-6">
        <h1 className="text-lg font-semibold tracking-tight">Conversations</h1>
        <Card>
          <CardContent className="py-10 text-center text-sm text-muted-foreground">
            The Conversations explorer requires the flow-search feature
            (<code className="font-mono">FEATURE_FLOW_SEARCH</code>) to be enabled.
          </CardContent>
        </Card>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div className="flex items-baseline justify-between">
        <h1 className="text-lg font-semibold tracking-tight">Conversations</h1>
        <span className="text-[10px] text-muted-foreground">
          Bidirectional top talkers — A↔B folded into one row
        </span>
      </div>

      <Card>
        <CardHeader className="pb-3">
          <div className="flex items-center justify-between gap-2">
            <CardTitle className="text-sm">Top conversations</CardTitle>
            <div className="flex items-center gap-2">
              <div
                className="flex gap-0.5 rounded border border-input bg-muted/30 p-0.5"
                role="group"
                aria-label="Conversation dimension"
              >
                {DIMS.map((d) => (
                  <button
                    key={d.value}
                    type="button"
                    onClick={() => setFilter("dim", d.value === "src_dst_ip" ? undefined : d.value)}
                    aria-pressed={dim === d.value}
                    className={cn(
                      "px-2 py-0.5 text-[11px] font-medium rounded transition-colors",
                      dim === d.value
                        ? "bg-primary text-primary-foreground"
                        : "text-muted-foreground hover:text-foreground hover:bg-accent",
                    )}
                  >
                    {d.label}
                  </button>
                ))}
              </div>
              <ExportButton rows={rows} columns={exportColumns} filename={`conversations-${dim}`} />
            </div>
          </div>
        </CardHeader>
        <CardContent>
          <QueryBoundary query={query} isEmpty={(d) => d.data.length === 0} loadingCols={8}>
            {(data) => (
              <DataTable
                rows={data.data}
                columns={columns}
                rowKey={(c) => `${c.endpoint_a}-${c.endpoint_b}`}
                rowClassName="border-border/40"
              />
            )}
          </QueryBoundary>
        </CardContent>
      </Card>
    </div>
  )
}
