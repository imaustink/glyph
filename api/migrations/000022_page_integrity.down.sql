-- DI-02: restore the cascading foreign key. History of pages that were
-- deleted while it was absent has no page to reference and must go first.
DELETE FROM page_content_versions v WHERE NOT EXISTS (SELECT 1 FROM pages p WHERE p.id = v.page_id);
ALTER TABLE page_content_versions
    ADD CONSTRAINT page_content_versions_page_id_fkey
    FOREIGN KEY (page_id) REFERENCES pages(id) ON DELETE CASCADE;

-- The DI-22 share clean-up deleted rows that granted nothing reachable (or
-- nothing manageable); it is not reversed.
-- The DI-07 backfill (JSONB null → SQL NULL) is not reversible and needs no
-- undoing: both mean "no trigger" to the code on either side of it.
SELECT 1;
