-- migrate:up
ALTER TABLE tasks ADD COLUMN namespace TEXT NOT NULL DEFAULT '';

-- migrate:down
ALTER TABLE tasks DROP COLUMN IF EXISTS namespace;
