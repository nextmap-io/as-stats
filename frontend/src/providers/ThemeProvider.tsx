import { useCallback, useEffect, useState, type ReactNode } from "react"
import { ThemeCtx, type ResolvedTheme, type Theme } from "@/hooks/useTheme"
import { applyThemeClass, persistTheme, readStoredTheme, resolveTheme, systemPrefersDark } from "@/lib/theme"

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setThemeState] = useState<Theme>(readStoredTheme)
  const [systemDark, setSystemDark] = useState(systemPrefersDark)

  const setTheme = useCallback(
    (next: Theme) => {
      applyThemeClass(resolveTheme(next, systemDark))
      persistTheme(next)
      setThemeState(next)
    },
    [systemDark],
  )

  // Follow the OS setting live while the user has chosen "system".
  useEffect(() => {
    if (theme !== "system" || !window.matchMedia) return
    const mq = window.matchMedia("(prefers-color-scheme: dark)")
    const onChange = (e: MediaQueryListEvent) => {
      applyThemeClass(e.matches ? "dark" : "light")
      setSystemDark(e.matches)
    }
    mq.addEventListener("change", onChange)
    return () => mq.removeEventListener("change", onChange)
  }, [theme])

  const resolved: ResolvedTheme = resolveTheme(theme, systemDark)

  return <ThemeCtx.Provider value={{ theme, resolved, setTheme }}>{children}</ThemeCtx.Provider>
}
