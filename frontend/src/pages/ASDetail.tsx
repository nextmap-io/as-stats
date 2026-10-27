import { useMemo } from "react"
import { Link, useParams, useSearchParams } from "react-router-dom"
import { useASDetail, useASTopIPs, useASRemoteIPs, useLinkColors } from "@/hooks/useApi"
import { useFilters } from "@/hooks/useFilters"
import { useFeatureFlags } from "@/hooks/useFeatures"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { LinkTrafficChart } from "@/components/charts/LinkTrafficChart"
import { TrafficChart } from "@/components/charts/TrafficChart"
import { ExpandableChart } from "@/components/ExpandableChart"
import { ComparisonToggle } from "@/components/ComparisonToggle"
import { previousWindow, shiftSeries, useCompareEnabled } from "@/lib/comparison"
import { formatNumber, formatBytes } from "@/lib/utils"
import { useUnit } from "@/hooks/useUnit"
import { ExternalLink, Search } from "lucide-react"
import { IPWithPTR } from "@/components/PTR"
import { RatioBar, AsymmetryBadge } from "@/components/Asymmetry"
import { countryFlag, countryName, hasCountry } from "@/lib/countries"
import { ourView } from "@/lib/asn"
import { stepSecondsOf } from "@/lib/rate"
import { Segmented } from "@/components/ui/segmented"
import { IP_FAMILY_OPTIONS, mergeLinkSeries, parseIPFamily, type IPFamily } from "@/lib/series"
import { ErrorDisplay } from "@/components/ui/error"
import { PageSkeleton } from "@/components/ui/skeleton"

