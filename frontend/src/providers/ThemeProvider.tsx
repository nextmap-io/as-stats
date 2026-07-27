import { useEffect, useState, type ReactNode } from "react"
import { ThemeCtx } from "@/hooks/useTheme"

type Theme = "light" | "dark" | "system"

const STORAGE_KEY = "theme"

// localStorage access throws (not returns null) when storage is blocked —
// Safari private browsing, strict third-party-cookie policies, some enterprise
// profiles. ThemeProvider wraps the whole router, so an uncaught throw here
// takes the entire SPA down to the ErrorBoundary fallback. Degrade to the
// in-memory default instead: the app still works, the choice just isn't
// remembered across reloads.
function readStoredTheme(): Theme {
  try {
    const stored = localStorage.getItem(STORAGE_KEY)
    if (stored === "light" || stored === "dark" || stored === "system") return stored
  } catch {
    // storage unavailable — fall through to the default
  }
  return "system"
}

function persistTheme(theme: Theme) {
  try {
    localStorage.setItem(STORAGE_KEY, theme)
  } catch {
    // storage unavailable — the theme stays session-local
  }
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setTheme] = useState<Theme>(readStoredTheme)

  useEffect(() => {
    const root = document.documentElement
    root.classList.remove("light", "dark")

    if (theme === "system") {
      const isDark = window.matchMedia("(prefers-color-scheme: dark)").matches
      root.classList.add(isDark ? "dark" : "light")
    } else {
      root.classList.add(theme)
    }

    persistTheme(theme)
  }, [theme])

  return (
    <ThemeCtx.Provider value={{ theme, setTheme }}>
      {children}
    </ThemeCtx.Provider>
  )
}
