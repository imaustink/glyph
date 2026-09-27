-- Convert page_contents.content from TEXT to JSONB.
-- Drop the existing empty-string default first so PostgreSQL doesn't try to
-- cast '' to jsonb automatically when changing the column type.
ALTER TABLE page_contents ALTER COLUMN content DROP DEFAULT;

ALTER TABLE page_contents
  ALTER COLUMN content TYPE jsonb
  USING CASE
    -- '' was the old TEXT default. The column stays NOT NULL, so it must
    -- become an empty document rather than NULL (NULL aborted this migration
    -- on any database that still had such a row).
    WHEN content = '' THEN '{"type":"doc","content":[]}'::jsonb
    ELSE content::jsonb
  END;

-- Update the default to a proper empty ProseMirror doc instead of empty string.
ALTER TABLE page_contents
  ALTER COLUMN content SET DEFAULT '{"type":"doc","content":[]}'::jsonb;
