-- migrate:up
DROP INDEX tasks_org_created_id_idx;
CREATE INDEX tasks_org_created_id_idx
  ON tasks (org_id, created_at DESC, id DESC);

-- migrate:down
DROP INDEX tasks_org_created_id_idx;
CREATE INDEX tasks_org_created_id_idx
  ON tasks (org_id, created_at DESC, id DESC)
  WHERE archived = FALSE;
