-- ============================================================================
-- Fix silent under-counting in the hot 1-minute alert tables
-- ============================================================================
--
-- traffic_by_dst_1min / traffic_by_src_1min are AggregatingMergeTree, but their
-- counter columns were declared as plain UInt64 (migration 000008).
-- AggregatingMergeTree only combines AggregateFunction / SimpleAggregateFunction
-- columns; for an ordinary column it keeps ONE ARBITRARY value among the rows
-- sharing the sorting key and silently discards the rest.
--
-- The materialised views emit one row per (ts, dst_ip, protocol) per INSERT
-- block, and the collector flushes a batch every few seconds, so a single
-- 1-minute bucket receives ~12+ rows per key. After a background merge only one
-- survived. Measured on production: a 10-minute window held 67.1 GB in
-- flows_raw but only 17.2 GB in traffic_by_dst_1min — a ~4x under-count.
--
-- Every alert rule reads these tables (EvalVolumeInbound, EvalSynFlood,
-- EvalAmplification, EvalPortScan, EvalConnectionFlood, EvalProtocolFlood) and
-- so has been firing at roughly 4x its configured threshold. Live Threats
-- under-reports by the same factor.
--
-- SimpleAggregateFunction(sum, UInt64) has the same physical representation as
-- UInt64, so this ALTER is metadata-only: verified on ClickHouse 24.3 to
-- complete without scheduling any mutation (system.mutations stays empty) and
-- therefore without rewriting any part. That matters — a whole-table rewrite on
-- this volume is what previously filled the data disk.
--
-- Rows already collapsed keep their wrong value; they age out within the 7-day
-- TTL. Read queries are unchanged: sum() over SimpleAggregateFunction(sum, …)
-- is already the correct and idiomatic form.

ALTER TABLE asstats.traffic_by_dst_1min
    MODIFY COLUMN bytes      SimpleAggregateFunction(sum, UInt64),
    MODIFY COLUMN packets    SimpleAggregateFunction(sum, UInt64),
    MODIFY COLUMN flow_count SimpleAggregateFunction(sum, UInt64),
    MODIFY COLUMN syn_count  SimpleAggregateFunction(sum, UInt64);

ALTER TABLE asstats.traffic_by_src_1min
    MODIFY COLUMN bytes      SimpleAggregateFunction(sum, UInt64),
    MODIFY COLUMN packets    SimpleAggregateFunction(sum, UInt64),
    MODIFY COLUMN flow_count SimpleAggregateFunction(sum, UInt64);
