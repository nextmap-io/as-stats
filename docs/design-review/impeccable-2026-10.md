# Design review: impeccable 4.7.0, October 2026

A quality pass on the web UI with the [impeccable](https://github.com/pbakaus/impeccable)
toolkit (v4.7.0). We followed its `audit`, `critique` and `polish` procedures over
the whole product, plus `harden` and `adapt` where the findings called for them.
The baseline is release 3.4.0.

The "Ledger" direction was the brief (Archivo, ink on grey, one cobalt accent,
rules instead of boxes, colour only for abnormal states). Impeccable was used to
check quality, accessibility and consistency within that direction, not to
change the aesthetic. Section 6 lists where impeccable's taste and Ledger
disagree and what we did.

## 1. Method

- **Data.** The demo stack in `scripts/demo/` ran with tag `3.4.0`, using its
  invented data (AS64500, documentation prefixes). For the "after" state, the
  branch's frontend was built in `node:22-alpine` and served by `nginx:1.27-alpine`
  with `frontend/nginx.conf`, against the same demo API.
- **Pages.** All 21 routes were captured as full-page screenshots in light and
  dark, at 1440 px and 390 px (84 captures before, 84 after). A small DOM audit
  ran on each capture: text under 12 px, unnamed controls, targets under 24 px,
  heading jumps, horizontal overflow, console errors.
- **Detector.** `npx impeccable@4.7.0 detect --json` ran in
  `zenika/alpine-chrome:with-puppeteer` with `DO_NOT_TRACK=1` and
  `IMPECCABLE_NO_TELEMETRY=1`. Chromium was wrapped to add `--no-sandbox`, which
  the container needs.
  - Light passes used URL scans with `--viewport 1440x1000` and `390x844`.
  - Dark passes opened each page in a Chromium we control, with the theme set
    before first render, then attached the detector with `--cdp`. The detector
    itself has no colour-scheme option.
  - A static scan of `frontend/src` had already returned nothing; the useful
    rules only run on rendered pages.
- **What was not run.** `impeccable install`, the skill launcher (`context`,
  `critique-storage`) and the live overlay were not run. Nothing under
  `.impeccable/` was written.
- **Critique isolation.** Assessment A (design review) ran in an isolated
  sub-agent that never saw detector output. Assessment B (detector and browser
  evidence) ran in the parent context, not in a second sub-agent, so the run is
  partly degraded by impeccable's own rules. The critique's closing questions
  were skipped: this was an autonomous run with the full scope delegated.

## 2. Detector results

### Counts

| Pass | Before (3.4.0) | After |
|---|---:|---:|
| Light, 1440 px | 24 | 0 |
| Light, 390 px | 29 | 0 |
| Dark, 1440 px | 20 | 0 |
| Dark, 390 px | 25 | 0 |
| **Total** | **98** | **0** |

### By rule

| Rule | Severity | Pages (before) | Count before | After | What we did |
|---|---|---|---:|---:|---|
| `skipped-heading` | warning | 19 of 21: every page but Home and Live threats (h1 then h3) | 76 (19 × 4 passes) | 0 | Fixed. `CardTitle` renders an `h2` (`as="h3"` when nested). The stacked-chart titles on Links, AS detail and Link detail became `h2`. |
| `low-contrast` | warning | Changes: the "(+12 %)" note, `opacity-70`, 3.5:1 | 2 (light passes) | 0 | Fixed: tertiary ink, no opacity. |
| `low-contrast` | advisory | AS detail, Link detail ("Compare previous"); Flow search (Period buttons): tertiary ink on `bg-muted/50`, 4.4:1 | 6 (light passes) | 0 | Fixed. The shared segmented control and the toggle now use secondary ink. |
| `line-length` | warning | Live threats intro, about 103 characters per line | 2 (1440 passes) | 0 | Fixed: capped at 62ch. |
| `straggler` | advisory | 390 px: Home, Capacity, Link detail captions; Live threats (3) | 12 (390 passes) | 0 | Fixed globally: `p { text-wrap: pretty }`, headings `balance`. |

## 3. Audit (technical) and critique (design)

### Audit health score (impeccable `audit`)

| # | Dimension | Before | After | Key finding |
|---|---|---:|---:|---|
| 1 | Accessibility | 2 | 3 | Sort headers not keyboard-reachable; `role="menu"` without arrow keys; h1→h3 everywhere; icon-only "×" Resolve; 3.5:1 text |
| 2 | Performance | 3 | 3 | One 500 kB+ JS chunk (Vite warning); otherwise lean, no animation cost |
| 3 | Theming | 3 | 4 | Tokens used throughout; a few opacity-dimmed texts and tinted-fill controls fell below AA |
| 4 | Responsive | 2 | 3 | Alerts actions clipped at 390 px; active time range scrolled out of view; 18 px page overflow on Top protocols; values wrapped away from units |
| 5 | Implementation integrity | 3 | 3 | Nine hand-rolled segmented controls in three styles; feature pages 404 when the flag is off |
| | **Total** | **13/20** (acceptable) | **16/20** (good) | |

### Critique (impeccable `critique`, Assessment A)

**Specificity.** Assessment A called the design specific to this product and
not generic. Home ("Right now", the mirrored one-scale chart, link rows with a
peak tick and a 70 % line, "AS64500 is your network… so it is not ranked") and
Live threats are product-specific. The secondary pages drift toward a generic
analytics kit: Treemap and Donut, emoji flags, boxed chips, rainbow stacks.

**Nielsen heuristics.** Assessment A scored 25/36 with #9 left n/a, because no
error state was captured. The detector-side evidence did capture error states:
BGP's three raw "Not Found" panels, and bare red text with no retry on the
detail pages. On that evidence #9 scores 2, which makes the total 27/40,
"acceptable".

| # | Heuristic | Score | Key issue |
|---|---|---:|---|
| 1 | Visibility of system status | 3 | Active time range hidden at 390 px |
| 2 | Match with the real world | 3 | Mixed units: "10000 Mbps", "10,000 Mbps", "10 Gb/s" |
| 3 | User control and freedom | 3 | Unlabeled one-click Resolve "×" |
| 4 | Consistency and standards | 2 | Three segmented-control styles; Title Case vs sentence case; icons in only three h1s; duplicate period pickers |
| 5 | Error prevention | 2 | Icon-only destructive buttons. Link delete does ask for confirmation. |
| 6 | Recognition rather than recall | 3 | No incident context on IP detail; ⌘K palette not advertised |
| 7 | Flexibility and efficiency | 3 | No bulk ack/resolve |
| 8 | Aesthetic and minimalist design | 3 | Home is exemplary; secondary pages are noisier |
| 9 | Error recovery | 2 | Raw "Not Found" on BGP; bare error text on detail pages |
| 10 | Help and documentation | 3 | Good inline captions; no glossary for "Class" |

**Cognitive load.** 4 of 8 checks failed:

- Minimal choices: the time bar has 11 options, Admin has 8 tabs, Top AS shows 3 control groups.
- One thing at a time: two or three time selectors on detail pages.
- Grouping: Links mixes viewing with configuration.
- Working memory: link names differ between pages; categorical colours must be
  matched against a legend.

### Findings ranked by severity

| Sev. | Finding | Source | Status |
|---|---|---|---|
| P1 | Sortable table headers were `<th onClick>` with no button or tabindex, so sorting was mouse-only (`DataTable`, used on about 15 tables). | audit | **Fixed**: a `<button>` in the `<th>`, with `aria-sort` kept. |
| P1 | Alerts at 390 px clipped the Actions column ("Ac…"); on-call staff could not ack or resolve from a phone. | DOM audit + critique | **Fixed**: the target sits under the rule name; actions wrap. |
| P1 | Resolve was an icon-only "×" in a 26×18 button, with `aria-label` on the svg, placed next to Ack. | critique | **Fixed**: a "Resolve" text button; row actions are 28 px. |
| P1 | The incident context was lost on drill-down. IP detail for 203.0.113.10, a SYN-flood target, said nothing about the attack. | critique | **Fixed**: `HostAlerts` line on IP detail, in the dashboard's words and colours. Silent when the host is quiet. |
| P1 | At 390 px the active time preset (24h by default) was scrolled out of view, with no scrollbar. | critique + screenshots | **Fixed**: the active preset is scrolled into view. The Custom popover no longer overflows a phone. |
| P1 | Headings jumped from h1 to h3 on 19 pages. | detector | **Fixed** (see section 2). |
| P1 | Feature-gated pages reached by URL with the flag off: `/bgp` showed three "Failed to load … Not Found" panels, a live "Block IP" button and six console 404s. | DOM audit (console) | **Fixed**: `FeatureGate` route wrapper explains which setting enables the page. |
| P2 | Nav dropdowns used `role="menu"`/`menuitem` with no arrow-key support; focus could leave an open menu; Escape did not return focus. | audit | **Fixed**: disclosure pattern with `aria-controls`; closes on focus-out; Escape refocuses the trigger. |
| P2 | Nine segmented controls in three styles. Four had no `aria-pressed`, so state was conveyed by colour only. Inactive labels were 4.4:1. View-toggle buttons were 28×16 on phones. | critique + detector | **Fixed**: one `Segmented` component (Ledger spec). |
| P2 | p95 and capacity labels were drawn over the filled series and unreadable (AS detail, Link detail). The lowest y tick touched the first time label on every mirrored chart. | critique + screenshots | **Fixed**: ink labels with a background halo; 8 px between the plot and the time axis. |
| P2 | AS, IP and Link detail showed "Loading…" as text and errors as bare red text, with no retry. | audit | **Fixed**: `PageSkeleton` and `ErrorDisplay` (names what failed, offers Retry). |
| P2 | A second 1h/6h/24h/7d picker on AS and IP detail duplicated the global time bar; both wrote the same filter. | critique | **Fixed**: removed. |
| P2 | Link detail coloured p95 utilisation amber or red with no word (Ledger: never colour alone). | screenshots | **Fixed**: `utilLevel` thresholds plus a "Watch"/"Incident" badge. |
| P2 | Numbers wrapped away from their unit on phones ("2.26 / Gb/s"); Top protocols overflowed 390 px by 18 px (header row did not wrap). | DOM audit | **Fixed**: numeric cells `nowrap`, percent bar hidden below `sm`, page header rows wrap. |
| P2 | Icon-only Admin and Links buttons were about 20 px; checkboxes 14 px; Countries chart labels 11 px (Ledger floor is 12). | DOM audit | **Fixed**: 28 px buttons, 16 px checkboxes, 12 px labels. |
| P3 | Capacity units differed ("10000 Mbps", "10,000 Mbps", "1,000 Mb/s", "10 Gb/s"). | critique | **Fixed**: `formatCapacity` everywhere. |
| P3 | Title Case outliers ("Top Communicating IPs", "Link Configuration", "Top Prefixes", "Flow Search", "BGP Blocks"…); icons in only three h1s. | critique | **Fixed**: sentence case; h1 icons removed. |
| P3 | Text selection used the browser default. | polish (craft floor) | **Fixed**: cobalt `::selection`. |

## 4. What changed (commits)

1. `fix(a11y)`: keyboard sorting, nav disclosure, heading outline, target sizes.
2. `refactor(ui)`: one segmented control for every pick-one switch (`components/ui/segmented.tsx`).
3. `fix(ui)`: feature pages explain why they are empty instead of showing 404s (`components/FeatureGate.tsx`).
4. `fix(responsive)`: the active window and the alert actions stay visible on a phone.
5. `fix(charts)`: legible p95 and capacity labels; no axis-label collisions; `text-wrap`.
6. `feat(detail)`: incident context on IP detail (`components/HostAlerts.tsx`); consistent loading and error states; duplicate pickers removed.
7. `fix(a11y)`: chart titles in the heading outline; AA text on tinted controls (from the detector re-run).
8. `fix(link)`: utilisation state named, not only coloured.
9. `docs`: this report and refreshed README screenshots.

No token value changed. Two shared helpers were added: `annotationLabel` and
`X_TICK_MARGIN` in `hooks/useChartColors.ts`.

## 5. Deliberately not changed

| Finding | Why it stays |
|---|---|
| Rainbow categorical stacks on Links, AS detail and Link detail, and IPv4/IPv6 charts side by side (critique P2) | This is a change of design language, not a defect. Ledger gives no breakdown pattern for "which link carries it" (see section 6), so the owner should decide. |
| Emoji flags on Top AS and Countries | Impeccable bans emoji used as icons. These flags are country data, the alpha-2 code sits next to them, and they render at 12 px or more. An ISO-code-only variant is an option for the owner. |
| Table / Treemap / Donut view toggle | It is a feature. It is now a proper segmented control. |
| Touch targets below 44 px | WCAG 2.2 AA (2.5.8) asks for 24 px. Controls are now at least 28 px and row links are full-width table cells. 44 px is AAA or platform guidance and would cost the density Ledger asks for. |
| Global `prefers-reduced-motion` kill (impeccable flags blanket kills) | The UI's only motion is 100–150 ms colour transitions on controls; nothing is lost by cutting them. |
| Global time bar shown on pages it does not affect (Admin, Alerts) | This needs per-route layout configuration. It is not a defect in rendering. |
| Flow search keeps its own Period selector | It is form state, applied on Search, with a capped set of windows. It is not a duplicate of the global filter. It now uses the shared segmented control. |
| Bulk ack/resolve, a visible ⌘K hint, glossary for "Class" | These are feature work beyond a quality pass. |
| One JS chunk over 500 kB | Code splitting is a separate performance task. |
| Admin tab size, Alerts zero-count KPIs, the Capacity "—" for "no forecast", the cobalt heatmap ramp | Minor and taste-level; listed for the owner. |
| Critique note "Further analysis titles look like links but are plain text" | False positive: they are links (`FurtherAnalysis.tsx`), underlined in `decoration-border`. |

Observed but not investigated: on the demo stack, Top protocols shows identical
inbound and outbound figures. This is probably a property of the seeded data,
not of the UI.

## 6. Where impeccable and Ledger disagree

| Impeccable says | Ledger says | Decision |
|---|---|---|
| A breakdown by link or AS needs distinct series colours; it showed 5–10 hues. | Colour is reserved; amber and red mean states. | Kept as is (section 5). Ledger needs a sanctioned breakdown pattern. |
| 44 px touch targets | Dense tables and 4 px-radius controls | Ledger kept, with WCAG AA 24 px met (28 px controls). |
| A blanket reduced-motion override can destroy feedback. | No motion except control state. | Ledger kept: there is no feedback motion to lose. |
| "Normal stays silent" can starve Status of positive confirmation (critique). | The normal state has no colour and no badge. | Kept. A worded "All clear" line would satisfy both (proposal 8 below). |

## 7. Proposed changes to the Ledger design guide

These are not applied, because the guide lives outside this repository.

1. **Segmented control, exact recipe.**
   - Track: `bg-muted` with a 1 px `border-input`.
   - Active segment: `bg-card`, `ring-1 ring-border`, ink, 600 weight.
   - Inactive segments: **secondary** ink. Tertiary ink on the track is 4.4:1 in light mode and fails AA.
   - Size: 28 px tall; `aria-pressed` and a group label are required.
2. **Chart annotations.** Draw the label in ink with a page-background halo
   (`paint-order: stroke`, 3 px), not tertiary ink over the data. Keep 8 px
   between the plot and the time axis so the lowest negative tick of a mirrored
   chart never touches the first time label.
3. **Heading outline.** A section or card title is `h2` under the page `h1`; a
   chart title inside a section is `h3`.
4. **Tables at phone width.** Numeric cells never wrap. Decorative bars
   (percent bars) drop below `sm`. A row that cannot fit becomes a two-line
   record (identity on line 1, values and actions on line 2), as Live threats
   and Alerts now do.
5. **Minimum targets.** Controls at least 28 px; icon-only buttons 28 × 28 with
   the name on the button, not the icon; checkboxes 16 px.
6. **A state is never colour alone**, including inline values. A percentage
   coloured amber or red carries the "Watch" or "Incident" badge.
7. **Disabled features.** A page whose feature is off keeps its title and says
   which setting enables it. It never shows raw API errors.
8. **Positive confirmation where it is the question.** On a status page, one
   worded "All clear" line is allowed. Silence is right for the dashboard, not
   for the page whose purpose is to confirm health.
9. **Breakdown pattern.** Needed: per-link or per-AS stacks in greys with the
   hovered or selected series in cobalt, or small multiples in ink. Either
   replaces the current 8-hue ordinal palette in stacks.
10. **Cobalt does two jobs** (link text and the outbound series), so a cobalt
    number in a table reads as clickable. Either keep links in ink with an
    underline, or state that numbers are never links.
11. **Typography.** Captions use `text-wrap: pretty` and headings `balance`;
    `::selection` takes the accent.

## 8. Evidence

The screenshots (before and after: 21 pages × light/dark × 1440/390), the DOM
audit JSON and the raw detector JSON were kept outside the repository, in the
session's scratch directory. The README screenshots in `docs/screenshots/` were
regenerated from the same invented data with `scripts/demo/shot.js` and
compressed with pngquant and oxipng.
