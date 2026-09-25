DROP INDEX IF EXISTS tasks_source_page_live_idx;
DROP INDEX IF EXISTS tasks_source_page_node_uniq;
-- Rolling back removes the soft-delete concept, so soft-deleted rows would
-- otherwise reappear as live tasks. Remove them to preserve the pre-migration
-- meaning of "deleted".
DELETE FROM tasks WHERE deleted_at IS NOT NULL;
ALTER TABLE tasks DROP CONSTRAINT IF EXISTS tasks_deleted_consistent;
ALTER TABLE tasks DROP COLUMN IF EXISTS deleted_reason;
ALTER TABLE tasks DROP COLUMN IF EXISTS deleted_at;
