-- Convert page_contents.content from TEXT to JSONB.
-- Drop the existing empty-string default first so PostgreSQL doesn't try to
-- cast '' to jsonb automatically when changing the column type.
ALTER TABLE page_contents ALTER COLUMN content DROP DEFAULT;

ALTER TABLE page_contents
  ALTER COLUMN content TYPE jsonb
  USING CASE
    WHEN content = '' THEN NULL
    ELSE content::jsonb
  END;

-- Update the default to a proper empty ProseMirror doc instead of empty string.
ALTER TABLE page_contents
  ALTER COLUMN content SET DEFAULT '{"type":"doc","content":[]}'::jsonb;
