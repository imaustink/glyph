CREATE INDEX tasks_source_node_id_idx ON tasks (user_id, source_node_id) WHERE source_node_id IS NOT NULL;
