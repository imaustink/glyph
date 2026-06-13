-- Widen the "order" columns from INTEGER (int4, max ~2.1 billion) to BIGINT
-- (int8) so the frontend can safely use Date.now() (milliseconds, ~1.75
-- trillion today) as a monotonic sort key without integer overflow.

ALTER TABLE pages ALTER COLUMN "order" TYPE BIGINT;
ALTER TABLE tasks ALTER COLUMN "order" TYPE BIGINT;
ALTER TABLE lanes ALTER COLUMN "order" TYPE BIGINT;
