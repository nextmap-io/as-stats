import { lazy } from "react"
import type { ComponentType } from "react"
import { BrowserRouter, Routes, Route } from "react-router-dom"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { ThemeProvider } from "@/providers/ThemeProvider"
import { UnitProvider, useUnitState } from "@/hooks/useUnit"
import { ErrorBoundary } from "@/components/ErrorBoundary"
import { AppLayout } from "@/components/layout/AppLayout"
import { FeatureGate } from "@/components/FeatureGate"

// One chunk per page: the shell (layout, header, filters, command palette)
// loads first, each page (and recharts, shared by the chart pages) on first
// visit. AppLayout holds the <Suspense> around the <Outlet>, so the header
// stays up while a page chunk loads. Pages use named exports, hence the
// re-wrap into the default export React.lazy expects.
function page<K extends string>(load: () => Promise<Record<K, ComponentType>>, name: K) {
  return lazy(() => load().then((m) => ({ default: m[name] })))
}

const Dashboard = page(() => import("@/pages/Dashboard"), "Dashboard")
const TopAS = page(() => import("@/pages/TopAS"), "TopAS")
const TopIP = page(() => import("@/pages/TopIP"), "TopIP")
const TopPrefixes = page(() => import("@/pages/TopPrefixes"), "TopPrefixes")
const Countries = page(() => import("@/pages/Countries"), "Countries")
const ASDetail = page(() => import("@/pages/ASDetail"), "ASDetail")
const IPDetail = page(() => import("@/pages/IPDetail"), "IPDetail")
const Links = page(() => import("@/pages/Links"), "Links")
const LinkDetail = page(() => import("@/pages/LinkDetail"), "LinkDetail")
const Capacity = page(() => import("@/pages/Capacity"), "Capacity")
const Status = page(() => import("@/pages/Status"), "Status")
const SearchPage = page(() => import("@/pages/SearchPage"), "SearchPage")
const FlowSearch = page(() => import("@/pages/FlowSearch"), "FlowSearch")
const Conversations = page(() => import("@/pages/Conversations"), "Conversations")
const Changes = page(() => import("@/pages/Changes"), "Changes")
const TopProtocols = page(() => import("@/pages/TopProtocols"), "TopProtocols")
const TopPorts = page(() => import("@/pages/TopPorts"), "TopPorts")
const Alerts = page(() => import("@/pages/Alerts"), "Alerts")
const LiveThreats = page(() => import("@/pages/LiveThreats"), "LiveThreats")
const Admin = page(() => import("@/pages/Admin"), "Admin")
const BGP = page(() => import("@/pages/BGP"), "BGP")

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
