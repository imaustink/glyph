DELETE FROM lanes
WHERE title = 'Cancelled'
  AND filter_set @> '{"rules":[{"field":"status","value":"cancelled"}]}'::jsonb;
