-- +goose Up

ALTER TABLE users ADD COLUMN atlassian_account_id TEXT;
CREATE UNIQUE INDEX idx_users_atlassian_account_id ON users(atlassian_account_id);

ALTER TABLE orgs ADD COLUMN jira_webhook_secret TEXT NOT NULL DEFAULT '';

-- +goose Down

ALTER TABLE orgs DROP COLUMN jira_webhook_secret;
DROP INDEX idx_users_atlassian_account_id;
ALTER TABLE users DROP COLUMN atlassian_account_id;
