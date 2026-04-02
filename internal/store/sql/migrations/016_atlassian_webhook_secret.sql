-- +goose Up

ALTER TABLE orgs ADD COLUMN atlassian_webhook_secret TEXT NOT NULL DEFAULT '';

-- +goose Down

ALTER TABLE orgs DROP COLUMN atlassian_webhook_secret;
