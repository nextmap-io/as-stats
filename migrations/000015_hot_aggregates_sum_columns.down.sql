-- Revert the hot 1-minute counter columns to plain UInt64.
--
-- This restores the under-counting described in the up migration; it exists only
-- for symmetry with the rest of the migration set.

ALTER TABLE asstats.traffic_by_dst_1min
    MODIFY COLUMN bytes      UInt64,
    MODIFY COLUMN packets    UInt64,
    MODIFY COLUMN flow_count UInt64,
    MODIFY COLUMN syn_count  UInt64;

ALTER TABLE asstats.traffic_by_src_1min
    MODIFY COLUMN bytes      UInt64,
    MODIFY COLUMN packets    UInt64,
    MODIFY COLUMN flow_count UInt64;
