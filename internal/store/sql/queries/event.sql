-- name: CreateEvent :one
INSERT INTO events (description, data, url, created_at, org_id, task_id)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id;

-- name: GetEvent :one
SELECT id, description, data, url, org_id, created_at, task_id
FROM events
WHERE id = $1 AND org_id = $2;

-- name: ListEvents :many
SELECT id, description, data, url, org_id, created_at, task_id
FROM events
WHERE org_id = $1
ORDER BY created_at DESC
LIMIT $2;

-- name: DeleteEvent :exec
DELETE FROM events WHERE id = $1 AND org_id = $2;

-- name: ListEventsByTask :many
SELECT id, description, data, url, org_id, created_at, task_id
FROM events
WHERE task_id = $1 AND org_id = $2
ORDER BY created_at DESC;