export function ASDetail() {
  const { asn } = useParams<{ asn: string }>()
  const asnNum = Number(asn) || 0
  const { filters, filterSearch, periodSeconds, timeBounds, bounds } = useFilters()
  const { formatTraffic } = useUnit()
  const features = useFeatureFlags()
  const linkColors = useLinkColors()

  const { data, isLoading, error, refetch } = useASDetail(asnNum, filters)
  const { data: topIPsData } = useASTopIPs(asnNum, { ...filters, limit: 20 })
  const { data: remoteIPsData } = useASRemoteIPs(asnNum, { ...filters, limit: 20 })

  // Comparison overlay (Module D). When off, the prev query reuses the active
  // filters so it dedupes with the main query — no extra request.
  const compare = useCompareEnabled()
  // Memoized on the minute-snapped bounds: prevFilters feeds a query key.
  const { prevFilters, windowMs } = useMemo(
    () => previousWindow(filters, bounds),
    [filters, bounds],
  )
  const prevDetail = useASDetail(asnNum, compare ? prevFilters : filters)
  const prevSeries =
    compare && prevDetail.data?.data?.time_series
      ? shiftSeries(prevDetail.data.data.time_series, windowMs)
      : undefined

  // Address family of the by-link chart, in the URL like the dashboard's.
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

  // Same loading / error vocabulary as every other page: a flat skeleton, and
  // an error that names what failed and offers a retry.
  if (isLoading) return <PageSkeleton />
  if (error) return <ErrorDisplay error={error} title={`Could not load AS${asnNum}`} onRetry={() => void refetch()} />

  const detail = data?.data
  if (!detail) return null

  // Bucket width the server used for the series and the p95 values.
  const step = stepSecondsOf(data?.meta, periodSeconds)
  // One direction convention on this page: your side. The IPv4/IPv6 totals and
  // the charts already count that way; the asymmetry fields do not.
  const { inBytes, outBytes } = ourView(detail)

  return (
    <div className="space-y-5">
      {/* Header */}
      <div className="flex items-baseline justify-between flex-wrap gap-2">
        <h1 className="text-2xl font-semibold">
          AS{detail.as_number}
          {detail.as_name && (
            <span className="ml-3 text-lg font-normal text-foreground-secondary">{detail.as_name}</span>
          )}
          {hasCountry(detail.country) && (
            <span
              className="ml-2 text-sm font-normal text-muted-foreground"
              title={countryName(detail.country)}
            >
              <span aria-hidden>{countryFlag(detail.country)}</span>{" "}
              <span className="text-xs">{detail.country}</span>
            </span>
          )}
        </h1>
        <div className="flex flex-wrap items-center gap-3 text-[13px]">
          <a href={`https://bgp.he.net/AS${detail.as_number}`} target="_blank" rel="noopener noreferrer"
            className="text-muted-foreground hover:text-primary transition-colors inline-flex items-center gap-1">
            HE.net <ExternalLink className="size-3" aria-hidden />
          </a>
          <a href={`https://www.peeringdb.com/asn/${detail.as_number}`} target="_blank" rel="noopener noreferrer"
            className="text-muted-foreground hover:text-primary transition-colors inline-flex items-center gap-1">
            PeeringDB <ExternalLink className="size-3" aria-hidden />
          </a>
          <a href={`https://bgp.tools/as/${detail.as_number}`} target="_blank" rel="noopener noreferrer"
            className="text-muted-foreground hover:text-primary transition-colors inline-flex items-center gap-1">
            bgp.tools <ExternalLink className="size-3" aria-hidden />
          </a>
          {features.flow_search && (
            <Link
              to={`/flows?period=${filters.period || "1h"}&dst_as=${detail.as_number}`}
              className="text-primary hover:text-primary/80 transition-colors inline-flex items-center gap-1"
              title="Search flows involving this AS"
            >
              <Search className="size-3" aria-hidden />
              Flows
            </Link>
          )}
          <ComparisonToggle />
        </div>
      </div>

      {/* Volume + P95 summary */}
      <div className="flex flex-wrap gap-x-8 gap-y-2 text-sm">
        <div>
          <span className="text-muted-foreground">IPv4:</span>{" "}
          <span className="text-traffic-in">{formatBytes(detail.v4_bytes_in || 0)} in</span>
          {" / "}
          <span className="text-traffic-out">{formatBytes(detail.v4_bytes_out || 0)} out</span>
          {(detail.p95_v4_in || detail.p95_v4_out) ? (
            <span className="ml-2 text-muted-foreground">
              p95: <span className="text-traffic-in">{formatTraffic(detail.p95_v4_in || 0, step)}</span>
              {" / "}<span className="text-traffic-out">{formatTraffic(detail.p95_v4_out || 0, step)}</span>
            </span>
          ) : null}
        </div>
        <div>
          <span className="text-muted-foreground">IPv6:</span>{" "}
          <span className="text-traffic-in">{formatBytes(detail.v6_bytes_in || 0)} in</span>
          {" / "}
          <span className="text-traffic-out">{formatBytes(detail.v6_bytes_out || 0)} out</span>
          {(detail.p95_v6_in || detail.p95_v6_out) ? (
            <span className="ml-2 text-muted-foreground">
              p95: <span className="text-traffic-in">{formatTraffic(detail.p95_v6_in || 0, step)}</span>
              {" / "}<span className="text-traffic-out">{formatTraffic(detail.p95_v6_out || 0, step)}</span>
            </span>
          ) : null}
        </div>
        {inBytes || outBytes ? (
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-muted-foreground">In / out share:</span>
            <RatioBar bytesIn={inBytes} bytesOut={outBytes} />
            <AsymmetryBadge cls={detail.class} />
          </div>
        ) : null}
      </div>

      {/* Total traffic vs previous period (comparison overlay, opt-in) */}
      {compare && detail.time_series?.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>Total traffic against the previous period</CardTitle>
          </CardHeader>
          <CardContent>
            <TrafficChart data={detail.time_series} previous={prevSeries} height={300} timeBounds={timeBounds} stepSeconds={step} />
          </CardContent>
        </Card>
      )}

      {/* By link: one chart on one scale, the address family a switch (Ledger:
          filter, don't double the chart). p95 lines are drawn per family only —
          the p95 of the sum is not the sum of the p95s. */}
      <Card className="overflow-visible">
        <CardHeader>
          <div className="flex flex-wrap items-center justify-between gap-2">
            <CardTitle>By link</CardTitle>
            <Segmented label="IP version" options={IP_FAMILY_OPTIONS} value={family} onChange={setFamily} />
          </div>
        </CardHeader>
        <CardContent>
          {(() => {
            const series =
              family === "4" ? (detail.v4_series ?? []) : family === "6" ? (detail.v6_series ?? []) : mergeLinkSeries(detail.v4_series, detail.v6_series)
            if (!series.length) {
              return <p className="py-8 text-center text-[13px] text-muted-foreground">No {family === "both" ? "" : `IPv${family} `}traffic with this network in the window.</p>
            }
            const chart = (
              <LinkTrafficChart
                series={series}
                height={280}
                linkColors={linkColors}
                p95In={family === "4" ? detail.p95_v4_in : family === "6" ? detail.p95_v6_in : undefined}
                p95Out={family === "4" ? detail.p95_v4_out : family === "6" ? detail.p95_v6_out : undefined}
                timeBounds={timeBounds}
                stepSeconds={step}
              />
            )
            return family === "both" ? (
              <ExpandableChart title={`AS${detail.as_number} by link`}>{chart}</ExpandableChart>
            ) : (
              <ExpandableChart
                title={`AS${detail.as_number} by link, IPv${family}`}
                fetchType={family === "4" ? "as-detail-v4" : "as-detail-v6"}
                fetchParams={{ asn: asnNum }}
                linkColors={linkColors}
                currentPeriod={filters.period}
              >
                {chart}
              </ExpandableChart>
            )
          })()}
        </CardContent>
      </Card>

      {/* Tables: Top IPs + Peers */}
      <div className="grid gap-5 lg:grid-cols-2">
        {topIPsData?.data && topIPsData.data.length > 0 && (
          <Card>
            <CardHeader>
              <CardTitle>Your top IPs with this network</CardTitle>
            </CardHeader>
            <CardContent>
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-foreground">
                    <th className="pb-1.5 text-left text-[13px] font-normal text-muted-foreground">IP</th>
                    <th className="pb-1.5 text-right text-[13px] font-normal text-muted-foreground">Traffic</th>
                    <th className="pb-1.5 text-right text-[13px] font-normal text-muted-foreground">Flows</th>
                  </tr>
                </thead>
                <tbody>
                  {topIPsData.data.map(ip => (
                    <tr key={ip.ip} className="border-b border-border-soft last:border-0 hover:bg-accent/60">
                      <td className="py-1 max-w-[280px]">
                        <Link to={`/ip/${ip.ip}${filterSearch}`} className="text-primary hover:underline font-ip text-[13px] block truncate">
                          <IPWithPTR ip={ip.ip} />
                        </Link>
                      </td>
                      <td className="py-1 text-right">{formatTraffic(ip.bytes, periodSeconds, ip.packets)}</td>
                      <td className="py-1 text-right text-muted-foreground">{formatNumber(ip.flows)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </CardContent>
          </Card>
        )}

        {remoteIPsData?.data && remoteIPsData.data.length > 0 && (
          <Card>
            <CardHeader>
              <CardTitle>Their top IPs</CardTitle>
            </CardHeader>
            <CardContent>
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-foreground">
                    <th className="pb-1.5 text-left text-[13px] font-normal text-muted-foreground">IP</th>
                    <th className="pb-1.5 text-right text-[13px] font-normal text-muted-foreground">Traffic</th>
                    <th className="pb-1.5 text-right text-[13px] font-normal text-muted-foreground">Flows</th>
                  </tr>
                </thead>
                <tbody>
                  {remoteIPsData.data.map(ip => (
                    <tr key={ip.ip} className="border-b border-border-soft last:border-0 hover:bg-accent/60">
                      <td className="py-1 max-w-[280px]">
                        <Link to={`/ip/${ip.ip}${filterSearch}`} className="text-primary hover:underline font-ip text-[13px] block truncate">
                          <IPWithPTR ip={ip.ip} />
                        </Link>
                      </td>
                      <td className="py-1 text-right">{formatTraffic(ip.bytes, periodSeconds, ip.packets)}</td>
                      <td className="py-1 text-right text-muted-foreground">{formatNumber(ip.flows)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </CardContent>
          </Card>
        )}
      </div>
    </div>
  )
}
