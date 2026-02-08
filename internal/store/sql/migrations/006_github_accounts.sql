-- +goose Up
CREATE TABLE github_accounts (
    id              BIGSERIAL PRIMARY KEY,
    owner           TEXT NOT NULL,
    github_user_id  BIGINT NOT NULL,
    github_username TEXT NOT NULL,
    created_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX idx_github_accounts_owner ON github_accounts(owner);
CREATE UNIQUE INDEX idx_github_accounts_github_user_id ON github_accounts(github_user_id);

-- +goose Down
DROP TABLE github_accounts;
