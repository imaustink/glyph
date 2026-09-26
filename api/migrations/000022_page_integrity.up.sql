-- Page-tree data integrity (DI-07, …).

-- DI-07: a nil todoTrigger used to be written as the JSONB value null instead
-- of SQL NULL, and read back as an empty config that disables TODO detection.
UPDATE pages     SET todo_trigger = NULL WHERE todo_trigger = 'null'::jsonb;
UPDATE templates SET todo_trigger = NULL WHERE todo_trigger = 'null'::jsonb;
