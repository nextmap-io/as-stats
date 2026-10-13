import type { ResolvedTheme, Theme } from "@/hooks/useTheme"

const STORAGE_KEY = "theme"

// localStorage access throws (not returns null) when storage is blocked —
// Safari private browsing, strict third-party-cookie policies, some enterprise
// profiles. ThemeProvider wraps the whole router, so an uncaught throw here
// takes the entire SPA down to the ErrorBoundary fallback. Degrade to the
// in-memory default instead: the app still works, the choice just isn't
// remembered across reloads.
export function readStoredTheme(): Theme {
  try {
    const stored = localStorage.getItem(STORAGE_KEY)
    if (stored === "light" || stored === "dark" || stored === "system") return stored
  } catch {
    // storage unavailable — fall through to the default
  }
  return "system"
}

export function persistTheme(theme: Theme) {
  try {
    localStorage.setItem(STORAGE_KEY, theme)
  } catch {
    // storage unavailable — the theme stays session-local
  }
}

export function systemPrefersDark(): boolean {
  return typeof window !== "undefined" && !!window.matchMedia?.("(prefers-color-scheme: dark)").matches
}

export function resolveTheme(theme: Theme, systemDark = systemPrefersDark()): ResolvedTheme {
  if (theme === "system") return systemDark ? "dark" : "light"
  return theme
}

/**
 * applyThemeClass sets the light/dark class on <html>. It runs before React
 * re-renders (at boot in main.tsx, and inside the event handlers that change
 * the theme), so components that read CSS variables while rendering — chart
 * colours — never see the previous theme's values.
 */
export function applyThemeClass(resolved: ResolvedTheme) {
  const root = document.documentElement
  root.classList.remove("light", "dark")
  root.classList.add(resolved)
}
