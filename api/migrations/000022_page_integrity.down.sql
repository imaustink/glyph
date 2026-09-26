-- The DI-07 backfill (JSONB null → SQL NULL) is not reversible and needs no
-- undoing: both mean "no trigger" to the code on either side of it.
SELECT 1;
