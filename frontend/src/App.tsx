import { BrowserRouter, Routes, Route } from "react-router-dom"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { ThemeProvider } from "@/providers/ThemeProvider"
import { UnitProvider, useUnitState } from "@/hooks/useUnit"
import { AppLayout } from "@/components/layout/AppLayout"
import { Dashboard } from "@/pages/Dashboard"
import { TopAS } from "@/pages/TopAS"
import { TopIP } from "@/pages/TopIP"
import { TopPrefixes } from "@/pages/TopPrefixes"
import { ASDetail } from "@/pages/ASDetail"
import { IPDetail } from "@/pages/IPDetail"
import { Links } from "@/pages/Links"
import { LinkDetail } from "@/pages/LinkDetail"
import { SearchPage } from "@/pages/SearchPage"

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      retry: 1,
    },
  },
})

function AppWithProviders() {
  const unitState = useUnitState()

  return (
    <UnitProvider value={unitState}>
      <BrowserRouter>
        <Routes>
          <Route element={<AppLayout />}>
            <Route path="/" element={<Dashboard />} />
            <Route path="/top/as" element={<TopAS />} />
            <Route path="/top/ip" element={<TopIP />} />
            <Route path="/top/prefix" element={<TopPrefixes />} />
            <Route path="/as/:asn" element={<ASDetail />} />
            <Route path="/ip/:ip" element={<IPDetail />} />
            <Route path="/links" element={<Links />} />
            <Route path="/link/:tag" element={<LinkDetail />} />
            <Route path="/search" element={<SearchPage />} />
          </Route>
        </Routes>
      </BrowserRouter>
    </UnitProvider>
  )
}

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <ThemeProvider>
        <AppWithProviders />
      </ThemeProvider>
    </QueryClientProvider>
  )
}
