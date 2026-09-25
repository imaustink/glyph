-- Task ↔ bullet integrity, in preparation for realtime collaborative editing.
--
-- 1. Soft delete. Deleting a bullet used to hard-delete its task from the
--    client that noticed the bullet was gone. With more than one editor that
--    is unsafe: a cut/paste, an undo, or a concurrent edit makes every
--    connected client observe a "removed" bullet. Tasks are now soft-deleted,
--    and the server restores a task whose bullet comes back.
--
--    deleted_reason distinguishes an explicit delete ('user', never
--    auto-restored) from one the server inferred from the document
--    ('source_removed', restored when the bullet reappears).
--
-- 2. One task per bullet. Two clients seeing the same new TODO bullet both
--    created a task for it. (source_page_id, source_node_id) is now unique, so
--    task creation for a bullet is idempotent.

ALTER TABLE tasks ADD COLUMN deleted_at TIMESTAMPTZ;
ALTER TABLE tasks ADD COLUMN deleted_reason TEXT
    CHECK (deleted_reason IS NULL OR deleted_reason IN ('user', 'source_removed'));
ALTER TABLE tasks ADD CONSTRAINT tasks_deleted_consistent
    CHECK ((deleted_at IS NULL) = (deleted_reason IS NULL));

-- Pre-existing duplicates must be resolved before the unique index can be
-- built. Nothing is deleted: surplus tasks are unlinked from the bullet
-- (source_node_id cleared) and remain on the board as standalone tasks. The
-- task that survives as the bullet's link is the one the document itself
-- references via attrs.taskId, falling back to the oldest.
WITH ranked AS (
    SELECT t.id,
           row_number() OVER (
               PARTITION BY t.source_page_id, t.source_node_id
               ORDER BY
                   EXISTS (
                       SELECT 1 FROM page_contents pc
                       WHERE pc.page_id = t.source_page_id
                         AND jsonb_path_exists(
                               pc.content,
                               '$.** ? (@.attrs.taskId == $tid)',
                               jsonb_build_object('tid', t.id::text))
                   ) DESC,
                   t.created_at ASC,
                   t.id ASC
           ) AS rn
    FROM tasks t
    WHERE t.source_page_id IS NOT NULL AND t.source_node_id IS NOT NULL
)
UPDATE tasks
SET source_node_id = NULL
FROM ranked
WHERE tasks.id = ranked.id AND ranked.rn > 1;

-- Deliberately includes soft-deleted rows: re-creating a task for a bullet
-- whose task was soft-deleted restores that task (keeping its status, due
-- date, description…) instead of minting a second one.
CREATE UNIQUE INDEX tasks_source_page_node_uniq
    ON tasks (source_page_id, source_node_id)
    WHERE source_page_id IS NOT NULL AND source_node_id IS NOT NULL;

-- Reconciliation looks up a page's live tasks on every content write.
CREATE INDEX tasks_source_page_live_idx
    ON tasks (source_page_id)
    WHERE deleted_at IS NULL;
