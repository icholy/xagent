-- name: CreateGitHubAccount :one
INSERT INTO github_accounts (owner, github_user_id, github_username)
VALUES ($1, $2, $3)
ON CONFLICT (owner) DO UPDATE SET
    github_user_id = EXCLUDED.github_user_id,
    github_username = EXCLUDED.github_username
RETURNING id, owner, github_user_id, github_username, created_at;

-- name: GetGitHubAccountByOwner :one
SELECT id, owner, github_user_id, github_username, created_at
FROM github_accounts
WHERE owner = $1;

-- name: GetGitHubAccountByGitHubUserID :one
SELECT id, owner, github_user_id, github_username, created_at
FROM github_accounts
WHERE github_user_id = $1;

-- name: DeleteGitHubAccount :exec
DELETE FROM github_accounts WHERE owner = $1;

-- name: UpdateGitHubUsername :exec
UPDATE github_accounts SET github_username = $1 WHERE github_user_id = $2;
