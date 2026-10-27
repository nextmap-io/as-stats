-- Drop the dry-run marker. Run only after the API with BGP_DRY_RUN support is
-- gone: an older API would re-announce dry-run rows through the real daemon,
-- so withdraw (or delete) every active dry-run row first:
--   ALTER TABLE asstats.bgp_blocks UPDATE status = 'withdrawn' WHERE dry_run = 1 AND status = 'active';

ALTER TABLE asstats.bgp_blocks
    DROP COLUMN IF EXISTS dry_run;
