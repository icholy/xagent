-- +goose Up

ALTER TABLE orgs RENAME COLUMN jira_webhook_secret TO atlassian_webhook_secret;

-- +goose Down

ALTER TABLE orgs RENAME COLUMN atlassian_webhook_secret TO jira_webhook_secret;
