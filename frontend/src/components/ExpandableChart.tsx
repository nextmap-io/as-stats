import { useState, cloneElement, isValidElement, useCallback } from "react"
import { useQuery } from "@tanstack/react-query"
import { ChartModal } from "./ChartModal"
import { useUnit } from "@/hooks/useUnit"
import { api } from "@/lib/api"
import { LinkTrafficChart } from "./charts/LinkTrafficChart"
import { TrafficChart } from "./charts/TrafficChart"
import type { LinkTimeSeries, TrafficPoint } from "@/lib/types"

interface ExpandableChartProps {
  title?: string
  children: React.ReactElement<{ height?: number }>
  expandedHeight?: number
  // For dynamic refetch in modal
  fetchType?: "link-traffic" | "link-detail" | "as-detail-v4" | "as-detail-v6"
  fetchParams?: Record<string, unknown>
  linkColors?: Record<string, string>
  currentPeriod?: string
}

// Map period to bucket seconds for p95 display
function periodToBucket(period: string): number {
  if (period === "1h" || period === "3h") return 60
  if (period === "6h" || period === "24h") return 300
  if (period === "7d") return 3600
  return 86400
}

export function ExpandableChart({
  title, children, expandedHeight = 500,
  fetchType, fetchParams, linkColors, currentPeriod = "24h",
}: ExpandableChartProps) {
  const [open, setOpen] = useState(false)
  const [modalPeriod, setModalPeriod] = useState(currentPeriod)
  const { formatTraffic } = useUnit()

  // Fetch data for the modal period (only when modal is open and period differs)
  const modalFilters = { period: modalPeriod }
  const needsFetch = open && fetchType

  const { data: modalData } = useQuery({
    queryKey: ["modal", fetchType, fetchParams, modalPeriod],
    queryFn: async () => {
      if (!fetchType) return null
      switch (fetchType) {
        case "link-traffic": {
          const ipv = fetchParams?.ip_version as number
          return api.linksTraffic({ ...modalFilters, ip_version: ipv })
        }
        case "link-detail": {
          const tag = fetchParams?.tag as string
          return api.linkDetail(tag, modalFilters)
        }
        case "as-detail-v4": {
          const asn = fetchParams?.asn as number
          return api.asDetail(asn, modalFilters)
        }
        case "as-detail-v6": {
          const asn = fetchParams?.asn as number
          return api.asDetail(asn, modalFilters)
        }
        default:
          return null
      }
    },
    enabled: !!needsFetch,
    staleTime: 30_000,
  })

  const handleOpen = useCallback(() => {
    setModalPeriod(currentPeriod)
    setOpen(true)
  }, [currentPeriod])

  // Build the modal chart content
  const bucket = periodToBucket(modalPeriod)
  let modalChart: React.ReactNode = null
  const stats: { label: string; value: string; color?: string }[] = []

  if (needsFetch && modalData?.data) {
    switch (fetchType) {
      case "link-traffic": {
        const series = modalData.data as LinkTimeSeries[]
        if (series.length > 0) {
          modalChart = <LinkTrafficChart series={series} height={expandedHeight} linkColors={linkColors} />
        }
        break
      }
      case "link-detail": {
        const d = modalData.data as { time_series: TrafficPoint[]; p95_in?: number; p95_out?: number }
        if (d.time_series?.length > 0) {
          modalChart = <TrafficChart data={d.time_series} height={expandedHeight} p95In={d.p95_in} p95Out={d.p95_out} />
          if (d.p95_in) stats.push({ label: "P95 in", value: formatTraffic(d.p95_in, bucket), color: "text-traffic-in" })
          if (d.p95_out) stats.push({ label: "P95 out", value: formatTraffic(d.p95_out, bucket), color: "text-traffic-out" })
        }
        break
      }
      case "as-detail-v4":
      case "as-detail-v6": {
        const d = modalData.data as {
          v4_series?: LinkTimeSeries[]; v6_series?: LinkTimeSeries[];
          p95_v4_in?: number; p95_v4_out?: number; p95_v6_in?: number; p95_v6_out?: number;
          v4_bytes_in?: number; v4_bytes_out?: number; v6_bytes_in?: number; v6_bytes_out?: number;
        }
        const isV4 = fetchType === "as-detail-v4"
        const series = isV4 ? d.v4_series : d.v6_series
        const p95In = isV4 ? d.p95_v4_in : d.p95_v6_in
        const p95Out = isV4 ? d.p95_v4_out : d.p95_v6_out
        const bytesIn = isV4 ? d.v4_bytes_in : d.v6_bytes_in
        const bytesOut = isV4 ? d.v4_bytes_out : d.v6_bytes_out
        if (series && series.length > 0) {
          modalChart = <LinkTrafficChart series={series} height={expandedHeight} linkColors={linkColors} p95In={p95In} p95Out={p95Out} />
          if (p95In) stats.push({ label: "P95 in", value: formatTraffic(p95In, bucket), color: "text-traffic-in" })
          if (p95Out) stats.push({ label: "P95 out", value: formatTraffic(p95Out, bucket), color: "text-traffic-out" })
        }
        // Volume totals
        if (bytesIn) stats.push({ label: "Volume in", value: formatBytes(bytesIn) })
        if (bytesOut) stats.push({ label: "Volume out", value: formatBytes(bytesOut) })
        break
      }
    }
  }

  // Fallback: clone the original chart with larger height
  if (!modalChart) {
    modalChart = isValidElement(children)
      ? cloneElement(children, { height: expandedHeight } as Record<string, unknown>)
      : children
  }

  return (
    <>
      <div onClick={handleOpen} className="cursor-zoom-in" title="Click to enlarge">
        {children}
      </div>
      <ChartModal
        open={open}
        onClose={() => setOpen(false)}
        title={title}
        activePeriod={modalPeriod}
        onPeriodChange={fetchType ? setModalPeriod : undefined}
        stats={stats.length > 0 ? stats : undefined}
      >
        {modalChart}
      </ChartModal>
    </>
  )
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B"
  const units = ["B", "KB", "MB", "GB", "TB", "PB"]
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1000)), units.length - 1)
  const val = bytes / Math.pow(1000, i)
  return `${val < 10 ? val.toFixed(1) : Math.round(val)} ${units[i]}`
}
