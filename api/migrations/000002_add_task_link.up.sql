-- Add link JSONB column to tasks for storing external resource link metadata
ALTER TABLE tasks ADD COLUMN link JSONB;
