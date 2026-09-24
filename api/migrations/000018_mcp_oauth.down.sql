DROP INDEX IF EXISTS oauth_tokens_acting_user_active_idx;
ALTER TABLE oauth_tokens DROP COLUMN IF EXISTS include_personal;
ALTER TABLE oauth_authorization_codes DROP COLUMN IF EXISTS include_personal;
-- Dynamic clients have no creator and cannot satisfy NOT NULL; drop them first.
DELETE FROM oauth_clients WHERE is_dynamic;
ALTER TABLE oauth_clients
    DROP CONSTRAINT IF EXISTS oauth_clients_creator_or_dynamic,
    DROP COLUMN IF EXISTS is_dynamic,
    ALTER COLUMN created_by_id SET NOT NULL;
