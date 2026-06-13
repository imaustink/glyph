-- Add folder_id to lanes so that lanes can be scoped to a folder (folder board).
-- Personal board lanes have folder_id = NULL.
ALTER TABLE lanes ADD COLUMN folder_id UUID REFERENCES pages(id) ON DELETE CASCADE;

-- Add folder_id to tasks so that tasks created directly (without a source page)
-- can be assigned to a folder board. Tasks with a source_page_id belong to their
-- page's folder implicitly; this column is only meaningful when source_page_id IS NULL.
ALTER TABLE tasks ADD COLUMN folder_id UUID REFERENCES pages(id) ON DELETE SET NULL;

-- Extend the shares resource_type check to allow folder shares.
ALTER TABLE shares DROP CONSTRAINT IF EXISTS shares_resource_type_check;
ALTER TABLE shares ADD CONSTRAINT shares_resource_type_check
    CHECK (resource_type IN ('page', 'task', 'template', 'folder'));

-- Indexes for efficient per-folder lane/task queries.
CREATE INDEX ON lanes(folder_id) WHERE folder_id IS NOT NULL;
CREATE INDEX ON tasks(folder_id) WHERE folder_id IS NOT NULL;
