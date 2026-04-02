-- +goose Up

ALTER TABLE users ADD COLUMN atlassian_account_id TEXT;
CREATE UNIQUE INDEX idx_users_atlassian_account_id ON users(atlassian_account_id);

-- +goose Down

DROP INDEX idx_users_atlassian_account_id;
ALTER TABLE users DROP COLUMN atlassian_account_id;
