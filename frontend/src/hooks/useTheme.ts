import { createContext, useContext } from "react"

export type Theme = "light" | "dark" | "system"
export type ResolvedTheme = "light" | "dark"

export interface ThemeContextType {
  theme: Theme
  /** The theme actually applied to <html> ("system" resolved). */
  resolved: ResolvedTheme
  setTheme: (t: Theme) => void
}

export const ThemeCtx = createContext<ThemeContextType>({ theme: "system", resolved: "dark", setTheme: () => {} })

export const useTheme = () => useContext(ThemeCtx)
