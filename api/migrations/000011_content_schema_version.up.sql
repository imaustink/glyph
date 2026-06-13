-- Add schema_version to page_contents to track the ProseMirror document schema.
-- Existing rows are stamped as version 1 (first versioned schema).
ALTER TABLE page_contents ADD COLUMN schema_version INTEGER NOT NULL DEFAULT 1;
