-- Extensions
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- ─── Users ────────────────────────────────────────────────────────────────────
-- Users are created on first OIDC login; the sub claim + issuer form the
-- stable identity. All other domain objects are scoped to a user_id.
CREATE TABLE users (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    sub        TEXT        NOT NULL,
    issuer     TEXT        NOT NULL,
    email      TEXT,
    name       TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (sub, issuer)
);

-- ─── Pages (tree nodes) ───────────────────────────────────────────────────────
CREATE TABLE pages (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type             TEXT        NOT NULL CHECK (type IN ('page', 'folder')),
    title            TEXT        NOT NULL DEFAULT '',
    parent_id        UUID        REFERENCES pages(id) ON DELETE CASCADE,
    "order"          INTEGER     NOT NULL DEFAULT 0,
    tags             TEXT[]      NOT NULL DEFAULT '{}',
    -- JSON column for TodoTriggerConfig; NULL means use the app default
    todo_trigger     JSONB,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX pages_user_id_idx     ON pages(user_id);
CREATE INDEX pages_parent_id_idx   ON pages(parent_id);

-- ─── Page Content ─────────────────────────────────────────────────────────────
-- Stored separately so listing the tree is cheap.
CREATE TABLE page_contents (
    page_id    UUID        PRIMARY KEY REFERENCES pages(id) ON DELETE CASCADE,
    content    TEXT        NOT NULL DEFAULT '',  -- ProseMirror JSON string
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ─── Tasks ────────────────────────────────────────────────────────────────────
CREATE TABLE tasks (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title          TEXT        NOT NULL DEFAULT '',
    description    TEXT        NOT NULL DEFAULT '',
    status         TEXT        NOT NULL DEFAULT 'todo'
                                CHECK (status IN ('todo','in-progress','done','cancelled')),
    priority       TEXT        NOT NULL DEFAULT 'none'
                                CHECK (priority IN ('urgent','high','medium','low','none')),
    tags           TEXT[]      NOT NULL DEFAULT '{}',
    due_date       DATE,
    source_page_id UUID        REFERENCES pages(id) ON DELETE SET NULL,
    source_node_id TEXT,
    "order"        INTEGER     NOT NULL DEFAULT 0,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX tasks_user_id_idx ON tasks(user_id);
CREATE INDEX tasks_source_page_id_idx ON tasks(source_page_id);

-- ─── Lanes ────────────────────────────────────────────────────────────────────
CREATE TABLE lanes (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title       TEXT        NOT NULL DEFAULT '',
    filter_set  JSONB       NOT NULL DEFAULT '{"conjunction":"and","rules":[]}',
    sort_config JSONB       NOT NULL DEFAULT '{"mode":"auto"}',
    "order"     INTEGER     NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX lanes_user_id_idx ON lanes(user_id);

-- ─── Templates ────────────────────────────────────────────────────────────────
CREATE TABLE templates (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name           TEXT        NOT NULL DEFAULT '',
    content        TEXT        NOT NULL DEFAULT '',
    title_template TEXT        NOT NULL DEFAULT '',
    todo_trigger   JSONB,
    is_default     BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX templates_user_id_idx ON templates(user_id);
