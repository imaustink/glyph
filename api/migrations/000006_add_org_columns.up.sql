-- Add org_id and is_private columns to all four resource tables.
-- org_id links a resource to an organization; NULL means the resource is personal.
-- is_private hides the resource from org members (only the owner sees it).

ALTER TABLE pages      ADD COLUMN org_id     UUID    REFERENCES organizations(id) ON DELETE SET NULL;
ALTER TABLE pages      ADD COLUMN is_private BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE tasks      ADD COLUMN org_id     UUID    REFERENCES organizations(id) ON DELETE SET NULL;
ALTER TABLE tasks      ADD COLUMN is_private BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE lanes      ADD COLUMN org_id     UUID    REFERENCES organizations(id) ON DELETE SET NULL;
ALTER TABLE lanes      ADD COLUMN is_private BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE templates  ADD COLUMN org_id     UUID    REFERENCES organizations(id) ON DELETE SET NULL;
ALTER TABLE templates  ADD COLUMN is_private BOOLEAN NOT NULL DEFAULT false;

CREATE INDEX pages_org_id_idx     ON pages(org_id)     WHERE org_id IS NOT NULL;
CREATE INDEX tasks_org_id_idx     ON tasks(org_id)     WHERE org_id IS NOT NULL;
CREATE INDEX lanes_org_id_idx     ON lanes(org_id)     WHERE org_id IS NOT NULL;
CREATE INDEX templates_org_id_idx ON templates(org_id) WHERE org_id IS NOT NULL;
