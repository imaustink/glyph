-- Add a priority column to pages (notes). Drives task-board ordering:
-- tasks are sorted by their source note's priority first, then task priority.
ALTER TABLE pages ADD COLUMN priority TEXT NOT NULL DEFAULT 'none'
    CHECK (priority IN ('urgent','high','medium','low','none'));
