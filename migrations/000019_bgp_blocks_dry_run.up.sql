-- ============================================================================
-- BGP blackholing: dry-run marker on bgp_blocks
-- ============================================================================
--
-- dry_run = 1 marks a block recorded while BGP_DRY_RUN=true: as-stats went
-- through every check and recorded it, but ran no daemon command. The API
-- never re-announces such a row through the real daemon, so switching dry-run
-- off cannot turn an old rehearsal into a real blackhole.
--
-- status gains the value 'failed' (the announce command failed after the row
-- was written); LowCardinality(String) needs no schema change for it.
--
-- ADD COLUMN with a DEFAULT is metadata-only on MergeTree: existing parts are
-- not rewritten, the default is computed on read.

ALTER TABLE asstats.bgp_blocks
    ADD COLUMN IF NOT EXISTS dry_run UInt8 DEFAULT 0;
