-- name: CreateKey :exec
INSERT INTO keys (id, name, token_hash, owner, expires_at, created_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: GetKey :one
SELECT id, name, token_hash, owner, expires_at, created_at
FROM keys
WHERE id = $1 AND owner = $2;

-- name: GetKeyByHash :one
SELECT id, name, token_hash, owner, expires_at, created_at
FROM keys
WHERE token_hash = $1;

-- name: ListKeys :many
SELECT id, name, token_hash, owner, expires_at, created_at
FROM keys
WHERE owner = $1
ORDER BY created_at DESC;

-- name: DeleteKey :exec
DELETE FROM keys WHERE id = $1 AND owner = $2;
