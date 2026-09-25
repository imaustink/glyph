-- Realtime collaborative editing storage.
--
-- While a page is ATTACHED, its authoritative content is the Yjs update log in
-- page_collab_updates, owned by the collab service. page_contents becomes a
-- derived snapshot the collab service writes back through the API (so search,
-- the task board, exports and REST reads keep working), and REST content
-- writes for the page are refused.
--
-- While a page is DETACHED (never collaboratively edited, collaboration turned
-- off, or a version was just restored), page_contents is authoritative and the
-- next collaborative session re-seeds the log from it under a new epoch.

CREATE TABLE page_collab_docs (
    page_id            UUID        PRIMARY KEY REFERENCES pages(id) ON DELETE CASCADE,
    -- Bumped every time the shared document is (re)seeded from page_contents.
    -- A client that still holds state from an earlier epoch must discard it:
    -- merging it would resurrect whatever the re-seed replaced.
    epoch              INTEGER     NOT NULL DEFAULT 0,
    attached           BOOLEAN     NOT NULL DEFAULT false,
    -- Fingerprint of the ProseMirror schema the log was written under.
    schema_fingerprint TEXT,
    -- Highest update seq already reflected in page_contents. Snapshots must
    -- not go backwards, so a lagging collab replica can't regress content.
    snapshot_seq       BIGINT      NOT NULL DEFAULT 0,
    -- Set when the collab service finds the merged document invalid. The page
    -- becomes read-only for collaborators until a version is restored.
    quarantined_at     TIMESTAMPTZ,
    quarantine_reason  TEXT,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Append-only: rows are only ever inserted, or replaced by a merged row during
-- compaction (in one transaction, deleting exactly the rows it merged). Two
-- writers can therefore never lose each other's updates.
CREATE TABLE page_collab_updates (
    seq        BIGSERIAL   PRIMARY KEY,
    page_id    UUID        NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
    epoch      INTEGER     NOT NULL,
    data       BYTEA       NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX page_collab_updates_doc_idx ON page_collab_updates (page_id, epoch, seq);
