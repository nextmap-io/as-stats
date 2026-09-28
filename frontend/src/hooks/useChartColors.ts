import { useMemo } from "react"
import { useTheme } from "./useTheme"

/** Returns resolved chart colors that adapt to the current theme */
export function useChartColors() {
  const { theme } = useTheme()

  return useMemo(() => {
    const root = document.documentElement
    const get = (v: string) => getComputedStyle(root).getPropertyValue(v).trim()

    return {
      grid: get("--color-border") || "hsl(220 15% 16%)",
      text: get("--color-muted-foreground") || "hsl(215 12% 50%)",
      tooltipBg: get("--color-popover") || "hsl(220 18% 10%)",
      tooltipBorder: get("--color-border") || "hsl(220 15% 16%)",
      tooltipText: get("--color-popover-foreground") || "hsl(210 20% 88%)",
    }
    // `theme` is not read inside the memo, but it is the dependency that
    // matters: switching theme rewrites the CSS variables read above, and this
    // is the only signal to re-read them. Dropping it (as the lint rule
    // suggests) would freeze chart colours on the first theme.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [theme])
}
