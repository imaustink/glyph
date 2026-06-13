-- Remove sharing columns from lanes. Lane sharing was never exposed in the UI
-- and the concept is being dropped in favour of simpler personal-only lanes.
DROP INDEX IF EXISTS lanes_org_id_idx;
ALTER TABLE lanes DROP COLUMN IF EXISTS org_id;
ALTER TABLE lanes DROP COLUMN IF EXISTS is_private;

-- Remove any existing lane shares from the shares table.
DELETE FROM shares WHERE resource_type = 'lane';
