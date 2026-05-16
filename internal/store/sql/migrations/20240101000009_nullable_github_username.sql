-- migrate:up
ALTER TABLE users ALTER COLUMN github_username DROP NOT NULL;

-- migrate:down
UPDATE users SET github_username = '' WHERE github_username IS NULL;
ALTER TABLE users ALTER COLUMN github_username SET NOT NULL;
