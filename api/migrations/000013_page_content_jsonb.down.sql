ALTER TABLE page_contents
  ALTER COLUMN content TYPE text
  USING COALESCE(content::text, '');

ALTER TABLE page_contents
  ALTER COLUMN content SET DEFAULT '';
