-- The composite PK (org_id, user_id) on org_members covers lookups by org_id,
-- but ListForUser queries by user_id alone (no org_id prefix), so Postgres has
-- to do a full table scan. Add a covering index for that access pattern.
CREATE INDEX IF NOT EXISTS idx_org_members_user_id ON org_members(user_id);
