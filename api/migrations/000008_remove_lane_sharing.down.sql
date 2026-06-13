-- Restore lane sharing columns (reverting 000007).
ALTER TABLE lanes ADD COLUMN IF NOT EXISTS org_id UUID REFERENCES organizations(id) ON DELETE SET NULL;
ALTER TABLE lanes ADD COLUMN IF NOT EXISTS is_private BOOLEAN NOT NULL DEFAULT false;
CREATE INDEX IF NOT EXISTS lanes_org_id_idx ON lanes(org_id) WHERE org_id IS NOT NULL;
