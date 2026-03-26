import { useParams } from "react-router-dom"
import { useIPDetail } from "@/hooks/useApi"
import { useFilters } from "@/hooks/useFilters"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { TrafficChart } from "@/components/charts/TrafficChart"

export function IPDetail() {
  const { ip } = useParams<{ ip: string }>()
  const { filters } = useFilters()
  const { data, isLoading, error } = useIPDetail(ip || "", filters)

  if (isLoading) return <p className="text-muted-foreground">Loading...</p>
  if (error) return <p className="text-destructive">{error.message}</p>

  const detail = data?.data
  if (!detail) return null

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold tracking-tight font-mono">{detail.ip}</h1>

      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base">Traffic</CardTitle>
        </CardHeader>
        <CardContent>
          {detail.time_series?.length > 0 ? (
            <TrafficChart data={detail.time_series} height={350} />
          ) : (
            <p className="text-sm text-muted-foreground">No traffic data for this period</p>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
