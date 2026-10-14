import { Link } from "react-router-dom"
import { useFilters } from "@/hooks/useFilters"
import { ErrorDisplay } from "@/components/ui/error"
import { ChartSkeleton, TableSkeleton } from "@/components/ui/skeleton"
import { Section } from "@/components/home/Section"
import { StatusStrip } from "@/components/home/StatusStrip"
import { TotalTraffic } from "@/components/home/TotalTraffic"
import { LinkTable } from "@/components/home/LinkTable"
import { TopNetworks } from "@/components/home/TopNetworks"
import { FurtherAnalysis } from "@/components/home/FurtherAnalysis"
import { useHomeData } from "@/components/home/useHomeData"

/**
 * The home page is ordered by the operator's questions: is anything wrong,
 * how much traffic and where is it going, are the links OK, who is it with.
 * Pattern views (heatmap, per-AS chart walls, stacked link charts) live on
 * their own pages; the footer points to them.
 */
export function Dashboard() {
  const { filterSearch } = useFilters()
  const home = useHomeData()
  const ownIn = home.total.both.reduce((s, p) => s + p.bytes_in, 0)
  const ownOut = home.total.both.reduce((s, p) => s + p.bytes_out, 0)

  return (
    <div>
      <h1 className="sr-only">Dashboard</h1>

      <Section id="h-status" title="Right now" note={"Only what needs attention is highlighted. Refreshed every 30\u00a0s."} first>
        <StatusStrip links={home.links} linksLoading={home.capacityLoading || home.seriesLoading} />
      </Section>

      <Section
        id="h-traffic"
        title="Total traffic"
        note="All links. Inbound above the line, outbound below, on one scale."
      >
        {home.seriesLoading ? (
          <ChartSkeleton height={280} />
        ) : home.seriesError ? (
          <ErrorDisplay error={home.seriesError} onRetry={home.refetchSeries} />
        ) : (
          <TotalTraffic total={home.total} step={home.step} />
        )}
      </Section>

      <Section
        id="h-links"
        title="Links"
        note="Utilisation is the busier direction against capacity. The bar is now, the tick is the peak in this window, the dashed line is 70%."
        aside={
          <Link to={`/capacity${filterSearch}`} className="text-foreground-secondary underline underline-offset-2 hover:text-foreground">
            Open Capacity
          </Link>
        }
      >
        {home.capacityLoading || home.seriesLoading ? (
          <TableSkeleton rows={7} cols={5} />
        ) : home.capacityError ? (
          <ErrorDisplay error={home.capacityError} />
        ) : home.links.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            No link has carried traffic in this window. Links are configured in{" "}
            <Link to="/links" className="underline underline-offset-2">Network › Links</Link>.
          </p>
        ) : (
          <LinkTable rows={home.links} step={home.step} filterSearch={filterSearch} />
        )}
      </Section>

      <Section
        id="h-networks"
        title="Who it's with"
        note="Remote networks over the window. All bars share one scale."
        aside={
          <Link to={`/top/as${filterSearch}`} className="text-foreground-secondary underline underline-offset-2 hover:text-foreground">
            Open Top AS
          </Link>
        }
      >
        <TopNetworks ownIn={ownIn} ownOut={ownOut} />
      </Section>

      <Section id="h-further" title="Further analysis" note="Views that used to sit on this page.">
        <FurtherAnalysis filterSearch={filterSearch} />
      </Section>
    </div>
  )
}
