-- name: CreateWebhook :exec
INSERT INTO webhooks (uuid, secret, owner, created_at)
VALUES ($1, $2, $3, $4);

-- name: GetWebhook :one
SELECT uuid, secret, owner, created_at
FROM webhooks
WHERE uuid = $1 AND owner = $2;

-- name: ListWebhooks :many
SELECT uuid, secret, owner, created_at
FROM webhooks
WHERE owner = $1
ORDER BY created_at DESC;

-- name: DeleteWebhook :exec
DELETE FROM webhooks WHERE uuid = $1 AND owner = $2;
