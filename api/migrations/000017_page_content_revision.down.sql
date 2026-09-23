DROP INDEX IF EXISTS page_content_versions_page_replaced_idx;
DROP TABLE IF EXISTS page_content_versions;
ALTER TABLE page_contents DROP COLUMN IF EXISTS revision;
