import { Link } from "react-router-dom"
import { useLinks } from "@/hooks/useApi"
import { useFilters } from "@/hooks/useFilters"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { formatBytes } from "@/lib/utils"
import type { LinkTraffic } from "@/lib/types"

export function Links() {
  const { filters } = useFilters()
  const { data, isLoading, error } = useLinks(filters)

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold tracking-tight">Links</h1>

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
                {(data.data as LinkTraffic[]).map(l => (
                  <tr key={l.tag} className="border-b border-border/50 last:border-0 hover:bg-muted/50">
                    <td className="py-2">
                      <Link to={`/link/${l.tag}`} className="text-primary hover:underline font-medium">
                        {l.tag}
                      </Link>
                    </td>
                    <td className="py-2 text-muted-foreground truncate max-w-48">{l.description || "-"}</td>
                    <td className="py-2 text-right font-mono text-muted-foreground">
                      {l.capacity_mbps ? `${l.capacity_mbps} Mbps` : "-"}
                    </td>
                    <td className="py-2 text-right font-mono">{formatBytes(l.bytes_in)}</td>
                    <td className="py-2 text-right font-mono">{formatBytes(l.bytes_out)}</td>
                    <td className="py-2 text-right font-mono font-medium">
                      {formatBytes(l.bytes_in + l.bytes_out)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
