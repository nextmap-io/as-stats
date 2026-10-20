import { BrowserRouter, Routes, Route } from "react-router-dom"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { ThemeProvider } from "@/providers/ThemeProvider"
import { UnitProvider, useUnitState } from "@/hooks/useUnit"
import { ErrorBoundary } from "@/components/ErrorBoundary"
import { AppLayout } from "@/components/layout/AppLayout"
import { FeatureGate } from "@/components/FeatureGate"
import { Dashboard } from "@/pages/Dashboard"
import { TopAS } from "@/pages/TopAS"
import { TopIP } from "@/pages/TopIP"
import { TopPrefixes } from "@/pages/TopPrefixes"
import { Countries } from "@/pages/Countries"
import { ASDetail } from "@/pages/ASDetail"
import { IPDetail } from "@/pages/IPDetail"
import { Links } from "@/pages/Links"
import { LinkDetail } from "@/pages/LinkDetail"
import { Capacity } from "@/pages/Capacity"
import { Status } from "@/pages/Status"
import { SearchPage } from "@/pages/SearchPage"
import { FlowSearch } from "@/pages/FlowSearch"
import { Conversations } from "@/pages/Conversations"
import { Changes } from "@/pages/Changes"
import { TopProtocols } from "@/pages/TopProtocols"
import { TopPorts } from "@/pages/TopPorts"
import { Alerts } from "@/pages/Alerts"
import { LiveThreats } from "@/pages/LiveThreats"
import { Admin } from "@/pages/Admin"
import { BGP } from "@/pages/BGP"

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
            <Route path="/countries" element={<Countries />} />
            <Route path="/as/:asn" element={<ASDetail />} />
            <Route path="/ip/:ip" element={<IPDetail />} />
            <Route path="/links" element={<Links />} />
            <Route path="/link/:tag" element={<LinkDetail />} />
            <Route path="/capacity" element={<Capacity />} />
            <Route path="/status" element={<Status />} />
            <Route path="/search" element={<SearchPage />} />
            <Route path="/flows" element={<FeatureGate flag="flow_search" title="Flow search" setting="FEATURE_FLOW_SEARCH=true"><FlowSearch /></FeatureGate>} />
            <Route path="/conversations" element={<FeatureGate flag="flow_search" title="Conversations" setting="FEATURE_FLOW_SEARCH=true"><Conversations /></FeatureGate>} />
            <Route path="/changes" element={<Changes />} />
            <Route path="/top/protocol" element={<FeatureGate flag="port_stats" title="Top protocols" setting="FEATURE_PORT_STATS=true"><TopProtocols /></FeatureGate>} />
            <Route path="/top/port" element={<FeatureGate flag="port_stats" title="Top ports" setting="FEATURE_PORT_STATS=true"><TopPorts /></FeatureGate>} />
            <Route path="/alerts" element={<FeatureGate flag="alerts" title="Alerts" setting="FEATURE_ALERTS=true"><Alerts /></FeatureGate>} />
            <Route path="/live" element={<FeatureGate flag="alerts" title="Live threats" setting="FEATURE_ALERTS=true"><LiveThreats /></FeatureGate>} />
            <Route path="/bgp" element={<FeatureGate flag="bgp" title="BGP blocks" setting="BGP_ENABLED=true (plus the BGP peer settings)"><BGP /></FeatureGate>} />
            <Route path="/admin" element={<Admin />} />
          </Route>
        </Routes>
      </BrowserRouter>
    </UnitProvider>
  )
}

export default function App() {
  return (
    <ErrorBoundary>
      <QueryClientProvider client={queryClient}>
        <ThemeProvider>
          <AppWithProviders />
        </ThemeProvider>
      </QueryClientProvider>
    </ErrorBoundary>
  )
}
