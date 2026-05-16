-- migrate:up

ALTER TABLE orgs ADD COLUMN archived BOOLEAN NOT NULL DEFAULT FALSE;

-- migrate:down

ALTER TABLE orgs DROP COLUMN archived;
