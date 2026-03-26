import { createContext, useContext } from "react"

type Theme = "light" | "dark" | "system"

export interface ThemeContextType {
  theme: Theme
  setTheme: (t: Theme) => void
}

export const ThemeCtx = createContext<ThemeContextType>({ theme: "system", setTheme: () => {} })

export const useTheme = () => useContext(ThemeCtx)
