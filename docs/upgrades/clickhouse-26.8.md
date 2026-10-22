# Upgrading ClickHouse 24.3 LTS → 26.8 LTS

Prepared 2026-10-22 against AS-Stats 3.5.1. Nothing here has been run on
production. Every result below comes from a throwaway rehearsal in Docker.

| | |
|---|---|
| **From** | `clickhouse/clickhouse-server:24.3-alpine` (24.3.18.7). Upstream support ended in early 2025, and SECURITY.md lists all of 24.x as unsupported. |
| **To** | `clickhouse/clickhouse-server:26.8.33.10-alpine` (26.8 LTS, released 2026-08-27). Pin the full tag. `26.8-alpine` also tracks the line. |
| **Support** | LTS lines are supported for one year, so 26.8 is covered until about August 2027, when 27.8 ships. 26.3 LTS (the previous LTS) is only covered until about March 2027. |
| **Image** | Alpine is still published for amd64 and arm64. 26.3 added distroless variants; we don't need them. |
| **Switch** | `CLICKHOUSE_IMAGE` in `.env`. The default stays `24.3-alpine` until the operator changes it. |

## Decisions for the owner

1. **The VM CPU must support AVX2. This blocks the upgrade until checked.**
   - Since 26.6 (ClickHouse #105019), the amd64 binary is built for x86-64-v3: AVX2, BMI2 and FMA (Haswell or newer). On an older CPU it dies with `Illegal instruction`.
   - There is no `amd64compat` Docker image. That build only exists as a raw binary.
   - Proxmox's default VM CPU type is `x86-64-v2-AES` (PVE 8), and it does **not** expose AVX2.
   - Check inside the VM: `grep -c avx2 /proc/cpuinfo`. The result must be greater than 0.
   - If it is 0, set the VM CPU type to `host` (or `x86-64-v3`) in Proxmox. This needs a full VM stop and start, so do it in the same window, before taking the snapshot.
   - If the physical host itself has no AVX2, the target becomes **26.3 LTS**. 26.3 was not tested here: the rules for this work allowed pulling only one new image. Re-run the integration suite on it first, since CI takes any tag.
2. **Which 26.8 patch to use.**
   - ClickHouse #121124 is a TTL bug in 26.8: a TTL-drop merge can empty a part that has *stale non-row TTL info*. That happens after `ATTACH PARTITION … FROM` or after a column TTL is removed.
   - AS-Stats does neither. Pre-check 13 below confirms it on production.
   - The fix (#123068) is merged to master. The 26.8 backport (#125243) was still open on 2026-10-22.
   - If a 26.8 patch containing #125243 is out by the upgrade date, use it. Otherwise 26.8.33.10 is acceptable once pre-check 13 comes back empty.
3. **Rollback strategy.**
   - From the first write onwards, 26.8 writes parts in formats that 24.3 cannot read (see Rollback). A Proxmox snapshot of **both** disks is the main safety net.
   - The rehearsal also proved a second path: run 26.8 with `merge-tree-downgrade-compat.xml` during a soak period. This is the only rollback path if `/data-cold` cannot be snapshotted (for example a passthrough HDD).
   - Recommendation: do both, and soak for 3–7 days.

## What changed between 24.3 and 26.8 that concerns us

I read every "Backward Incompatible Change" section from 24.4 to 26.8, plus `system.settings_changes` of 26.8 (1,195 default changes since 24.4). The table below lists only the items that touch this stack.

| Version | Change | Effect on AS-Stats | Status |
|---|---|---|---|
| 26.6 | x86-64-v3 (AVX2) binaries | The server does not start on a CPU without AVX2 | **Pre-check (decision 1)** |
| 26.2 | `async_insert` default 0 → 1 | Without a fix, the collector's native batch INSERTs and the `schema_migrations` events went through the async buffer (seen in `system.asynchronous_insert_log`) | **Fixed**: `async_insert = 0` is pinned on the store and migrator connections (`TestIntegrationInsertsAreSynchronous`) |
| 26.2 | `deduplicate_insert = enable`, `deduplicate_blocks_in_dependent_materialized_views = 1` | Only applies with `non_replicated_deduplication_window > 0` (we use 0). Inserting an identical block twice still stores both copies (tested) | None |
| 26.8 | A TTL-drop merge writes an *empty* part (rows = 0, `min_time` 1970) until cleanup | Admin › Storage lost "oldest data" for TTL'd tables. `PortHistorySince` and the follower check would have counted an emptied legacy table as still holding data | **Fixed**: those `system.parts` reads now ignore empty parts. The sweeps already filtered `min_time > 0` |
| 25.10 | `EXPLAIN` draws a tree (`│ ├──`) for multi-child steps | Test helper only (flow-log migration test). Index use is unchanged: 3/37 granules on 26.8 vs 2/37 on 24.3 | **Fixed** (test) |
| 24.8 → 26.8 | `text_log`, `error_log`, `query_metric_log`, `background_schedule_pool_log` are on by default with no TTL. `text_log` is at trace level | `text_log` logs ~1.3 M rows a day on an *idle* 26.8 and grows without bound on the data disk (the 12 GiB incident again) | **Fixed**: `system-logs-ttl.xml` caps all four at 7 days and sets `text_log` to `information`. Verified on both versions |
| 26.8 | System log schemas changed | The first start renames `query_log`, `part_log`, `metric_log`, `asynchronous_metric_log`, `trace_log`, `processors_profile_log` and `query_views_log` to `*_0`. Those keep their 7-day TTL | Drop them after the soak (runbook step 12). They also block a rollback |
| 25.8 / 25.11 / 26.3 / 26.8 | New part formats: substream marks in compact parts, `with_size_stream` strings, serialization-info v1, packed skip indexes, auto statistics | 24.3 cannot read data that 26.8 has written | See Rollback; `merge-tree-downgrade-compat.xml.example` |
| 25.10 | Formatter no longer rewrites functions ↔ operators | Risk to `parseTableTTL`, which reads the normalised `create_table_query` | **No change**: the string is byte-identical on 26.8 (below) |
| 25.1 | Merge tables use the union of the children's columns | `flows_log_all` | No change: `DESCRIBE flows_log_all` is identical on both versions |
| 25.4 | `CREATE MATERIALIZED VIEW … TO` checks the SELECT against the target | Fresh installs run every migration on 26.8 | All 18 migrations apply on 26.8 (fresh and in-place) |
| 26.7 | AggregatingMergeTree rejects columns that are neither key nor aggregate | `traffic_by_{dst,src}_1min` | OK since 000015 (fresh install passes). Keep it that way |
| 25.4 | SummingMergeTree no longer sums key columns | Rollups | Correct for us; no change |
| 24.6 / 25.2 | `async_load_databases` on | The server accepts connections before tables are loaded, so the compose healthcheck can pass early | Queries wait for their table. Rehearsal: all tables loaded 0.06–0.3 s after the first `SELECT 1`. `migrate` has a 30 min timeout |
| 26.5 | `date_time_input_format = best_effort` | Native typed parameters are unaffected | None |
| 26.8 | `max_insert_threads` 1 → auto | INSERT…SELECT only (migrations copy nothing) | None |
| Docker image | User / password entrypoint logic | Unchanged between the two tags. The compose healthcheck (`clickhouse-client --query "SELECT 1"`) works on both | Tested |

**No incompatibility entry** exists for any of these, and the rehearsal confirmed each one:
- uniq / AggregateFunction states and `SimpleAggregateFunction(sum, UInt64)` (identical `system.columns` types)
- `toStartOfFifteenMinutes`, `toIPv6`, `isIPAddressInRange`
- EXCHANGE TABLES / RENAME
- `MOVE PARTITION … TO VOLUME`, `prefer_not_to_merge`, `move_factor`
- the storage-policy config keys
- the `<ttl>` syntax of system logs
- clickhouse-go v2.49 over the native protocol

The analyzer was already the default in 24.3.

**Coming after 26.8, not part of this upgrade:**
- 26.9: the analyzer becomes mandatory; the default codec becomes ZSTD(3).
- 26.10: strict `storage_configuration` validation; `SummingMergeTree … FINAL` keeps all-zero rows.

### `create_table_query` TTL strings (24.3 and 26.8 print the same bytes)

```
flows_log   … TTL ts + toIntervalDay(14) TO VOLUME 'cold', ts + toIntervalDay(180) SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1, storage_policy = 'hot_cold'
flows_raw   … TTL toDateTime(timestamp) + toIntervalDay(3) SETTINGS index_granularity = 8192
traffic_by_dst_1min … TTL ts + toIntervalDay(7) SETTINGS index_granularity = 8192
```

On the upgraded volume, a reconciler cycle on 26.8 issued **0 ALTERs**. After `cold_after_days` was changed from 7 to 5, it rewrote the TTL (`ts + toIntervalDay(5) TO VOLUME 'cold', ts + toIntervalDay(180)`), moved two partitions of `flows_log_legacy` to cold, and the next cycle was a no-op again.

## Rehearsal and results

### Setup (production-like, all in Docker)

- **Server:** 24.3.18.7 with two volumes: hot `/var/lib/clickhouse` and cold `/var/lib/clickhouse-cold`. It used the shipped `hot_cold` policy (`default` + `cold`, `prefer_not_to_merge`, `move_factor 0`), `system-logs-ttl.xml`, and `CLICKHOUSE_USER/PASSWORD` as in compose.
- **Migrations up to 000016**, then 15 days of the demo traffic (`scripts/demo`).
- **Cold tier:** `cold_after_days` 7 on `flows_log` and 20 on `traffic_by_port`. Reconciler cycles adopted `hot_cold` and moved 8 partitions to cold; one `traffic_by_port` part was moved by hand.
- **Migrations 000017 and 000018** (the EXCHANGE migrations), then 3 more hours of dense traffic plus the attack scenarios, then reconcile. The result matched production's shape:
  - `flows_log_legacy` was half on cold.
  - `traffic_by_port_legacy` was mostly on cold.
  - New `flows_log` and `traffic_by_port` had been adopted into `hot_cold`.
- **Size:** 1.7 GiB hot + 311 MiB cold; 283 active / 810 total parts; 3.4 M rows in `flows_raw`; 13 M in `flows_log_legacy`; 13.6 M in `traffic_by_port_legacy`.
- **Upgrade itself:** 24.3 stopped cleanly (`docker stop`, 0.6 s). The volumes were copied, then **26.8 started on the original volumes (in place)** while **24.3 started on the copy**. Two API instances (this branch) ran against them and every request was sent to both at the same moment, so the time-dependent endpoints (Live Threats, capacity "now") compare as well.

### In-place upgrade checks (26.8 on the 24.3 volume)

| Check | Result |
|---|---|
| Server start | Accepting queries 0.77 s after `docker run`; all tables loaded at 0.84 s |
| Tables / engines / sorting keys / TTLs / storage policies | Identical to 24.3 (state dump diff: only the version and TTL-expiry row counts differ) |
| Detached parts, readonly tables, pending mutations | None / none / 0 |
| Parts per disk | Identical. Cold parts stayed on `cold`: `flows_log_legacy` 15 cold / 15 hot, `traffic_by_port_legacy` 1 / 2 |
| `system.warnings` | Only "Delay accounting is not enabled" |
| Server log errors | No new kinds. `Listen [::]` warnings (IPv6 is off in Docker) and the cgroup memory observer appear on both versions |
| `system.errors` | `TYPE_MISMATCH` counter (376), from the API's `audit_log` INSERT … VALUES taking the expression-parsing fallback. The query succeeds; 24.3 counts the same fallback as `CANNOT_PARSE_INPUT_ASSERTION_FAILED`. Keep it in mind if you alert on `system.errors` |
| Migrations runner | `DetectBaseline` = 18, history current, `migrate` → "applied in 67 ms" (nothing to do) |
| Retention reconciler | No-op (0 ALTERs in `query_log`) |
| MVs | The same 22,500-row INSERT into `flows_raw` on both servers gave identical sums in `traffic_by_as`, `_ip`, `_port`, `flows_log_all`, `traffic_by_dst_1min`/`_src_1min` (incl. `uniqMerge`) |
| Cold moves / drops | Reconciler `MOVE PARTITION … TO VOLUME 'cold'`, manual MOVE and `DROP PARTITION ID` all OK |
| EXCHANGE migrations on 26.8 | `TestIntegrationFlowLogMigration` and `TestIntegrationPortStatsMigration` (4 concurrent inserters across the swap, exact totals) pass |

### API golden outputs (53 endpoints, 26.8 vs 24.3 at the same instant)

- **47 byte-identical.** These cover overview, top AS 3h/1d/30d, top IP internal/external, prefixes, countries, links, links traffic 3h/7d, capacity 7d, link detail, load curve, AS detail/peers/IPs, IP detail, all movers/talkers, heatmap, **Live Threats in/out/15 min**, **flow search by dst/src/port incl. a legacy-only window**, flow time series, conversations, top port/protocol, status, alerts, rules, search and anomaly explain.
- **6 differences, all explained:**
  - `capacity_now`, `links`: `meta.from/to` is the request time (nanoseconds apart).
  - `status.db_size`, `admin/storage`: part sizes and counts differ because merges and TTL expiry ran at different moments on two live servers. `oldest_data` was missing on 26.8 (the empty-part change above, now fixed).
  - `top/as/traffic`: the order of the per-link series inside an AS is not deterministic. It also differs between two runs on 24.3, and the payloads are identical after sorting by link.

### Integration suite

`go test ./internal/... -run Integration` (13 tests, cold-tier policy configured, so nothing is skipped) **passes on 24.3.18.7 and 26.8.33.10**. Before the test fix, one test failed on 26.8 because of the EXPLAIN tree format. CI now runs both versions (`go-integration` job).

### Performance (same data, sequential, alternating servers, cache-busted, median of 7–8 runs, end-to-end API ms)

| Request | 26.8 | 24.3 |
|---|---:|---:|
| Live Threats in (all) | 44 | 46 |
| Live Threats out (all) | 35 | 36 |
| Flow search dst IP, 3 h | 11 | 12 |
| Flow search dst IP, 7 d (via `flows_log_all`) | 53 | 53 |
| Flow search src IP, 7 d | 60 | 55 |
| Links capacity 7 d | 13 | 13 |
| Top AS 30 d | 17 | 16 |
| IP detail 3 h | 33 | 33 |
| Top IP external 7 d | 137 | 100 |

Everything is within noise except **Top IP external (+37 %)**. In `query_log` that query read 6.25 M rows on 26.8 vs 5.75 M on 24.3 (different part layout after the TTL merges that had just run) and used 11 MiB of memory vs 2.4 MiB. Per-query memory is generally higher on 26.8 (MiB, not GiB).

The dataset is small (13 M flow-log rows). Re-check `/top/ip?scope=external` and Flow Search on production after the upgrade (see "Verify after").

### Restart / downtime measurements

| | Result |
|---|---|
| 24.3 `docker stop` | 0.6 s |
| 26.8 `docker stop` | 2.3–2.9 s |
| 26.8 start → accepting queries | 0.77–0.80 s |
| 26.8 start → all tables loaded | 0.84–1.06 s (1,162 to 4,240 parts, SSD, warm page cache) |
| First start on the 24.3 volume | 0.77 s: no extra one-time conversion. The system log tables are rotated lazily, at their first flush |

**Extrapolation to production** (~100 GiB, part of it on the HDD):
- **Part count, not bytes, drives startup.** Run pre-check 8.
- Loading reads a handful of small files per part. On SSD that cost does not show at 4 k parts. Cold-HDD parts with a cold page cache cost a few ms of seeks each, partly in parallel.
- **Expect ClickHouse to be unavailable for 5–60 s.** With `async_load_databases` it accepts connections before the last table is loaded.
- **Ingestion gap for the whole runbook (stop collector → collector back): about 1–2 min** with the image pre-pulled.
- If the Proxmox CPU type must change, the VM restart (2–5 min) adds to that.

### What the collector loses during the restart (1,300 flows/s NetFlow v5, 15 s flush)

| Procedure | Rehearsal result |
|---|---|
| Collector keeps running, ClickHouse restarted under it | Run 1: **0 flows lost**, because the 6.4 s outage fell between two flushes. Run 2: **19,100 flows (14.7 s) lost**: the batch in flight was cut ("failed to read first block packet"), then dropped and counted. Never retried by design |
| Collector stopped first (graceful: last batch flushed in 0.2 s), restarted after ClickHouse + `migrate` | **3,290 flows (2.5 s) lost** = exactly the time it was down |

- **Keeping the collector running** costs 0 or one full batch per flush that falls in the outage: random, ≤15 s for a short outage, more for a long one.
- Its in-flight INSERT is cut by the server shutdown. CLAUDE.md warns that a cut INSERT can be partially committed across the MVs; that did not happen here, but nothing prevents it.
- **Recommendation: stop the collector first** (`stop_grace_period` 60 s, it flushes) and start it after `migrate`. The loss equals the window, is deterministic, and no INSERT is cut.

## Rollback (tested)

| Path | Result |
|---|---|
| 24.3 on a volume 26.8 wrote to with **default** settings | **24.3 does not start (exit 183)**. System tables: `Unknown mark file extension: '4'`, `Type of Map key … Float64` (new `metric_log`), `Unknown version of serialization infos (1)` → part detached as broken. With the system logs removed, **every `asstats` table** fails the same way (`Unknown mark file extension: '4'` on `traffic_by_*`, `links`, `schema_migrations`…). **Not recoverable in place → restore the snapshot.** |
| 26.8 run with `merge-tree-downgrade-compat.xml` (insert, `OPTIMIZE FINAL` on every table, move to cold), then system log tables removed offline, then 24.3 | **24.3 starts, no detached parts, every row count identical, rows written by 26.8 readable, inserts OK, cold part readable.** |

Notes on the compat file:
- Its setting names are unknown to 24.3, which refuses to start with it mounted (exit 115). Remove the mount when you roll back.
- It only covers parts written **while it is mounted**. Remove it at the end of the soak, then restart.

## Pre-checks (on production, 24.3, the day before and again in the window)

Run as the `asstats` user: `docker compose … exec clickhouse clickhouse-client -u asstats --password "$CLICKHOUSE_PASSWORD" --multiquery < precheck.sql`.

```sql
SELECT '1 version', version(), uptime();
SELECT '2 detached parts', database, table, reason, count() FROM system.detached_parts GROUP BY ALL ORDER BY ALL;
SELECT '3 mutations not done', database, table, mutation_id, command, latest_fail_reason FROM system.mutations WHERE NOT is_done;
SELECT '4 merges running', database, table, round(elapsed), round(progress, 2), formatReadableSize(total_size_bytes_compressed) FROM system.merges;
SELECT '5 moves running', database, table, part_name, target_disk_name, round(elapsed) FROM system.moves;
SELECT '6 disks', name, path, formatReadableSize(free_space), formatReadableSize(total_space), round(100 * (1 - free_space / total_space), 1) AS used_pct FROM system.disks;
SELECT '7 readonly / broken tables', database, name, engine FROM system.tables WHERE database NOT IN ('system', 'INFORMATION_SCHEMA', 'information_schema') AND engine LIKE '%MergeTree%' AND (is_temporary OR total_rows IS NULL);
SELECT '8 parts', countIf(active) AS active_parts, count() AS all_parts, uniqExact(database, table) AS tables, formatReadableSize(sumIf(bytes_on_disk, active)) AS active_bytes FROM system.parts;
SELECT '9 parts per disk', disk_name, countIf(active), formatReadableSize(sumIf(bytes_on_disk, active)) FROM system.parts GROUP BY disk_name;
SELECT '10 settings changed (server/profile)', name, value FROM system.settings WHERE changed;
SELECT '11 merge_tree settings changed', name, value FROM system.merge_tree_settings WHERE changed;
SELECT '12 storage policies', policy_name, volume_name, volume_priority, disks, prefer_not_to_merge, move_factor FROM system.storage_policies ORDER BY policy_name, volume_priority;
SELECT '13 TTL tables with parts lacking rows-TTL info', database, table, count() FROM system.parts WHERE active AND database = 'asstats' AND rows > 0 AND delete_ttl_info_max = toDateTime(0) AND table IN (SELECT name FROM system.tables WHERE database = 'asstats' AND create_table_query LIKE '% TTL %') GROUP BY ALL ORDER BY ALL;
SELECT '14 schema', max(version), argMax(status, (version, recorded_at)) FROM asstats.schema_migrations;
SELECT '15 system log size', formatReadableSize(sum(bytes_on_disk)) FROM system.parts WHERE active AND database = 'system';
SELECT '16 small tables for BACKUP', formatReadableSize(sum(bytes_on_disk)) FROM system.parts WHERE active AND database = 'asstats' AND table IN ('schema_migrations', 'retention_policies', 'links', 'as_names', 'api_tokens', 'alert_rules', 'webhook_configs', 'hostgroups', 'report_schedules', 'bgp_blocks', 'alerts', 'audit_log', 'traffic_by_as_daily', 'traffic_by_link_daily', 'traffic_by_as_hourly', 'traffic_by_link_hourly');
```

Go / no-go:

| Pre-check | Go condition |
|---|---|
| 2 (detached parts) | Empty, or understood |
| 3 (mutations) | Empty: never upgrade with a mutation running |
| 4 / 5 (merges, moves) | Nothing long-running; wait if needed |
| 6 (disks) | Hot disk ≥ 20 % free (merges after the restart need scratch space) |
| 7 (readonly / broken tables) | Empty |
| 8 (parts) | Note the count, to estimate start time |
| 10 / 11 (changed settings) | Anything set here (users.d / profile) must still exist in 26.8; review it |
| 13 (rows-TTL info) | Empty (decision 2) |
| 14 (schema) | Must be 18 / applied: AS-Stats 3.5.x |

Also check on the host:
- `grep -c avx2 /proc/cpuinfo` > 0.
- `docker compose … config | grep -A3 'clickhouse:'`: make sure `docker-compose.deploy.yml` does not pin its own `image:` for `clickhouse`. If it does, change it there instead of `.env`.
- `docker pull clickhouse/clickhouse-server:26.8.33.10-alpine` ahead of time (~250 MB).

## Backup plan

1. **Proxmox snapshot of the VM, both disks, with ClickHouse stopped** (runbook step 5). That makes it consistent without the RAM state.
   - On LVM-thin, ZFS, Ceph or qcow2 it takes seconds. Its space grows with the blocks rewritten afterwards: ingestion (~1 GiB/day of flow log) plus merges rewriting recent parts. Budget **5–15 GiB/day** of snapshot growth and delete it after the soak.
   - If `/data-cold` sits on a storage without snapshots (raw passthrough HDD, plain LVM), the snapshot covers the hot disk only. The compat file is then the rollback path for the cold data, and 26.8 must not move new data to cold during the soak; this is already true by design, because new parts go to cold only after `cold_after_days`.
2. **ClickHouse BACKUP of the small tables** (config plus long-term rollups, the data you cannot rebuild from the routers). Run it on 24.3 just before the window. The image already allows the `backups` path, so no config change is needed. Tested on 24.3.

   ```sql
   BACKUP TABLE asstats.schema_migrations, TABLE asstats.retention_policies, TABLE asstats.links,
          TABLE asstats.as_names, TABLE asstats.api_tokens, TABLE asstats.alert_rules,
          TABLE asstats.webhook_configs, TABLE asstats.hostgroups, TABLE asstats.report_schedules,
          TABLE asstats.bgp_blocks, TABLE asstats.alerts, TABLE asstats.audit_log,
          TABLE asstats.traffic_by_as_daily, TABLE asstats.traffic_by_link_daily,
          TABLE asstats.traffic_by_as_hourly, TABLE asstats.traffic_by_link_hourly
   TO File('backups/pre-26.8') SETTINGS id = 'pre-26.8';
   ```

   - It lands in `/var/lib/clickhouse/backups/pre-26.8` (hot disk). Copy it to the cold disk from the host, e.g. `cp -a /data/…/backups/pre-26.8 /data-cold/backups-pre-26.8`; use the real host path of the volume (`docker volume inspect`).
   - **Size:** pre-check 16 gives it. In the rehearsal it was 25 KiB.
   - Production estimate: config tables are KiB; `alerts`/`audit_log` are MiB; the hourly/daily rollups are the bulk, probably **≤ 1–2 GiB** for two years of hourly AS data at our cardinality. Confirm with pre-check 16 before relying on that number.
   - Restore onto a 24.3 server (`RESTORE TABLE … FROM File('backups/pre-26.8')`) if the snapshot is lost.

## Runbook

`C="docker compose -f docker-compose.yml -f docker-compose.prod.yml -f docker-compose.deploy.yml"`. `.env` holds `IMAGE_TAG`; leave it unchanged. Upgrade the application first (step 0), then ClickHouse alone in a separate window.

0. **Before the window:** deploy the AS-Stats release that contains this branch, while still on 24.3.
   - It pins `async_insert = 0`, handles the 26.8 empty parts, adds the system log TTLs, and adds the `CLICKHOUSE_IMAGE` switch.
   - The new `system-logs-ttl.xml` takes effect at the next ClickHouse restart. On 24.3 that would switch `text_log` on at `information` level; on 26.8 it caps `text_log`.
   - Run the pre-checks. `docker pull clickhouse/clickhouse-server:26.8.33.10-alpine`. Check AVX2.
1. **BACKUP** the small tables (above) and copy them to the cold disk.
2. `$C stop collector`: graceful, the last batch is flushed. **Flow ingestion stops here.**
3. `$C stop api frontend` (optional; otherwise the UI shows errors for a minute).
4. `$C stop clickhouse`. Check `$C logs --tail 20 clickhouse` for a clean shutdown.
5. **Proxmox snapshot** of the VM (both disks). If the CPU type must change, shut the VM down, change it to `host` / `x86-64-v3`, snapshot, boot, and check `grep -c avx2 /proc/cpuinfo`.
6. Edit `.env`: `CLICKHOUSE_IMAGE=clickhouse/clickhouse-server:26.8.33.10-alpine`.
   - **Soak (recommended):** add the compat file to the `clickhouse` service in `docker-compose.deploy.yml`. The volumes list merges by container path, so the other mounts stay.

     ```yaml
     services:
       clickhouse:
         volumes:
           - ./clickhouse/config.d/merge-tree-downgrade-compat.xml.example:/etc/clickhouse-server/config.d/merge-tree-downgrade-compat.xml:ro
     ```
7. `$C up -d clickhouse` (recreates it on 26.8), then wait for `healthy`: `$C ps clickhouse`.
8. **Verify ClickHouse** (as `asstats`):
   - `SELECT version()` → `26.8.33.10`.
   - Pre-checks 2, 3, 7 and 12 unchanged; pre-check 9 shows the same part counts per disk.
   - `SELECT * FROM system.warnings`.
   - `$C logs clickhouse | grep -E '<(Error|Fatal)>'`: only the known `Listen [::]` / cgroup lines are expected.
   - Soak only: `SELECT name, value FROM system.merge_tree_settings WHERE name IN ('string_serialization_version','write_marks_for_substreams_in_compact_parts')` → `single_stream`, `0`.
9. `$C up -d`. `migrate` runs first and must log `schema is current` (nothing to apply); collector and API start after it. **Ingestion resumes.**
10. **Verify the application:**
    - `$C logs --tail 50 collector`: no `batch write error`; `asstats_flows_dropped_total` is flat.
    - The first reconcile logs nothing: no "applied TTL", which would mean the TTL parser broke.
    - UI: Overview, Live Threats (both directions), Flow Search on a busy IP for 24 h (it spans `flows_log_legacy`), Top Ports, Admin › Storage (cold bytes, "oldest data" filled in), Capacity.
    - Timing in `system.query_log` vs. before: `/top/ip?scope=external` and Flow Search.
11. **Soak 3–7 days.** Watch:
    - disk (`system.disks`, the `disk_usage` rule)
    - `asstats_flows_dropped_total`
    - `system.errors`: a `TYPE_MISMATCH` counter from the audit-log INSERT is expected
    - query times
12. **Accept:**
    - Remove the compat mount from `docker-compose.deploy.yml`.
    - `$C stop collector && $C up -d clickhouse && $C up -d`: another ~1-minute ingestion gap.
    - Drop the leftovers the first start rotated: `SELECT name FROM system.tables WHERE database = 'system' AND match(name, '_log_[0-9]+$')`, then `DROP TABLE system.<name>` for each. They hold at most 7 days and would expire on their own, but the empty tables stay around.
    - Delete the Proxmox snapshot.

## Rollback

**From the snapshot.** This works whatever happened, but it loses the flows ingested since step 5.
- In Proxmox, roll back the VM to the snapshot. The `.env` in the snapshot still says 24.3.
- Start the VM, then `$C up -d`.

**During the soak, with the compat file mounted, without the snapshot.** This keeps the data written by 26.8. Rehearsed end to end.

1. `$C stop collector api frontend`, then `$C stop clickhouse`.
2. Remove the system log tables 26.8 created: metadata **and** data, server stopped. 24.3 cannot attach them, and dropping them on a running server is useless because 26.8 re-creates them at its next flush. The script below is the one used in the rehearsal; find the volume name with `docker volume ls | grep clickhouse_data`. If `/var/lib/clickhouse` is bind-mounted from a host path in `docker-compose.deploy.yml`, mount that path instead.

   ```sh
   docker run --rm -v <project>_clickhouse_data:/var/lib/clickhouse \
     -v "$PWD/scripts/clickhouse":/s:ro alpine:latest sh /s/drop-system-logs.sh
   ```

3. Remove the compat mount from `docker-compose.deploy.yml`: 24.3 refuses unknown MergeTree settings. Then set `CLICKHOUSE_IMAGE=clickhouse/clickhouse-server:24.3-alpine`.
4. `$C up -d clickhouse`, check for no detached parts (pre-check 2), then `$C up -d`.

**After the soak (compat removed).** Use the snapshot only. The BACKUP from step 1 restores the config tables and long-term rollups onto a fresh 24.3 if the snapshot is gone.

## How this was tested (reproducible)

- **Fresh install / suite:** the `go-integration` CI job, matrix `24.3-alpine` / `26.8.33.10-alpine`.
- **In-place rehearsal:**
  1. 24.3 on named volumes with `storage-hot-cold.xml.example`.
  2. `migrate` up to 16, `scripts/demo` seed of 15 days, cold tier, `migrate` 17–18, last 3 h, reconcile.
  3. Stop; copy the volumes.
  4. 26.8 on the originals and 24.3 on the copy, with two APIs and simultaneous requests to 53 endpoints, then a JSON deep diff.
- **Collector loss:** the real collector, a NetFlow v5 generator at 1,300 flows/s, `flows_raw` counts per second vs. sent.
- **Rollback:** both paths above.
