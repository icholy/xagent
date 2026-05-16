-- migrate:up
ALTER TABLE task_links RENAME COLUMN notify TO subscribe;

-- migrate:down
ALTER TABLE task_links RENAME COLUMN subscribe TO notify;
