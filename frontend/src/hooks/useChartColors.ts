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
  }, [theme])
}
