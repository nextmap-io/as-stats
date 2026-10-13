import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App.tsx'
import { applyThemeClass, readStoredTheme, resolveTheme } from './lib/theme'

// Set the theme class before the first render so nothing reads the wrong
// palette (index.html ships class="dark" as the no-JS default).
applyThemeClass(resolveTheme(readStoredTheme()))

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
