-- Page-tree data integrity (DI-02, DI-07, DI-22).

-- DI-07: a nil todoTrigger used to be written as the JSONB value null instead
-- of SQL NULL, and read back as an empty config that disables TODO detection.
UPDATE pages     SET todo_trigger = NULL WHERE todo_trigger = 'null'::jsonb;
UPDATE templates SET todo_trigger = NULL WHERE todo_trigger = 'null'::jsonb;

-- DI-02: content history must outlive its page. Deleting a page used to
-- cascade to page_content_versions, so a deleted note (or a whole folder of
-- them) was unrecoverable. The page's last content is now archived into
-- history before the delete, and history rows are kept. page_id stays as a
-- plain column (not a foreign key); the API only exposes versions of the
-- current page with that id (replaced_at >= pages.created_at).
ALTER TABLE page_content_versions DROP CONSTRAINT IF EXISTS page_content_versions_page_id_fkey;

-- DI-22: shares.resource_id has no foreign key and nothing deleted shares
-- with their resource, so a PUT re-creating a deleted page/template under
-- the same id re-granted the old recipients. Deletes now remove shares in
-- the same transaction; collect the ones already left behind.
--
-- A page whose type was changed (page↔folder) left shares of the old type,
-- which the share API (it matches on type) can no longer list or revoke.
-- Type is now immutable; re-type those shares so their owner can manage
-- them again, unless a share of the right type already exists for that
-- recipient, in which case the stale one is dropped.
UPDATE shares s SET resource_type = p.type
FROM pages p
WHERE s.resource_type IN ('page', 'folder') AND s.resource_id = p.id AND s.resource_type <> p.type
  AND NOT EXISTS (SELECT 1 FROM shares d
                  WHERE d.resource_type = p.type AND d.resource_id = s.resource_id
                    AND d.shared_with_id = s.shared_with_id);
DELETE FROM shares s USING pages p
WHERE s.resource_type IN ('page', 'folder') AND s.resource_id = p.id AND s.resource_type <> p.type;

DELETE FROM shares s WHERE s.resource_type IN ('page', 'folder')
  AND NOT EXISTS (SELECT 1 FROM pages p WHERE p.id = s.resource_id);
DELETE FROM shares s WHERE s.resource_type = 'template'
  AND NOT EXISTS (SELECT 1 FROM templates t WHERE t.id = s.resource_id);
DELETE FROM shares s WHERE s.resource_type = 'task'
  AND NOT EXISTS (SELECT 1 FROM tasks t WHERE t.id = s.resource_id);
