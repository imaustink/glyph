-- Reverse migration for 000012_folder_board.up.sql

-- Restore the original shares resource_type check constraint.
ALTER TABLE shares DROP CONSTRAINT IF EXISTS shares_resource_type_check;
ALTER TABLE shares ADD CONSTRAINT shares_resource_type_check
    CHECK (resource_type IN ('page', 'task', 'template'));

-- Remove the folder_id column and its index from tasks.
DROP INDEX IF EXISTS tasks_folder_id_idx;
ALTER TABLE tasks DROP COLUMN IF EXISTS folder_id;

-- Remove the folder_id column and its index from lanes.
DROP INDEX IF EXISTS lanes_folder_id_idx;
ALTER TABLE lanes DROP COLUMN IF EXISTS folder_id;
