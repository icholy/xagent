-- migrate:up
ALTER TABLE tasks ADD COLUMN shell_session text NOT NULL DEFAULT '';

-- migrate:down
ALTER TABLE tasks DROP COLUMN shell_session;
