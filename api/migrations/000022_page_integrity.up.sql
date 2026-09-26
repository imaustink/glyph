-- Page-tree data integrity (DI-02, DI-07, …).

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
