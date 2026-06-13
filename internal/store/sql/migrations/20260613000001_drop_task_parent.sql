-- migrate:up

-- Child tasks have been removed. Drop the parent column and its index.
DROP INDEX IF EXISTS idx_tasks_parent;
ALTER TABLE tasks DROP COLUMN IF EXISTS parent;

-- migrate:down

ALTER TABLE tasks ADD COLUMN parent bigint DEFAULT 0 NOT NULL;
CREATE INDEX idx_tasks_parent ON tasks (parent);
