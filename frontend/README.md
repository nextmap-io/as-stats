# AS-Stats — Frontend

React + TypeScript + Vite single-page app for the AS-Stats platform. See the
[top-level README](../README.md) for the project as a whole.

## Stack

- **React 19** with hooks, no class components
- **TypeScript** strict mode (no `any`)
- **Vite** dev server with HMR, production builds via `tsc -b && vite build`
- **TanStack Query** for every data fetch (no raw `useEffect` for API calls)
- **React Router 6** with URL-synced filters
- **Recharts** for all charts (`AreaChart` with `stepAfter` interpolation)
- **Tailwind CSS** with a dark-first NOC-inspired theme
- **JetBrains Mono** as the primary font, with `tabular-nums` everywhere

## Layout

```
src/
├── App.tsx                    # router + providers
├── pages/                     # one file per route in App.tsx
│   ├── Dashboard.tsx
│   ├── TopAS.tsx / TopIP.tsx / TopPrefixes.tsx
│   ├── ASDetail.tsx / IPDetail.tsx / LinkDetail.tsx
│   ├── Links.tsx
│   ├── SearchPage.tsx
│   ├── FlowSearch.tsx         # gated by FEATURE_FLOW_SEARCH
│   ├── TopProtocols.tsx       # gated by FEATURE_PORT_STATS
│   ├── TopPorts.tsx           # gated by FEATURE_PORT_STATS
│   ├── Alerts.tsx             # gated by FEATURE_ALERTS
│   ├── LiveThreats.tsx        # gated by FEATURE_ALERTS
│   └── Admin.tsx              # tabs: Links / Rules / Webhooks / Audit
├── components/
│   ├── ui/                    # Card, Skeleton, ErrorDisplay primitives
│   ├── charts/                # TrafficChart, LinkTrafficChart, ASTrafficChart
│   ├── layout/                # AppLayout + Header
│   ├── ExpandableChart.tsx    # click-to-fullscreen wrapper with period selector
│   ├── ChartModal.tsx
│   ├── PTR.tsx                # IPWithPTR — IP + reverse DNS rendering
│   └── ErrorBoundary.tsx
├── hooks/
│   ├── useApi.ts              # TanStack Query hooks for every endpoint
│   ├── useFilters.ts          # URL-synced period / link / direction filters
│   ├── useUnit.ts             # bps / pps / Bps cycle
│   ├── useChartColors.ts      # theme-aware chart palette
│   ├── useDns.ts              # cached PTR lookups
│   ├── useFeatures.ts         # /features discovery (forever-cached)
│   └── useTheme.ts            # light / dark / system
├── lib/
│   ├── api.ts                 # typed fetch wrapper with CSRF injection
│   ├── types.ts               # TypeScript mirror of internal/model
│   └── utils.ts
└── providers/
    └── ThemeProvider.tsx
```

## Conventions

- **Filters live in URL search params**, not in component state. The
  `useFilters` hook is the single source of truth for `period`, `link`,
  `direction`, etc. Bookmarking and sharing a deep link works because of
  this.
- **Every API call** goes through `lib/api.ts`. The `api` object exposes one
  typed method per endpoint, all with explicit return types from
  `lib/types.ts`. CSRF tokens are injected automatically on writes.
- **Feature gates**: pages and nav entries that depend on a backend feature
  flag wrap themselves in `useFeatureFlags()`. The hook returns safe
  defaults (`false`) while the `/features` request is in flight, so the UI
  never flashes on then off.
- **Cards**: always use `<CardHeader> + <CardTitle> + <CardContent>` from
  `components/ui/card.tsx`. Packing the title into a bare `CardContent`
  produces inconsistent vertical baselines vs. the rest of the app.
- **Numeric columns**: `font-mono tabular-nums` for digit alignment, plus
  the `useUnit` hook for the user's chosen formatter.
- **Recharts**: `AreaChart` with `type="stepAfter"` everywhere — flow data
  is bucketed counters, not continuous.

## Running

```bash
npm ci
npm run dev          # Vite dev server on http://localhost:5173
```

The dev server proxies `/api/v1/*` to `http://localhost:8080` so the
collector + API server have to be running separately:

```bash
# from the repo root
make docker-up && make run-collector & make run-api
```

## Building

```bash
npm run build        # tsc -b && vite build, output in dist/
npm run preview      # serve the built bundle locally
```

The production Docker image (`as-stats-frontend`) wraps `dist/` in nginx.

## Linting

```bash
npm run lint         # ESLint with the React + TypeScript rules in eslint.config.js
```

CI runs `npm run build` (which includes `tsc -b`) and `npm run lint` on
every PR.
