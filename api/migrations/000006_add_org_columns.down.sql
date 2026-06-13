DROP INDEX IF EXISTS templates_org_id_idx;
DROP INDEX IF EXISTS lanes_org_id_idx;
DROP INDEX IF EXISTS tasks_org_id_idx;
DROP INDEX IF EXISTS pages_org_id_idx;

ALTER TABLE templates  DROP COLUMN IF EXISTS is_private;
ALTER TABLE templates  DROP COLUMN IF EXISTS org_id;

ALTER TABLE lanes      DROP COLUMN IF EXISTS is_private;
ALTER TABLE lanes      DROP COLUMN IF EXISTS org_id;

ALTER TABLE tasks      DROP COLUMN IF EXISTS is_private;
ALTER TABLE tasks      DROP COLUMN IF EXISTS org_id;

ALTER TABLE pages      DROP COLUMN IF EXISTS is_private;
ALTER TABLE pages      DROP COLUMN IF EXISTS org_id;
