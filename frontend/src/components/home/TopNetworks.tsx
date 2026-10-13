import { Link } from "react-router-dom"
import { useTopAS } from "@/hooks/useApi"
import { useFilters } from "@/hooks/useFilters"
import { useFeatureFlags } from "@/hooks/useFeatures"
import { useUnit } from "@/hooks/useUnit"
import { asPath, isPrivateASGroup, ourView } from "@/lib/asn"
import { formatRate } from "@/lib/rate"
import { ErrorDisplay } from "@/components/ui/error"
import { TableSkeleton } from "@/components/ui/skeleton"

const ROWS = 11

/**
 * TopNetworks: one ranked list of remote networks, every row on ONE scale so
 * lengths compare. Your own AS is on one side of nearly every flow, so it is
 * stated once as a reference line instead of being ranked (the API already
 * leaves it out). The private-ASN group is a labelled row with no detail page.
 * Fetches exactly the rows shown, without per-AS series.
 */
export function TopNetworks({ ownIn, ownOut }: { ownIn: number; ownOut: number }) {
  const { filters, filterSearch, periodSeconds } = useFilters()
  const features = useFeatureFlags()
  const { formatTraffic, unit } = useUnit()
  const query = useTopAS({ ...filters, limit: ROWS, offset: undefined })

  if (query.isLoading) return <TableSkeleton rows={8} cols={4} />
  if (query.error) return <ErrorDisplay error={query.error} onRetry={() => void query.refetch()} />

  const rows = (query.data?.data ?? []).map((as) => ({ ...as, ...ourView(as) }))
  const max = Math.max(1, ...rows.map((r) => Math.max(r.inBytes, r.outBytes)))
  const fmt = (bytes: number) => formatTraffic(bytes, periodSeconds)
  const rateMode = unit !== "bytes"

  return (
    <div>
      {features.local_as ? (
        <p className="mb-3 flex flex-wrap items-baseline gap-x-5 gap-y-1 text-sm text-foreground-secondary">
          <span>
            <span className="font-semibold text-foreground">AS{features.local_as}</span> is your network. It sits on one side of
            almost every flow, so it is not ranked.
          </span>
          <span>
            {formatRate(ownIn, 1, "bytes", "bytes")} in, {formatRate(ownOut, 1, "bytes", "bytes")} out over the window.
          </span>
        </p>
      ) : null}

      <div
        className="hidden grid-cols-[28px_minmax(0,1.5fr)_96px_96px_minmax(200px,2fr)] items-end gap-x-5 border-b border-foreground pb-2 text-[13px] text-muted-foreground md:grid"
        aria-hidden
      >
        <span>#</span>
        <span>Network</span>
        <span className="text-right">{rateMode ? "Average in" : "In"}</span>
        <span className="text-right">{rateMode ? "Average out" : "Out"}</span>
        <span>Inbound, outbound</span>
      </div>
      {rows.length === 0 ? (
        <p className="py-8 text-sm text-muted-foreground">No remote network exchanged traffic in this window.</p>
      ) : (
        <ol>
          {rows.map((r, i) => {
            const path = asPath(r.as_number, filterSearch)
            const grouped = isPrivateASGroup(r.as_number)
            return (
              <li
                key={r.as_number}
                className="grid grid-cols-[22px_minmax(0,1fr)_76px_76px] items-center gap-x-3 gap-y-1.5 border-b border-border-soft py-2 md:grid-cols-[28px_minmax(0,1.5fr)_96px_96px_minmax(200px,2fr)] md:gap-x-5"
              >
                <span className="text-[13px] text-muted-foreground">{i + 1}</span>
                <span className="min-w-0 truncate">
                  {grouped ? (
                    <>
                      <span className="font-semibold">Private / internal</span>{" "}
                      <span className="text-[13px] text-muted-foreground">private ASNs, grouped</span>
                    </>
                  ) : (
                    <>
                      <Link to={path ?? "#"} className="font-semibold hover:underline">
                        AS{r.as_number}
                      </Link>{" "}
                      <span className="text-foreground-secondary">{r.as_name}</span>
                    </>
                  )}
                </span>
                <span className="text-right text-sm">{fmt(r.inBytes)}</span>
                <span className="text-right text-sm text-traffic-out">{fmt(r.outBytes)}</span>
                <div
                  className="col-span-3 col-start-2 grid h-3 grid-cols-2 md:col-span-1 md:col-start-auto"
                  role="img"
                  aria-label={`In ${fmt(r.inBytes)}, out ${fmt(r.outBytes)}`}
                >
                  <div className="flex justify-end border-r border-foreground">
                    <span className="block h-full bg-traffic-in opacity-90" style={{ width: `${(r.inBytes / max) * 100}%` }} />
                  </div>
                  <div>
                    <span className="block h-full bg-traffic-out" style={{ width: `${(r.outBytes / max) * 100}%` }} />
                  </div>
                </div>
              </li>
            )
          })}
        </ol>
      )}
      <p className="mt-3 text-sm">
        Showing the top {rows.length} networks.{" "}
        <Link to={`/top/as${filterSearch}`} className="font-semibold underline underline-offset-2">
          See all on Top AS
        </Link>
      </p>
    </div>
  )
}
