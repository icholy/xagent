-- name: UpsertUser :one
INSERT INTO users (id, email, name)
VALUES ($1, $2, $3)
ON CONFLICT (id) DO UPDATE SET
    email = EXCLUDED.email,
    name = EXCLUDED.name,
    updated_at = CURRENT_TIMESTAMP
RETURNING id, email, name, created_at, updated_at;

-- name: GetUser :one
SELECT id, email, name, created_at, updated_at
FROM users
WHERE id = $1;

-- name: GetUserByEmail :one
SELECT id, email, name, created_at, updated_at
FROM users
WHERE email = $1;
