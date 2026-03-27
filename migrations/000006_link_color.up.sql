-- Add color field to links table for per-link chart colors
ALTER TABLE asstats.links ADD COLUMN IF NOT EXISTS color String DEFAULT '';
