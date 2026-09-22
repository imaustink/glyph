-- Optimistic concurrency and recovery history for page content.
--
-- Background: page_contents updates were unconditional last-write-wins with no
-- history. A stale client could overwrite a note (and, on 2026-09-21, ten of
-- them) with no way to detect the conflict and no way to recover except a
-- point-in-time restore of the whole database.
--
-- `revision` gives writers a precondition to check against, so a write based on
-- a stale read can be rejected instead of silently clobbering.
-- `page_content_versions` retains superseded content so a bad write is
-- recoverable in-product.

ALTER TABLE page_contents
  ADD COLUMN revision INTEGER NOT NULL DEFAULT 1;

CREATE TABLE IF NOT EXISTS page_content_versions (
    id             BIGSERIAL   PRIMARY KEY,
    page_id        UUID        NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
    content        JSONB       NOT NULL,
    -- The revision this content WAS, before being superseded.
    revision       INTEGER     NOT NULL,
    schema_version INTEGER     NOT NULL DEFAULT 1,
    replaced_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Supports "most recent versions for this page" lookups and the retention
-- prune, which both order by replaced_at within a page.
CREATE INDEX IF NOT EXISTS page_content_versions_page_replaced_idx
    ON page_content_versions (page_id, replaced_at DESC);

-- Seed history with the current content so the very first post-migration
-- overwrite is still recoverable.
INSERT INTO page_content_versions (page_id, content, revision, schema_version, replaced_at)
SELECT page_id, content, revision, schema_version, updated_at
FROM page_contents;
