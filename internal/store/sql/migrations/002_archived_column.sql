-- +goose Up
ALTER TABLE tasks ADD COLUMN archived BOOLEAN NOT NULL DEFAULT FALSE;
UPDATE tasks SET archived = TRUE, status = 'completed' WHERE status = 'archived';
CREATE INDEX idx_tasks_archived ON tasks(archived);

-- +goose Down
UPDATE tasks SET status = 'archived' WHERE archived = TRUE;
ALTER TABLE tasks DROP COLUMN archived;
