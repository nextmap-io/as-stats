# Review: BGP blackholing subsystem, October 2026

Baseline: release 3.6.1 (`6a447a2`). Scope: `internal/bgp/**`,
`internal/api/handler/bgp.go`, the block action of `/alerts/{id}/block`, the
`bgp_blocks` store and migration, the alert engine's `auto_block` path, the
BGP config, `frontend/src/pages/BGP.tsx` and the block dialogs,
`docs/bgp-blackhole.md`. File:line references point at the 3.6.1 sources.

The subsystem predated the 3.x rework and had not been revisited since. The
short version: **auto-block had never worked**, nothing stopped a block of an
arbitrary address (the BGP peer included), a block from an alert could become
permanent after a restart, and the commands ran through a shell with the API's
secrets in their environment. Everything ranked Critical and High is fixed in
this branch; see the last column.

## Findings

| # | Severity | Finding | Where (3.6.1) | Status |
|---|---|---|---|---|
| 1 | Critical | **Auto-block never worked, and had no authentication by design.** `RemoteBlocker` posted to `/api/v1/bgp/blocks` with no session, no CSRF token and no credential ("the collector and API are assumed to be on the same host"). CSRF is mounted API-wide, so every request was a 403 (401 with `AUTH_ENABLED=true`); auto_block rules only ever logged `auto-block FAILED`. Making it "work" by relaxing CSRF would have let anyone reaching the API port blackhole any address. | `internal/bgp/remote.go:20-21,52-59`; `internal/api/router.go:139-150` | Fixed: `/internal/bgp/blocks`, outside `/api/v1`, bearer `BGP_API_TOKEN` (≥ 32 chars, constant-time compare, registered only when set, rate-limited, not proxied by nginx); `bgp.Client` refuses redirects. |
| 2 | Critical | **No target validation.** Any parseable address was announced: `0.0.0.0`, loopback, multicast, the BGP peer or next hop (dropping the session and with it every blackhole), router loopbacks, other networks' space. An admin typo or a mis-scoped rule (`target_filter` can be any CIDR) was one click from an outage. | `internal/api/handler/bgp.go:74-78`; `internal/api/handler/alerts.go:163-170`; `internal/alerts/engine.go:513` | Fixed: `bgp.Policy` — allow-list (`BGP_ALLOWED_PREFIXES`, else LOCAL_AS prefixes, else refuse everything), protected peer / next hops / router id / `BGP_PROTECTED_PREFIXES`, reserved ranges, IPv4-mapped unmapping, host routes by default with floors /24 and /48. |
| 3 | Critical | **A block from an alert was never persisted.** `/alerts/{id}/block` called `Announce` only: the block did not appear on the BGP page, had no history or audit row, and its expiry lived in an in-memory timer. After an API restart the route stayed in the daemon with nothing left to withdraw it — a permanent blackhole until someone used the daemon CLI. | `internal/api/handler/alerts.go:170` | Fixed: all sources go through `Service.Block`, which writes the row (with the alert id, rule and metrics) before announcing. |
| 4 | High | **Shell execution and environment leak.** Templates were string-substituted and run with `sh -c`; the child inherited the whole API environment (`CLICKHOUSE_PASSWORD`, `OIDC_CLIENT_SECRET`, …). Values were `net.IP`-derived so not remotely injectable today, but one new placeholder or input path away from it. | `internal/bgp/script.go:332-343,350,363` | Fixed: templates parsed once into argv (POSIX quoting), `exec` without shell, unquoted shell operators and unknown placeholders refused at startup, environment reduced to `PATH`, `LANG=C` and `ASSTATS_BGP_*`. |
| 5 | High | **No rate limit, no cap.** One goroutine per violation, no limit on active blocks or blocks per hour: a runaway rule (bad threshold, an amplification hitting many hosts) could blackhole every address in the network. | `internal/alerts/engine.go:513-514`; `internal/bgp/script.go:159-207` | Fixed: `BGP_MAX_ACTIVE_BLOCKS` (50), `BGP_AUTO_BLOCK_MAX_PER_HOUR` (10, rebuilt from `bgp_blocks` at start so a restart does not reset it), duration bounds. 429 with the variable named. |
| 6 | High | **Restart and crash safety.** Re-announce at start was skipped silently when ClickHouse was unreachable (then expired blocks were never withdrawn and the API believed nothing was active); `CreateBGPBlock` announced before inserting, so an insert failure left an untracked route; expiry relied on per-route timers. | `internal/bgp/script.go:96-102,370-418`; `internal/api/handler/bgp.go:117-125` | Fixed: row first, route second (`failed` on announce error); the loop retries loading (blocking answers 503 meanwhile), withdraws expired blocks, retries failed withdrawals and store updates, keeps the original expiry. |
| 7 | High | **Withdraw answered "unblocked" for anything.** An address not in the in-memory map (e.g. after a restart without store) returned 200 while the route stayed announced. | `internal/bgp/script.go:223-227`; `internal/api/handler/bgp.go:152-162` | Fixed: 404 when nothing is active; a withdraw that fails is 502 unless the daemon's RIB confirms the route is gone. |
| 8 | High | **No reconciliation with the daemon.** A daemon restart (gobgpd keeps its RIB in memory) silently lost every blackhole; routes added by hand were invisible. The documented remedy was "restart the API". | — | Fixed: `BGP_ROUTES_CMD` (default for gobgp) compared every minute — missing routes re-announced, unknown routes listed and withdrawable; without it, a session returning to Established re-pushes everything. |
| 9 | High | **Audit gaps.** `/bgp/blocks` POST / DELETE were not in the audited actions, the audit middleware is only mounted with alerts or flow search, and expiry / engine actions were never recorded. | `internal/api/middleware/audit.go:115-133`; `internal/api/router.go:154` | Fixed: the service writes `bgp_block`, `bgp_unblock`, `bgp_expire` rows itself (user, client IP, target, source, alert, result, refusal reason), independent of feature flags. |
| 10 | Medium | **RFC 7999 semantics.** No NO_EXPORT / NO_ADVERTISE (RFC 7999 §3.2): a blackhole could propagate beyond the upstream. | `internal/bgp/script.go:21` | Fixed: `BGP_EXTRA_COMMUNITIES` defaults to `65535:65281`, `{communities}` in the gobgp template; communities validated. |
| 11 | Medium | **IPv6 broken.** One next hop (IPv4) for both families; the docs even showed `-a ipv4` hard-coded. IPv6 announces failed at the daemon. | `internal/bgp/script.go:339`; `docs/bgp-blackhole.md:94-95` | Fixed: `BGP_NEXT_HOP_V6`; IPv6 refused with an explicit 422 without it. |
| 12 | Medium | **Status did not reflect the daemon.** `DefaultStatusCmd` was defined but never applied, so the page always said "unknown"; the status command ran on every GET (each viewer, every 10 s); failures showed only "error". | `internal/bgp/script.go:85-90,284-296` | Fixed: default applied with the gobgp announce, cached and refreshed by the loop, "Check now" action, error text shown, `asstats_bgp_daemon_up`. |
| 13 | Medium | **Timeouts did not bound the call.** `CommandContext` killed `sh` but a child holding the output pipe kept `CombinedOutput` waiting; output was unbounded. | `internal/bgp/script.go:346-366` | Fixed: `WaitDelay`, 64 KiB output cap, `BGP_COMMAND_TIMEOUT`. |
| 14 | Medium | **Duplicates and races.** `FindActiveBlock` swallowed ClickHouse errors as "not blocked"; check-then-insert raced between concurrent requests; engine and API would both have inserted a row per auto-block. | `internal/store/bgp_blocks.go:120-134`; `internal/api/handler/bgp.go:81`; `internal/alerts/engine.go:557-581` | Fixed: the API is the only writer, operations are serialised in the service, status changes are keyed by id and cover legacy duplicate rows. |
| 15 | Medium | **Auto-block on non-host rules.** `subnet_flood` stores the aggregate's network address; auto_block would have blackholed that single (useless) address. | `internal/alerts/engine.go:513` | Fixed: the engine only asks for host rule types; the API also refuses by policy. |
| 16 | Medium | **Wrong examples in the docs.** BIRD has no `birdc 'route add …'`; the FRR example used an unsupported `{peer_as}` placeholder (and the peer's AS where the local AS belongs); no next-hop / provider guidance; config claimed a restart re-announces only unexpired blocks "with a filter" that did not exist. | `docs/bgp-blackhole.md:24,243-252` | Fixed: docs rewritten; `scripts/bgp/bird-rtbh.sh` and `frr-rtbh.sh` (tested against stub CLIs); unknown placeholders are now startup errors. |
| 17 | Medium | **Daemon errors invisible in the UI.** Every failure was a 500 "internal server error"; durations silently clamped (0 → 1 h, > 24 h → 24 h). | `internal/api/handler/bgp.go:86-93,118` | Fixed: 400 / 422 / 429 / 502 / 503 with actionable messages; command output never returned (logged). |
| 18 | Low | **UI.** `window.confirm()` with no statement of impact; auto blocks wore the green "success" colour (Ledger: colour only for abnormal states); no link from a block to its alert; history hid outcome; no dry-run state. | `frontend/src/pages/Alerts.tsx:171`; `LiveThreats.tsx:189`; `BGP.tsx:129,450` | Fixed: shared `BlockDialog` ("all traffic to X will be dropped upstream", duration, reason), BGP page rebuilt (see below). |
| 19 | Low | **Startup blocked by the daemon.** `NewScript` re-announced synchronously inside `NewRouter` (10 s × N blocks with a dead daemon before the API served anything). | `internal/bgp/script.go:77-155`; `internal/api/router.go:91` | Fixed: restore runs in the background loop. |
| 20 | Low | `BGP_ROUTER_ID` required but unused; config errors fell back to a log-only blocker at runtime. | `internal/config/config.go:344`; `internal/api/router.go:92-96` | Fixed: optional (protected when set); every BGP setting validated at startup, the API refuses to start on a bad one. |
| 21 | Low | SQL conventions: `toString(ip) = @ip` instead of typed comparison; `LIMIT` via `Sprintf`. | `internal/store/bgp_blocks.go:76,107,128` | Fixed: typed `toIPv6`, status by `id`, named `LIMIT`. |
| 22 | Info | `bgp_blocks` is a MergeTree with mutations, not a ReplacingMergeTree: with a single serialised writer and a few rows a day that is the right trade-off (no FINAL / bug #15 exposure, synchronous status). Kept and documented. | `migrations/000011_bgp_blocks.up.sql` | Documented. |

## Not done / residual risk

- **No live daemon in CI.** The gobgp / BIRD / FRR command lines and daemon
  configurations follow the tools' documentation; the scripts are tested
  against stub `birdc` / `vtysh`, the service against a fake daemon script.
  No gobgp / BIRD / FRR image was available locally to run them for real.
  Rehearse with `BGP_DRY_RUN=true`, then a test address.
- **gobgp `del` of an absent route**: if a gobgp version answers an error,
  the service checks the RIB and treats a missing route as withdrawn; without
  `BGP_ROUTES_CMD` the withdraw is retried until it succeeds.
- **Single API instance.** The service assumes one API process owns
  `bgp_blocks` (as the in-memory sessions already do). Two API replicas with
  BGP enabled would both reconcile; documented in CLAUDE.md.
- **Allow-list from RIPEstat** is fetched once at API start; if the fetch
  fails, every block is refused (fail closed). `BGP_ALLOWED_PREFIXES` is
  recommended in production.
- **Orphan routes are never withdrawn automatically** (a human decision);
  they are reported (`asstats_bgp_rib_orphans`) and withdrawable from the page.

## UI (Ledger)

The BGP page answers, in order: *is the blackhole path working?* — a
"right now" band (session, active blocks / max, automatic blocks this hour,
daemon RIB in sync / missing / unknown) where the normal state carries no
colour and an abnormal one carries a word (Watch / Incident); *what is
blocked and until when?* — active blocks with target, why (link to the
alert), who, since, expiry, and an inline withdraw confirmation stating that
traffic flows again; routes the daemon carries without a block; *how do I
block?* — a form that only takes the address and opens the shared dialog;
*which rules apply?* — the allow-list, protected addresses, lengths, limits,
communities and next hops in force; then the history with outcome (expired,
withdrawn, failed). The disabled-feature state (`FeatureGate`) is unchanged.
Not yet verified visually at 390 px / dark theme in a browser in this pass.
