import { useMemo } from "react"
import { useTheme } from "./useTheme"

const SERIES_COUNT = 8

export interface ChartColors {
  /** Gridlines. */
  grid: string
  /** Axis labels, secondary chart text. */
  text: string
  /** Ink: zero line, annotations. */
  ink: string
  inbound: string
  outbound: string
  /** Text tone for "watch" annotations; `warningMark` fills bars and ticks. */
  warning: string
  warningMark: string
  destructive: string
  tooltipBg: string
  tooltipBorder: string
  tooltipText: string
  /** Ordinal palette for per-link / per-AS series, in order. */
  series: string[]
}

/** Returns resolved chart colours that adapt to the current theme. */
export function useChartColors(): ChartColors {
  const { resolved } = useTheme()

  return useMemo(() => {
    const root = document.documentElement
    const style = getComputedStyle(root)
    const get = (v: string, fallback: string) => style.getPropertyValue(v).trim() || fallback

    return {
      grid: get("--color-border-soft", "#2a2e35"),
      text: get("--color-muted-foreground", "#8c949e"),
      ink: get("--color-foreground", "#eceef1"),
      inbound: get("--color-traffic-in", "#d3d8de"),
      outbound: get("--color-traffic-out", "#7d9bff"),
      warning: get("--color-warning", "#f2b852"),
      warningMark: get("--color-warning-mark", "#f2b852"),
      destructive: get("--color-destructive", "#ff6f64"),
      tooltipBg: get("--color-popover", "#20242a"),
      tooltipBorder: get("--color-border", "#333840"),
      tooltipText: get("--color-popover-foreground", "#eceef1"),
      series: Array.from({ length: SERIES_COUNT }, (_, i) => get(`--color-series-${i + 1}`, "#7d9bff")),
    }
    // `resolved` is not read inside the memo, but it is the dependency that
    // matters: switching theme rewrites the CSS variables read above, and this
    // is the only signal to re-read them. Dropping it (as the lint rule
    // suggests) would freeze chart colours on the first theme.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [resolved])
}

/** Colour for the i-th series, honouring an operator-configured override. */
export function seriesColor(colors: ChartColors, i: number, override?: string): string {
  return override || colors.series[i % colors.series.length]
}

/** Shared Recharts tooltip container style. */
export function tooltipBoxStyle(colors: ChartColors): React.CSSProperties {
  return {
    backgroundColor: colors.tooltipBg,
    border: `1px solid ${colors.tooltipBorder}`,
    borderRadius: 4,
    fontSize: 13,
    boxShadow: "0 2px 8px rgb(0 0 0 / 0.12)",
    padding: "6px 10px",
    color: colors.tooltipText,
  }
}

/** Axis tick style: readable size, quiet colour, tabular figures. */
export function axisTick(colors: ChartColors) {
  return { fontSize: 12, fill: colors.text }
}
