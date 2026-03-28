-- +goose Up
ALTER TABLE users ALTER COLUMN github_username DROP NOT NULL;

-- +goose Down
UPDATE users SET github_username = '' WHERE github_username IS NULL;
ALTER TABLE users ALTER COLUMN github_username SET NOT NULL;
