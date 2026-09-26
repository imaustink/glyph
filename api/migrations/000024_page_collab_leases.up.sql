-- Which collab replicas hold a page's shared document in memory.
--
-- A replica can hold edits for a page that it hasn't appended to the log yet
-- (they wait out the store debounce, or a persistence outage). Re-seeding the
-- page into a new epoch — which a replica running a newer editor schema does
-- the first time it opens a page written under an older one — makes those
-- appends fail, and the edits are lost. So each replica keeps a lease on
-- every page it has loaded, renewed while it is loaded and deleted when it
-- unloads, and a schema re-seed is refused while another replica holds an
-- unexpired lease on the epoch being replaced (DI-16). Leases expire, so a
-- replica that dies without releasing one only delays re-seeding.
--
-- Additive: collab builds that predate this table ignore it (and a new build
-- that starts before this migration has run skips leasing until it exists).
CREATE TABLE page_collab_leases (
    page_id    UUID        NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
    -- Opaque, unique per collab process.
    holder     TEXT        NOT NULL,
    -- The epoch of the copy the holder has loaded.
    epoch      INTEGER     NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (page_id, holder)
);
