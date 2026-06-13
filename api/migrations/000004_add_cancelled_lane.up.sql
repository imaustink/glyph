-- Add a Cancelled lane for any user who has lanes but is missing one.
INSERT INTO lanes (user_id, title, filter_set, sort_config, "order")
SELECT
    u.id,
    'Cancelled',
    '{"conjunction":"and","rules":[{"id":"default-3","field":"status","operator":"eq","value":"cancelled"}]}'::jsonb,
    '{"mode":"auto"}'::jsonb,
    (SELECT COALESCE(MAX(l."order"), -1) + 1 FROM lanes l WHERE l.user_id = u.id)
FROM users u
WHERE EXISTS (SELECT 1 FROM lanes l WHERE l.user_id = u.id)
  AND NOT EXISTS (
      SELECT 1 FROM lanes l
      WHERE l.user_id = u.id
        AND l.filter_set @> '{"rules":[{"field":"status","value":"cancelled"}]}'::jsonb
  );
