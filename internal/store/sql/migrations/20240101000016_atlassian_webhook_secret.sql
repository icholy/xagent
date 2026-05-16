-- migrate:up

ALTER TABLE orgs ADD COLUMN atlassian_webhook_secret TEXT NOT NULL DEFAULT '';

-- migrate:down

ALTER TABLE orgs DROP COLUMN atlassian_webhook_secret;
