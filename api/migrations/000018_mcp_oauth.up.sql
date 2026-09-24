-- OAuth changes backing the remote MCP server (/mcp).
--
-- 1. Dynamic client registration (RFC 7591). MCP clients (Claude Code,
--    Claude.ai, Cursor, …) register themselves at POST /oauth/register with no
--    logged-in user, so a self-registered client has no creator and belongs to
--    no org. `is_dynamic` marks these clients; the consent screen lets the user
--    pick which workspaces to grant instead of relying on oauth_client_orgs.
-- 2. Personal-workspace grants. Until now a token could only reach resources
--    inside the orgs it was granted. `include_personal` lets a user
--    additionally grant their own non-org resources on the consent screen.
--    Existing codes/tokens default to false, preserving today's org-only reach.

ALTER TABLE oauth_clients
    ALTER COLUMN created_by_id DROP NOT NULL,
    ADD COLUMN is_dynamic BOOLEAN NOT NULL DEFAULT false,
    ADD CONSTRAINT oauth_clients_creator_or_dynamic
        CHECK (is_dynamic OR created_by_id IS NOT NULL);

ALTER TABLE oauth_authorization_codes
    ADD COLUMN include_personal BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE oauth_tokens
    ADD COLUMN include_personal BOOLEAN NOT NULL DEFAULT false;

-- Supports the "connected apps" list (all live grants for one user).
CREATE INDEX oauth_tokens_acting_user_active_idx
    ON oauth_tokens(acting_user_id) WHERE revoked_at IS NULL;
