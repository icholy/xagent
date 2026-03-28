-- +goose Up
CREATE TABLE users (
    id              TEXT PRIMARY KEY,
    email           TEXT NOT NULL,
    name            TEXT NOT NULL DEFAULT '',
    github_user_id  BIGINT,
    github_username TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX idx_users_email ON users(email);
CREATE UNIQUE INDEX idx_users_github_user_id ON users(github_user_id);

-- Migrate existing GitHub account data into the users table.
-- This creates stub user records for any github_accounts that don't
-- already have a corresponding user row.
INSERT INTO users (id, email, name, github_user_id, github_username)
SELECT owner, '', '', github_user_id, github_username
FROM github_accounts
ON CONFLICT (id) DO UPDATE SET
    github_user_id = EXCLUDED.github_user_id,
    github_username = EXCLUDED.github_username;

DROP TABLE github_accounts;

-- +goose Down
CREATE TABLE github_accounts (
    id              BIGSERIAL PRIMARY KEY,
    owner           TEXT NOT NULL,
    github_user_id  BIGINT NOT NULL,
    github_username TEXT NOT NULL,
    created_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX idx_github_accounts_owner ON github_accounts(owner);
CREATE UNIQUE INDEX idx_github_accounts_github_user_id ON github_accounts(github_user_id);
DROP TABLE users;
