-- ─── OAuth Clients ──────────────────────────────────────────────────────────
-- Created by an org owner; scoped to one or more orgs + a resource/permission
-- scope set. Not bound to a single acting user — the acting user is chosen
-- per token request (client_credentials+subject) or via user consent (auth code).
CREATE TABLE oauth_clients (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id           TEXT        NOT NULL UNIQUE,
    client_secret_hash  TEXT        NOT NULL,
    name                TEXT        NOT NULL,
    description         TEXT,
    created_by_id       UUID        NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    grant_types         TEXT[]      NOT NULL DEFAULT '{client_credentials}'
                                     CHECK (grant_types <@ ARRAY['client_credentials','authorization_code']),
    scopes              TEXT[]      NOT NULL DEFAULT '{}',
    redirect_uris       TEXT[]      NOT NULL DEFAULT '{}',
    is_confidential     BOOLEAN     NOT NULL DEFAULT true,
    revoked_at          TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX oauth_clients_created_by_idx ON oauth_clients(created_by_id);

-- ─── OAuth Client Orgs ──────────────────────────────────────────────────────
-- Which orgs a client is scoped to; one client can be scoped to multiple orgs.
CREATE TABLE oauth_client_orgs (
    client_id   UUID        NOT NULL REFERENCES oauth_clients(id) ON DELETE CASCADE,
    org_id      UUID        NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    added_by_id UUID        NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (client_id, org_id)
);

CREATE INDEX oauth_client_orgs_org_idx ON oauth_client_orgs(org_id);

-- ─── OAuth Authorization Codes ──────────────────────────────────────────────
-- Single-use codes for the authorization_code + PKCE flow.
CREATE TABLE oauth_authorization_codes (
    id                    UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    code_hash             TEXT        NOT NULL UNIQUE,
    client_id             UUID        NOT NULL REFERENCES oauth_clients(id) ON DELETE CASCADE,
    user_id               UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    redirect_uri          TEXT        NOT NULL,
    scopes                TEXT[]      NOT NULL DEFAULT '{}',
    org_ids               UUID[]      NOT NULL DEFAULT '{}',
    code_challenge        TEXT        NOT NULL,
    code_challenge_method TEXT        NOT NULL DEFAULT 'S256' CHECK (code_challenge_method IN ('S256')),
    expires_at            TIMESTAMPTZ NOT NULL,
    used_at               TIMESTAMPTZ,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX oauth_auth_codes_client_idx ON oauth_authorization_codes(client_id);
CREATE INDEX oauth_auth_codes_expires_idx ON oauth_authorization_codes(expires_at);

-- ─── OAuth Tokens ───────────────────────────────────────────────────────────
-- Opaque, hashed access/refresh tokens. One row per issued grant; refresh
-- rotation replaces both hashes atomically on the same row.
CREATE TABLE oauth_tokens (
    id                        UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    access_token_hash         TEXT        NOT NULL UNIQUE,
    refresh_token_hash        TEXT        UNIQUE,
    client_id                 UUID        NOT NULL REFERENCES oauth_clients(id) ON DELETE CASCADE,
    acting_user_id            UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    grant_type                TEXT        NOT NULL CHECK (grant_type IN ('client_credentials','authorization_code')),
    scopes                    TEXT[]      NOT NULL DEFAULT '{}',
    org_ids                   UUID[]      NOT NULL DEFAULT '{}',
    access_token_expires_at   TIMESTAMPTZ NOT NULL,
    refresh_token_expires_at  TIMESTAMPTZ,
    revoked_at                TIMESTAMPTZ,
    last_used_at              TIMESTAMPTZ,
    created_at                TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX oauth_tokens_client_idx ON oauth_tokens(client_id);
CREATE INDEX oauth_tokens_acting_user_idx ON oauth_tokens(acting_user_id);
CREATE INDEX oauth_tokens_active_idx ON oauth_tokens(client_id) WHERE revoked_at IS NULL;
