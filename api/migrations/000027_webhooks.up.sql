-- User-configurable webhooks (#98).
--
-- A webhook subscribes a URL to a chosen set of event types. Events are
-- captured by database triggers on pages, page_contents and tasks, so every
-- write path (REST, MCP, collab snapshots, the server's own task
-- reconciliation, cascading deletes) produces them, and they are written in
-- the same transaction as the change itself (a transactional outbox): an
-- event exists if and only if its change committed.
--
-- Scope:
--   * personal webhook (org_id IS NULL): changes to rows the webhook's owner
--     owns (user_id).
--   * org webhook (org_id set): changes to non-private rows in that org, for
--     as long as the webhook's owner is still an owner of the org.

CREATE TABLE webhooks (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    org_id      UUID        REFERENCES organizations(id) ON DELETE CASCADE,
    name        TEXT        NOT NULL DEFAULT '',
    url         TEXT        NOT NULL,
    -- HMAC signing secret. Stored in the clear because the server must be
    -- able to compute signatures with it; never returned by the API except on
    -- create and rotate.
    secret      TEXT        NOT NULL,
    event_types TEXT[]      NOT NULL DEFAULT '{}'
                            CHECK (event_types <@ ARRAY[
                                'note.created', 'note.updated', 'note.deleted',
                                'task.created', 'task.updated', 'task.deleted'
                            ]::TEXT[]),
    active      BOOLEAN     NOT NULL DEFAULT true,
    -- Highest sequence number handed out to this webhook's events.
    last_seq    BIGINT      NOT NULL DEFAULT 0,
    -- Delivery lease: the dispatcher replica currently delivering this
    -- webhook's events, so deliveries stay in order across replicas.
    lease_until TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX webhooks_user_id_idx ON webhooks(user_id);
CREATE INDEX webhooks_org_id_idx  ON webhooks(org_id) WHERE org_id IS NOT NULL;

CREATE TABLE webhook_events (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    webhook_id       UUID        NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
    -- Insertion order, used to assign seq in the order events became visible.
    created_seq      BIGINT      GENERATED ALWAYS AS IDENTITY,
    -- Per-webhook, gap-free sequence number. Assigned after commit (by the
    -- dispatcher or the feed endpoint) rather than in the writing
    -- transaction, so a write never has to lock the webhook row; NULL until
    -- then.
    seq              BIGINT,
    event_type       TEXT        NOT NULL,
    resource_id      UUID        NOT NULL,
    payload          JSONB       NOT NULL,
    status           TEXT        NOT NULL DEFAULT 'pending'
                                 CHECK (status IN ('pending', 'delivered', 'failed')),
    attempts         INTEGER     NOT NULL DEFAULT 0,
    last_status_code INTEGER,
    last_error       TEXT,
    last_attempt_at  TIMESTAMPTZ,
    next_attempt_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    delivered_at     TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (webhook_id, seq)
);

CREATE INDEX webhook_events_webhook_created_idx ON webhook_events(webhook_id, created_seq);
CREATE INDEX webhook_events_unsequenced_idx ON webhook_events(webhook_id, created_seq) WHERE seq IS NULL;
CREATE INDEX webhook_events_pending_idx ON webhook_events(webhook_id, seq) WHERE status = 'pending';
CREATE INDEX webhook_events_created_at_idx ON webhook_events(created_at);

-- ─── Capture ─────────────────────────────────────────────────────────────────

-- webhook_covers reports whether webhook w's scope covers a row owned by
-- p_owner in org p_org with the given privacy.
CREATE FUNCTION webhook_covers(w webhooks, p_owner UUID, p_org UUID, p_private BOOLEAN)
RETURNS BOOLEAN LANGUAGE sql STABLE AS $$
    SELECT (w.org_id IS NULL AND w.user_id = p_owner)
        OR (w.org_id IS NOT NULL AND w.org_id IS NOT DISTINCT FROM p_org AND NOT p_private
            AND EXISTS (SELECT 1 FROM org_members m
                        WHERE m.org_id = w.org_id AND m.user_id = w.user_id AND m.role = 'owner'));
$$;

-- webhook_enqueue records one event for every active webhook subscribed to
-- p_type whose scope covers a row owned by p_owner in org p_org.
CREATE FUNCTION webhook_enqueue(p_type TEXT, p_owner UUID, p_org UUID, p_private BOOLEAN,
                                p_resource UUID, p_data JSONB)
RETURNS void LANGUAGE sql AS $$
    INSERT INTO webhook_events (webhook_id, event_type, resource_id, payload)
    SELECT w.id, p_type, p_resource, p_data
    FROM webhooks w
    WHERE w.active
      AND p_type = ANY (w.event_types)
      AND webhook_covers(w, p_owner, p_org, p_private)
    ORDER BY w.id;
$$;

-- webhook_enqueue_update records the events for an UPDATE of a row of kind
-- p_kind ('note' or 'task') whose scope tuple may have changed (it became
-- private or public, moved between orgs, or changed owner). Per webhook:
--   * covers OLD and NEW  -> <kind>.updated with the NEW payload
--   * covers NEW only     -> <kind>.created with the NEW payload (it entered scope)
--   * covers OLD only     -> <kind>.deleted with the OLD payload (it left scope)
-- so an org webhook never keeps a row that has become private, and never gets
-- an update for a row it was never told about.
CREATE FUNCTION webhook_enqueue_update(p_kind TEXT,
                                       o_owner UUID, o_org UUID, o_private BOOLEAN, o_data JSONB,
                                       n_owner UUID, n_org UUID, n_private BOOLEAN, n_data JSONB,
                                       p_resource UUID)
RETURNS void LANGUAGE sql AS $$
    INSERT INTO webhook_events (webhook_id, event_type, resource_id, payload)
    SELECT e.id, e.typ, p_resource, e.payload
    FROM (
        SELECT w.id, w.event_types,
               CASE WHEN c.o AND c.n THEN p_kind || '.updated'
                    WHEN c.n         THEN p_kind || '.created'
                    ELSE                  p_kind || '.deleted' END AS typ,
               CASE WHEN c.o AND NOT c.n THEN o_data ELSE n_data END AS payload
        FROM webhooks w
        CROSS JOIN LATERAL (SELECT webhook_covers(w, o_owner, o_org, o_private) AS o,
                                   webhook_covers(w, n_owner, n_org, n_private) AS n) c
        WHERE w.active AND (c.o OR c.n)
    ) e
    WHERE e.typ = ANY (e.event_types)
    ORDER BY e.id;
$$;

-- The event payloads mirror the REST API's JSON for a page and a task.
CREATE FUNCTION webhook_page_json(p pages) RETURNS JSONB LANGUAGE sql STABLE AS $$
    SELECT jsonb_build_object(
        'id', p.id,
        'userId', p.user_id,
        'type', p.type,
        'title', p.title,
        'parentId', p.parent_id,
        'order', p."order",
        'tags', to_jsonb(p.tags),
        'priority', p.priority,
        'todoTrigger', p.todo_trigger,
        'orgId', p.org_id,
        'isPrivate', p.is_private,
        'createdAt', p.created_at,
        'updatedAt', p.updated_at
    );
$$;

CREATE FUNCTION webhook_task_json(t tasks) RETURNS JSONB LANGUAGE sql STABLE AS $$
    SELECT jsonb_build_object(
        'id', t.id,
        'userId', t.user_id,
        'title', t.title,
        'description', t.description,
        'status', t.status,
        'priority', t.priority,
        'tags', to_jsonb(t.tags),
        'dueDate', t.due_date,
        'sourcePageId', t.source_page_id,
        'sourceNodeId', t.source_node_id,
        'link', t.link,
        'order', t."order",
        'orgId', t.org_id,
        'isPrivate', t.is_private,
        'folderId', t.folder_id,
        'createdAt', t.created_at,
        'updatedAt', t.updated_at
    );
$$;

-- Notes are pages of type 'page'; folders produce no events.
CREATE FUNCTION webhook_pages_trigger() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    n JSONB;
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.type = 'page' THEN
            PERFORM webhook_enqueue('note.deleted', OLD.user_id, OLD.org_id, OLD.is_private,
                                    OLD.id, webhook_page_json(OLD));
        END IF;
        RETURN NULL;
    END IF;
    IF NEW.type <> 'page' THEN
        RETURN NULL;
    END IF;
    n := webhook_page_json(NEW);
    IF TG_OP = 'INSERT' THEN
        PERFORM webhook_enqueue('note.created', NEW.user_id, NEW.org_id, NEW.is_private, NEW.id, n);
    ELSIF (webhook_page_json(OLD) - 'updatedAt') IS DISTINCT FROM (n - 'updatedAt') THEN
        PERFORM webhook_enqueue_update('note',
                                       OLD.user_id, OLD.org_id, OLD.is_private, webhook_page_json(OLD),
                                       NEW.user_id, NEW.org_id, NEW.is_private, n, NEW.id);
    END IF;
    RETURN NULL;
END;
$$;

CREATE TRIGGER pages_webhook_events
    AFTER INSERT OR UPDATE OR DELETE ON pages
    FOR EACH ROW EXECUTE FUNCTION webhook_pages_trigger();

-- A content change is a note.updated (the payload is the page itself; fetch
-- /pages/:id/content for the document).
CREATE FUNCTION webhook_page_contents_trigger() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    p pages;
BEGIN
    IF TG_OP = 'UPDATE' AND OLD.content IS NOT DISTINCT FROM NEW.content THEN
        RETURN NULL;
    END IF;
    SELECT * INTO p FROM pages WHERE id = NEW.page_id;
    IF FOUND AND p.type = 'page' THEN
        PERFORM webhook_enqueue('note.updated', p.user_id, p.org_id, p.is_private, p.id,
                                webhook_page_json(p));
    END IF;
    RETURN NULL;
END;
$$;

CREATE TRIGGER page_contents_webhook_events
    AFTER INSERT OR UPDATE ON page_contents
    FOR EACH ROW EXECUTE FUNCTION webhook_page_contents_trigger();

-- Tasks are soft-deleted: setting deleted_at is a task.deleted, and clearing
-- it again (the server restoring a task whose bullet came back) is a
-- task.created. Changes to an already-deleted task produce nothing.
CREATE FUNCTION webhook_tasks_trigger() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    n JSONB;
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.deleted_at IS NULL THEN
            PERFORM webhook_enqueue('task.deleted', OLD.user_id, OLD.org_id, OLD.is_private,
                                    OLD.id, webhook_task_json(OLD));
        END IF;
        RETURN NULL;
    END IF;
    IF NEW.deleted_at IS NOT NULL AND (TG_OP = 'INSERT' OR OLD.deleted_at IS NOT NULL) THEN
        RETURN NULL;
    END IF;
    n := webhook_task_json(NEW);
    IF TG_OP = 'INSERT' OR OLD.deleted_at IS NOT NULL THEN
        PERFORM webhook_enqueue('task.created', NEW.user_id, NEW.org_id, NEW.is_private, NEW.id, n);
    ELSIF NEW.deleted_at IS NOT NULL THEN
        PERFORM webhook_enqueue('task.deleted', NEW.user_id, NEW.org_id, NEW.is_private, NEW.id, n);
    ELSIF (webhook_task_json(OLD) - 'updatedAt') IS DISTINCT FROM (n - 'updatedAt') THEN
        PERFORM webhook_enqueue_update('task',
                                       OLD.user_id, OLD.org_id, OLD.is_private, webhook_task_json(OLD),
                                       NEW.user_id, NEW.org_id, NEW.is_private, n, NEW.id);
    END IF;
    RETURN NULL;
END;
$$;

CREATE TRIGGER tasks_webhook_events
    AFTER INSERT OR UPDATE OR DELETE ON tasks
    FOR EACH ROW EXECUTE FUNCTION webhook_tasks_trigger();
