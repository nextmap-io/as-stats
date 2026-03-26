import { Link } from "react-router-dom"
import { useOverview } from "@/hooks/useApi"
import { useFilters } from "@/hooks/useFilters"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { formatBytes, formatNumber } from "@/lib/utils"
import { ArrowDownLeft, ArrowUpRight, Layers, Network } from "lucide-react"
import type { ASTraffic, IPTraffic, LinkTraffic } from "@/lib/types"

export function Dashboard() {
  const { filters } = useFilters()
  const { data, isLoading, error } = useOverview(filters)

  if (isLoading) return <LoadingSkeleton />
  if (error) return <ErrorMessage error={error} />

  const overview = data?.data
  if (!overview) return null

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold tracking-tight">Dashboard</h1>

      {/* Summary cards */}
      <div className="grid gap-4 md:grid-cols-4">
        <StatCard
          title="Inbound"
          value={formatBytes(overview.total_bytes_in)}
          icon={<ArrowDownLeft className="h-4 w-4 text-emerald-500" />}
        />
        <StatCard
          title="Outbound"
          value={formatBytes(overview.total_bytes_out)}
          icon={<ArrowUpRight className="h-4 w-4 text-orange-500" />}
        />
        <StatCard
          title="Active ASes"
          value={formatNumber(overview.active_as_count)}
          icon={<Network className="h-4 w-4 text-blue-500" />}
        />
        <StatCard
          title="Total Flows"
          value={formatNumber(overview.total_flows)}
          icon={<Layers className="h-4 w-4 text-purple-500" />}
        />
      </div>

      {/* Top AS and Top IP side by side */}
      <div className="grid gap-6 lg:grid-cols-2">
        <Card>
          <CardHeader className="pb-3">
            <div className="flex items-center justify-between">
              <CardTitle className="text-base">Top AS</CardTitle>
              <Link to="/top/as" className="text-xs text-muted-foreground hover:text-foreground">
                View all
              </Link>
            </div>
          </CardHeader>
          <CardContent>
            <TopASTable data={overview.top_as} />
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-3">
            <div className="flex items-center justify-between">
              <CardTitle className="text-base">Top IP</CardTitle>
              <Link to="/top/ip" className="text-xs text-muted-foreground hover:text-foreground">
                View all
              </Link>
            </div>
          </CardHeader>
          <CardContent>
            <TopIPTable data={overview.top_ip} />
          </CardContent>
        </Card>
      </div>

      {/* Links */}
      {overview.links.length > 0 && (
        <Card>
          <CardHeader className="pb-3">
            <div className="flex items-center justify-between">
              <CardTitle className="text-base">Links</CardTitle>
              <Link to="/links" className="text-xs text-muted-foreground hover:text-foreground">
                View all
              </Link>
            </div>
          </CardHeader>
          <CardContent>
            <LinksTable data={overview.links} />
          </CardContent>
        </Card>
      )}
    </div>
  )
}

function StatCard({ title, value, icon }: { title: string; value: string; icon: React.ReactNode }) {
  return (
    <Card>
      <CardContent className="p-4">
        <div className="flex items-center justify-between">
          <p className="text-sm font-medium text-muted-foreground">{title}</p>
          {icon}
        </div>
        <p className="mt-1 text-2xl font-bold">{value}</p>
      </CardContent>
    </Card>
  )
}

function TopASTable({ data }: { data: ASTraffic[] }) {
  if (!data?.length) return <p className="text-sm text-muted-foreground">No data</p>

  return (
    <table className="w-full text-sm">
      <thead>
        <tr className="border-b border-border">
          <th className="pb-2 text-left font-medium text-muted-foreground">ASN</th>
          <th className="pb-2 text-left font-medium text-muted-foreground">Name</th>
          <th className="pb-2 text-right font-medium text-muted-foreground">Traffic</th>
          <th className="pb-2 text-right font-medium text-muted-foreground">%</th>
        </tr>
      </thead>
      <tbody>
        {data.map(as => (
          <tr key={as.as_number} className="border-b border-border/50 last:border-0">
            <td className="py-1.5">
              <Link to={`/as/${as.as_number}`} className="text-primary hover:underline font-mono">
                {as.as_number}
              </Link>
            </td>
            <td className="py-1.5 truncate max-w-48">{as.as_name || "-"}</td>
            <td className="py-1.5 text-right font-mono">{formatBytes(as.bytes)}</td>
            <td className="py-1.5 text-right font-mono text-muted-foreground">{as.pct?.toFixed(1)}%</td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}

function TopIPTable({ data }: { data: IPTraffic[] }) {
  if (!data?.length) return <p className="text-sm text-muted-foreground">No data</p>

  return (
    <table className="w-full text-sm">
      <thead>
        <tr className="border-b border-border">
          <th className="pb-2 text-left font-medium text-muted-foreground">IP</th>
          <th className="pb-2 text-left font-medium text-muted-foreground">AS</th>
          <th className="pb-2 text-right font-medium text-muted-foreground">Traffic</th>
        </tr>
      </thead>
      <tbody>
        {data.map(ip => (
          <tr key={ip.ip} className="border-b border-border/50 last:border-0">
            <td className="py-1.5">
              <Link to={`/ip/${ip.ip}`} className="text-primary hover:underline font-mono text-xs">
                {ip.ip}
              </Link>
            </td>
            <td className="py-1.5">
              {ip.as_number > 0 ? (
                <Link to={`/as/${ip.as_number}`} className="text-muted-foreground hover:text-foreground">
                  AS{ip.as_number}
                </Link>
              ) : "-"}
            </td>
            <td className="py-1.5 text-right font-mono">{formatBytes(ip.bytes)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}

function LinksTable({ data }: { data: LinkTraffic[] }) {
  return (
    <table className="w-full text-sm">
      <thead>
        <tr className="border-b border-border">
          <th className="pb-2 text-left font-medium text-muted-foreground">Link</th>
          <th className="pb-2 text-left font-medium text-muted-foreground">Description</th>
          <th className="pb-2 text-right font-medium text-muted-foreground">In</th>
          <th className="pb-2 text-right font-medium text-muted-foreground">Out</th>
        </tr>
      </thead>
      <tbody>
        {data.map(l => (
          <tr key={l.tag} className="border-b border-border/50 last:border-0">
            <td className="py-1.5">
              <Link to={`/link/${l.tag}`} className="text-primary hover:underline">
                {l.tag}
              </Link>
            </td>
            <td className="py-1.5 text-muted-foreground truncate max-w-48">{l.description || "-"}</td>
            <td className="py-1.5 text-right font-mono">{formatBytes(l.bytes_in)}</td>
            <td className="py-1.5 text-right font-mono">{formatBytes(l.bytes_out)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}

function LoadingSkeleton() {
  return (
    <div className="space-y-6">
      <div className="h-8 w-48 bg-muted rounded animate-pulse" />
      <div className="grid gap-4 md:grid-cols-4">
        {[...Array(4)].map((_, i) => (
          <div key={i} className="h-24 bg-muted rounded-lg animate-pulse" />
        ))}
      </div>
    </div>
  )
}

function ErrorMessage({ error }: { error: Error }) {
  return (
    <Card>
      <CardContent className="p-6">
        <p className="text-destructive">Error: {error.message}</p>
      </CardContent>
    </Card>
  )
}
