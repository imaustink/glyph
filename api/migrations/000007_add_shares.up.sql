-- ─── Per-Item Shares ──────────────────────────────────────────────────────────
-- A share grants a specific user access to a specific resource.
-- resource_type must match the table name convention used throughout the API.
CREATE TABLE shares (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    resource_type    TEXT        NOT NULL CHECK (resource_type IN ('page', 'task', 'lane', 'template')),
    resource_id      UUID        NOT NULL,
    shared_by_id     UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    shared_with_id   UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    permission       TEXT        NOT NULL CHECK (permission IN ('viewer', 'editor')),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- One share per (resource, recipient): updating changes the permission.
    UNIQUE (resource_type, resource_id, shared_with_id)
);

CREATE INDEX shares_resource_idx   ON shares(resource_type, resource_id);
CREATE INDEX shares_recipient_idx  ON shares(shared_with_id);
