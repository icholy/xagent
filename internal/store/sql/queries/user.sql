-- name: UpsertUser :one
INSERT INTO users (id, email, name)
VALUES ($1, $2, $3)
ON CONFLICT (id) DO UPDATE SET
    email = EXCLUDED.email,
    name = EXCLUDED.name,
    updated_at = CURRENT_TIMESTAMP
RETURNING id, email, name, github_user_id, github_username, created_at, updated_at;

-- name: GetUser :one
SELECT id, email, name, github_user_id, github_username, created_at, updated_at
FROM users
WHERE id = $1;

-- name: GetUserByEmail :one
SELECT id, email, name, github_user_id, github_username, created_at, updated_at
FROM users
WHERE email = $1;

-- name: GetUserByGitHubUserID :one
SELECT id, email, name, github_user_id, github_username, created_at, updated_at
FROM users
WHERE github_user_id = $1;

-- name: LinkGitHubAccount :exec
UPDATE users SET
    github_user_id = $2,
    github_username = $3,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $1;

-- name: UnlinkGitHubAccount :exec
UPDATE users SET
    github_user_id = NULL,
    github_username = '',
    updated_at = CURRENT_TIMESTAMP
WHERE id = $1;

-- name: UpdateGitHubUsername :exec
UPDATE users SET
    github_username = $1,
    updated_at = CURRENT_TIMESTAMP
WHERE github_user_id = $2;
